package server

import (
	"context"
	"testing"

	"github.com/Mutech-Innovation/coding-anywhere/internal/auth"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
)

func newTestGuestConn(promptLimit, maxSessions int) *Conn {
	return &Conn{
		id:        "conn-1",
		accountID: "acc-1",
		deviceID:  "share-1",
		kind:      auth.DeviceGuest,
		send:      make(chan wire.Envelope, 32),
		guest:     newGuestState(promptLimit, maxSessions),
	}
}

func chatReq(t *testing.T, id, method string, payload any) wire.Envelope {
	t.Helper()
	env, err := wire.MakeRequest(id, wire.ChannelChat, method, payload)
	if err != nil {
		t.Fatalf("MakeRequest: %v", err)
	}
	return env
}

func TestSessionIDFromPayload(t *testing.T) {
	flat, _ := wire.MakeEvent("e1", wire.ChannelChat, wire.ChatEventUpdate,
		wire.ChatUpdateEvent{SessionID: "sess-flat"})
	if got := sessionIDFromPayload(flat.Payload); got != "sess-flat" {
		t.Fatalf("flat: got %q", got)
	}
	nested, _ := wire.MakeResponse("r1", wire.ChannelChat,
		wire.ChatCreateSessionResponse{Session: wire.ChatSession{ID: "sess-nested"}}, nil)
	if got := sessionIDFromPayload(nested.Payload); got != "sess-nested" {
		t.Fatalf("nested: got %q", got)
	}
	if got := sessionIDFromPayload(nil); got != "" {
		t.Fatalf("empty: got %q", got)
	}
}

func TestGuestGateRejectsNonChatChannel(t *testing.T) {
	s := &Server{}
	conn := newTestGuestConn(0, 1)
	env, _ := wire.MakeRequest("t-1", wire.ChannelTerminal, "open", map[string]any{})
	if s.guestGate(context.Background(), conn, &env) {
		t.Fatal("expected non-chat channel to be rejected for guest")
	}
	select {
	case res := <-conn.send:
		if res.Error == nil {
			t.Fatal("expected error response on reject")
		}
	default:
		t.Fatal("expected a rejection response delivered to the guest")
	}
}

func TestGuestGateRejectsDisallowedChatMethods(t *testing.T) {
	s := &Server{}
	for _, m := range []string{
		wire.ChatMethodSetMode,
		wire.ChatMethodSetConfigOption,
		wire.ChatMethodResolvePermit,
		wire.ChatMethodCloseSession,
		wire.ChatMethodListSessions,
		wire.ChatMethodAgents,
	} {
		conn := newTestGuestConn(0, 1)
		env := chatReq(t, "x", m, map[string]any{})
		if s.guestGate(context.Background(), conn, &env) {
			t.Fatalf("expected chat method %q to be rejected for guest", m)
		}
	}
}

func TestGuestGateAllowsCreateSessionAndStampsOrigin(t *testing.T) {
	s := &Server{}
	conn := newTestGuestConn(0, 1)
	env := chatReq(t, "c-1", wire.ChatMethodCreateSession, wire.ChatCreateSessionRequest{AgentID: "devin"})
	if !s.guestGate(context.Background(), conn, &env) {
		t.Fatal("expected create_session to be allowed")
	}
	if env.Origin != wire.OriginGuest {
		t.Fatalf("expected Origin=guest, got %q", env.Origin)
	}
	// A second create exceeds maxSessions=1.
	env2 := chatReq(t, "c-2", wire.ChatMethodCreateSession, wire.ChatCreateSessionRequest{AgentID: "devin"})
	if s.guestGate(context.Background(), conn, &env2) {
		t.Fatal("expected second create_session to hit the session cap")
	}
}

func TestGuestSessionIsolationAndPromptCap(t *testing.T) {
	s := &Server{}
	conn := newTestGuestConn(1, 1) // promptLimit=1, maxSessions=1

	// Prompt to an unknown session is rejected (isolation).
	bad := chatReq(t, "p-0", wire.ChatMethodPrompt, wire.ChatPromptRequest{SessionID: "not-mine"})
	if s.guestGate(context.Background(), conn, &bad) {
		t.Fatal("expected prompt to unowned session to be rejected")
	}

	// Create a session, then simulate the daemon's response so the guest
	// learns ownership.
	create := chatReq(t, "c-1", wire.ChatMethodCreateSession, wire.ChatCreateSessionRequest{AgentID: "devin"})
	if !s.guestGate(context.Background(), conn, &create) {
		t.Fatal("create_session should be allowed")
	}
	res, _ := wire.MakeResponse("c-1", wire.ChannelChat,
		wire.ChatCreateSessionResponse{Session: wire.ChatSession{ID: "sess-1"}}, nil)
	if !guestMayReceive(conn, res) {
		t.Fatal("guest should receive its own create_session response")
	}

	// Now prompts to the owned session are allowed, up to the cap.
	p1 := chatReq(t, "p-1", wire.ChatMethodPrompt, wire.ChatPromptRequest{SessionID: "sess-1"})
	if !s.guestGate(context.Background(), conn, &p1) {
		t.Fatal("first prompt to owned session should be allowed")
	}
	p2 := chatReq(t, "p-2", wire.ChatMethodPrompt, wire.ChatPromptRequest{SessionID: "sess-1"})
	if s.guestGate(context.Background(), conn, &p2) {
		t.Fatal("second prompt should hit the prompt cap")
	}
}

func TestGuestMayReceiveFiltersEvents(t *testing.T) {
	conn := newTestGuestConn(0, 2)
	conn.guest.addOwned("mine")

	owned, _ := wire.MakeEvent("e1", wire.ChannelChat, wire.ChatEventUpdate,
		wire.ChatUpdateEvent{SessionID: "mine"})
	if !guestMayReceive(conn, owned) {
		t.Fatal("event for owned session should pass")
	}
	other, _ := wire.MakeEvent("e2", wire.ChannelChat, wire.ChatEventUpdate,
		wire.ChatUpdateEvent{SessionID: "someone-else"})
	if guestMayReceive(conn, other) {
		t.Fatal("event for a non-owned session must be filtered (isolation)")
	}
	// A response correlated to an id the guest never sent is dropped.
	stray, _ := wire.MakeResponse("never-sent", wire.ChannelChat, map[string]any{}, nil)
	if guestMayReceive(conn, stray) {
		t.Fatal("response for an unknown request id must be dropped")
	}
	// Non-chat traffic is never delivered to guests.
	term, _ := wire.MakeEvent("e3", wire.ChannelTerminal, "data", map[string]any{})
	if guestMayReceive(conn, term) {
		t.Fatal("non-chat envelopes must never reach a guest")
	}
}
