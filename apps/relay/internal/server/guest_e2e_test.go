package server_test

// End-to-end test for the guest "ask"-mode share path:
//
//   guest WS ─ chat.* ──▶ relay (guest gate) ──▶ daemon (chatrouter, ask-lock)
//
// Verifies that a guest token, minted via POST /v1/share/ask, can chat but:
//   - the daemon forces the session into ask mode at creation,
//   - the relay rejects set_mode, non-chat channels, and prompts to sessions
//     the guest does not own (isolation).

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/agent/acpagent"
	"github.com/Mutech-Innovation/coding-anywhere/internal/auth"
	"github.com/Mutech-Innovation/coding-anywhere/internal/chatrouter"
	"github.com/Mutech-Innovation/coding-anywhere/internal/pairingclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestGuestAskShareEndToEnd(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	pc := pairingclient.New(hs.URL)
	init, _ := pc.Init(ctx, "daemon", "lap", "")
	claim, _ := pc.Claim(ctx, init.Code, "client", "phone", "")
	dc, _ := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond)

	// Mock agent advertises ask+code, currently in "code" — so the daemon must
	// actively force "ask" for the guest session.
	mock := &snapshotMockACP{
		t:           t,
		respWaiters: map[string]chan map[string]any{},
		modes: map[string]any{
			"currentModeId": "code",
			"availableModes": []map[string]any{
				{"id": "ask", "name": "Ask"},
				{"id": "code", "name": "Code"},
			},
		},
	}
	stdoutR, stdoutW := io.Pipe()
	stdinR, stdinW := io.Pipe()
	mock.in = stdinR
	mock.wr = bufio.NewWriter(stdoutW)
	go mock.run()

	relayURL := strings.Replace(hs.URL, "http", "ws", 1)
	dcli := relayclient.New(relayclient.Config{
		RelayURL: relayURL, AccessToken: dc.AccessToken,
		PingInterval: time.Second, HandshakeTimeout: 3 * time.Second,
	}, logger)
	cr := chatrouter.New(dcli, logger, chatrouter.Builders{chatrouter.AgentDevin: func(opts acpagent.Options) (*acpagent.Adapter, error) {
		return acpagent.NewWithStreams(stdinW, stdoutR, opts), nil
	}})
	t.Cleanup(func() { _ = cr.Close() })
	go func() { _ = dcli.Run(ctx) }()
	if err := dcli.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}

	// Mint a guest token via the share endpoint using the client's bearer.
	// The share carries the owner-chosen cwd + model; this daemon's chatrouter
	// has NO defaultCwd, so the create_session below succeeds only if the relay
	// injects the share cwd (the guest sends none), and the daemon must apply
	// the locked model via set_config_option.
	guestToken := createGuestShareToken(t, ctx, hs.URL, claim.Credentials.AccessToken, "/tmp", "gpt-5-high")

	guestWS, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+guestToken, nil)
	if err != nil {
		t.Fatalf("guest dial: %v", err)
	}
	t.Cleanup(func() { _ = guestWS.Close(websocket.StatusNormalClosure, "") })

	envCh := make(chan wire.Envelope, 64)
	go func() {
		for {
			var env wire.Envelope
			if err := wsjson.Read(ctx, guestWS, &env); err != nil {
				return
			}
			envCh <- env
		}
	}()
	awaitResp := func(id string) wire.Envelope {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case env := <-envCh:
				if env.ID == id && env.Type == wire.TypeResponse {
					return env
				}
			case <-deadline:
				t.Fatalf("timeout waiting for response %s", id)
			}
		}
	}

	// 1. Guest create_session succeeds and the daemon forces ask mode. The
	// guest deliberately sends NO cwd — the relay must inject the share's.
	req, _ := wire.MakeRequest("c-1", wire.ChannelChat, wire.ChatMethodCreateSession,
		wire.ChatCreateSessionRequest{AgentID: "devin"})
	_ = wsjson.Write(ctx, guestWS, req)
	res := awaitResp("c-1")
	if res.Error != nil {
		t.Fatalf("guest create_session error: %+v", res.Error)
	}
	var createRes wire.ChatCreateSessionResponse
	if err := json.Unmarshal(res.Payload, &createRes); err != nil {
		t.Fatalf("create payload: %v", err)
	}
	guestSession := createRes.Session.ID
	if guestSession == "" {
		t.Fatal("empty guest session id")
	}
	mock.mu.Lock()
	forced := mock.lastSetModeID
	forcedConfigID := mock.lastSetConfigID
	forcedModel := mock.lastSetConfigValue
	mock.mu.Unlock()
	if forced != "ask" {
		t.Fatalf("daemon should force ask mode for guest, got %q", forced)
	}
	// The daemon must lock the share's model via set_config_option.
	if forcedConfigID != "model" || forcedModel != "gpt-5-high" {
		t.Fatalf("daemon should force share model, got %s=%s", forcedConfigID, forcedModel)
	}

	// 2. Guest set_mode is rejected by the relay (can't escape ask).
	req, _ = wire.MakeRequest("m-1", wire.ChannelChat, wire.ChatMethodSetMode,
		wire.ChatSetModeRequest{SessionID: guestSession, ModeID: "code"})
	_ = wsjson.Write(ctx, guestWS, req)
	if res = awaitResp("m-1"); res.Error == nil {
		t.Fatal("expected guest set_mode to be rejected")
	}

	// 3. Guest cannot use non-chat channels.
	req, _ = wire.MakeRequest("t-1", wire.ChannelTerminal, "open", map[string]any{})
	_ = wsjson.Write(ctx, guestWS, req)
	if res = awaitResp("t-1"); res.Error == nil {
		t.Fatal("expected guest terminal request to be rejected")
	}

	// 4. Isolation: a guest cannot prompt a session it does not own.
	req, _ = wire.MakeRequest("p-x", wire.ChannelChat, wire.ChatMethodPrompt,
		wire.ChatPromptRequest{SessionID: "not-the-guests-session", Text: "hi"})
	_ = wsjson.Write(ctx, guestWS, req)
	if res = awaitResp("p-x"); res.Error == nil {
		t.Fatal("expected prompt to an unowned session to be rejected")
	}
}

// TestGuestPermissionAutoDeny verifies that when the agent asks for permission
// on a guest session, the daemon auto-denies it: the guest never receives a
// permission_request event, and the prompt still completes.
func TestGuestPermissionAutoDeny(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	pc := pairingclient.New(hs.URL)
	init, _ := pc.Init(ctx, "daemon", "lap", "")
	claim, _ := pc.Claim(ctx, init.Code, "client", "phone", "")
	dc, _ := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond)

	relayURL := strings.Replace(hs.URL, "http", "ws", 1)
	dcli := relayclient.New(relayclient.Config{
		RelayURL: relayURL, AccessToken: dc.AccessToken,
		PingInterval: time.Second, HandshakeTimeout: 3 * time.Second,
	}, logger)
	// This mock pushes a session/request_permission during every prompt and
	// blocks until it sees a decision — so if the daemon did NOT auto-deny,
	// the prompt would hang and the test would time out.
	_, stdout, stdin := newChatMockACP(t)
	cr := chatrouter.New(dcli, logger, chatrouter.Builders{chatrouter.AgentDevin: func(opts acpagent.Options) (*acpagent.Adapter, error) {
		return acpagent.NewWithStreams(stdin, stdout, opts), nil
	}}, "/tmp")
	t.Cleanup(func() { _ = cr.Close() })
	go func() { _ = dcli.Run(ctx) }()
	if err := dcli.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}

	guestToken := createGuestShareToken(t, ctx, hs.URL, claim.Credentials.AccessToken, "/tmp", "")
	guestWS, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+guestToken, nil)
	if err != nil {
		t.Fatalf("guest dial: %v", err)
	}
	t.Cleanup(func() { _ = guestWS.Close(websocket.StatusNormalClosure, "") })

	envCh := make(chan wire.Envelope, 64)
	go func() {
		for {
			var env wire.Envelope
			if err := wsjson.Read(ctx, guestWS, &env); err != nil {
				return
			}
			envCh <- env
		}
	}()

	// create_session (no cwd — the relay injects the share's)
	req, _ := wire.MakeRequest("c-1", wire.ChannelChat, wire.ChatMethodCreateSession,
		wire.ChatCreateSessionRequest{AgentID: "devin"})
	_ = wsjson.Write(ctx, guestWS, req)
	var guestSession string
	for guestSession == "" {
		select {
		case env := <-envCh:
			if env.ID == "c-1" && env.Type == wire.TypeResponse {
				if env.Error != nil {
					t.Fatalf("create_session error: %+v", env.Error)
				}
				var crResp wire.ChatCreateSessionResponse
				_ = json.Unmarshal(env.Payload, &crResp)
				guestSession = crResp.Session.ID
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout creating guest session")
		}
	}

	// prompt — the mock will request permission; the daemon must auto-deny so
	// the prompt completes and no permission_request reaches the guest.
	req, _ = wire.MakeRequest("p-1", wire.ChannelChat, wire.ChatMethodPrompt,
		wire.ChatPromptRequest{SessionID: guestSession, Text: "hello"})
	_ = wsjson.Write(ctx, guestWS, req)

	deadline := time.After(8 * time.Second)
	for {
		select {
		case env := <-envCh:
			if env.Channel == wire.ChannelChat && env.Method == wire.ChatEventPermissionRequest {
				t.Fatal("guest must NOT receive a permission_request event")
			}
			if env.ID == "p-1" && env.Type == wire.TypeResponse {
				if env.Error != nil {
					t.Fatalf("prompt failed: %+v", env.Error)
				}
				return // success: prompt completed without surfacing a permission
			}
		case <-deadline:
			t.Fatal("timeout waiting for prompt to complete (auto-deny may be broken)")
		}
	}
}

// TestGuestSessionHiddenFromClients pins the "guest chatter stays out of the
// owner's UI" contract: sessions created through a share link must not appear
// in chat.list_sessions, and none of their traffic (session_created / status /
// update events, responses to guest requests, finished-turn notifications)
// may reach the account's regular clients — while the guest itself still
// receives everything for its own session.
func TestGuestSessionHiddenFromClients(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	pc := pairingclient.New(hs.URL)
	init, _ := pc.Init(ctx, "daemon", "lap", "")
	claim, _ := pc.Claim(ctx, init.Code, "client", "phone", "")
	dc, _ := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond)

	relayURL := strings.Replace(hs.URL, "http", "ws", 1)
	dcli := relayclient.New(relayclient.Config{
		RelayURL: relayURL, AccessToken: dc.AccessToken,
		PingInterval: time.Second, HandshakeTimeout: 3 * time.Second,
	}, logger)
	// The mock emits an agent_message_chunk update during every prompt, so
	// there is real guest-session traffic to (not) observe.
	_, stdout, stdin := newChatMockACP(t)
	cr := chatrouter.New(dcli, logger, chatrouter.Builders{chatrouter.AgentDevin: func(opts acpagent.Options) (*acpagent.Adapter, error) {
		return acpagent.NewWithStreams(stdin, stdout, opts), nil
	}}, "/tmp")
	t.Cleanup(func() { _ = cr.Close() })
	go func() { _ = dcli.Run(ctx) }()
	if err := dcli.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}

	// Owner client connection — it must stay completely silent throughout.
	ownerWS, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+claim.Credentials.AccessToken, nil)
	if err != nil {
		t.Fatalf("owner dial: %v", err)
	}
	t.Cleanup(func() { _ = ownerWS.Close(websocket.StatusNormalClosure, "") })
	ownerCh := make(chan wire.Envelope, 256)
	go func() {
		for {
			var env wire.Envelope
			if err := wsjson.Read(ctx, ownerWS, &env); err != nil {
				return
			}
			ownerCh <- env
		}
	}()

	// Guest: create a session and run a full prompt turn.
	guestToken := createGuestShareToken(t, ctx, hs.URL, claim.Credentials.AccessToken, "/tmp", "")
	guestWS, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+guestToken, nil)
	if err != nil {
		t.Fatalf("guest dial: %v", err)
	}
	t.Cleanup(func() { _ = guestWS.Close(websocket.StatusNormalClosure, "") })
	guestCh := make(chan wire.Envelope, 256)
	go func() {
		for {
			var env wire.Envelope
			if err := wsjson.Read(ctx, guestWS, &env); err != nil {
				return
			}
			guestCh <- env
		}
	}()
	var guestEvents []wire.Envelope
	awaitGuest := func(id string) wire.Envelope {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case env := <-guestCh:
				if env.ID == id && env.Type == wire.TypeResponse {
					return env
				}
				if env.Type == wire.TypeEvent {
					guestEvents = append(guestEvents, env)
				}
			case <-deadline:
				t.Fatalf("timeout waiting for guest response %s", id)
				return wire.Envelope{}
			}
		}
	}

	req, _ := wire.MakeRequest("c-1", wire.ChannelChat, wire.ChatMethodCreateSession,
		wire.ChatCreateSessionRequest{AgentID: "devin"})
	_ = wsjson.Write(ctx, guestWS, req)
	res := awaitGuest("c-1")
	if res.Error != nil {
		t.Fatalf("guest create_session error: %+v", res.Error)
	}
	var createRes wire.ChatCreateSessionResponse
	if err := json.Unmarshal(res.Payload, &createRes); err != nil {
		t.Fatalf("create payload: %v", err)
	}
	guestSession := createRes.Session.ID
	if createRes.Session.Origin != "guest" {
		t.Fatalf("guest session should carry origin=guest, got %q", createRes.Session.Origin)
	}

	req, _ = wire.MakeRequest("p-1", wire.ChannelChat, wire.ChatMethodPrompt,
		wire.ChatPromptRequest{SessionID: guestSession, Text: "hello"})
	_ = wsjson.Write(ctx, guestWS, req)
	if res = awaitGuest("p-1"); res.Error != nil {
		t.Fatalf("guest prompt error: %+v", res.Error)
	}

	// The guest must still receive its own session's update events (the
	// Origin stamp routes traffic, it must not break guest delivery).
	sawUpdate := false
	for _, env := range guestEvents {
		if env.Method == wire.ChatEventUpdate && strings.Contains(string(env.Payload), guestSession) {
			sawUpdate = true
		}
	}
	if !sawUpdate {
		t.Fatal("guest did not receive its own chat.update events")
	}

	// Owner barrier + assertions: list_sessions answers AFTER the daemon
	// already emitted all guest traffic (single ordered socket), so by the
	// time the response arrives, any leaked guest envelope would already be
	// in ownerCh ahead of it.
	req, _ = wire.MakeRequest("l-1", wire.ChannelChat, wire.ChatMethodListSessions, map[string]any{})
	_ = wsjson.Write(ctx, ownerWS, req)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case env := <-ownerCh:
			if env.ID == "l-1" && env.Type == wire.TypeResponse {
				var list wire.ChatListSessionsResponse
				if err := json.Unmarshal(env.Payload, &list); err != nil {
					t.Fatalf("list payload: %v", err)
				}
				if len(list.Sessions) != 0 {
					t.Fatalf("guest session leaked into owner list_sessions: %+v", list.Sessions)
				}
				return // success: nothing leaked before the barrier either
			}
			t.Fatalf("owner received guest-session traffic: type=%s channel=%s method=%s payload=%s",
				env.Type, env.Channel, env.Method, string(env.Payload))
		case <-deadline:
			t.Fatal("timeout waiting for owner list_sessions response")
		}
	}
}

func createGuestShareToken(t *testing.T, ctx context.Context, baseURL, bearer, cwd, model string) string {
	t.Helper()
	token, _ := createGuestShare(t, ctx, baseURL, bearer, cwd, model)
	return token
}

func createGuestShare(t *testing.T, ctx context.Context, baseURL, bearer, cwd, model string) (token, shareID string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"ttl_seconds": 600, "prompt_limit": 5, "max_sessions": 1, "cwd": cwd, "model": model,
	})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/share/ask", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	httpReq.Header.Set("authorization", "Bearer "+bearer)
	httpReq.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("share request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("share status %d: %s", resp.StatusCode, b)
	}
	var out struct {
		Token   string `json:"token"`
		ShareID string `json:"share_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode share: %v", err)
	}
	if out.Token == "" {
		t.Fatal("empty guest token from share endpoint")
	}
	return out.Token, out.ShareID
}

// TestGuestRejectedWithCloseReason pins the guest rejection UX contract:
// browsers can't see the HTTP status of a failed WS handshake, so the relay
// must accept rejected guests and immediately close with application code
// 4401 plus a short reason ("share revoked", "token expired or invalid", …)
// that the share page surfaces instead of reconnect-looping forever.
func TestGuestRejectedWithCloseReason(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pc := pairingclient.New(hs.URL)
	init, _ := pc.Init(ctx, "daemon", "lap", "")
	claim, _ := pc.Claim(ctx, init.Code, "client", "phone", "")
	if _, err := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	relayURL := strings.Replace(hs.URL, "http", "ws", 1)

	expectClose := func(token, wantReason string) {
		t.Helper()
		ws, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+token, nil)
		if err != nil {
			t.Fatalf("dial should upgrade before rejecting, got: %v", err)
		}
		defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()
		_, _, rerr := ws.Read(ctx)
		if rerr == nil {
			t.Fatal("expected the relay to close the rejected guest socket")
		}
		if got := websocket.CloseStatus(rerr); got != websocket.StatusCode(4401) {
			t.Fatalf("close status: got %d want 4401 (err %v)", got, rerr)
		}
		var ce websocket.CloseError
		if !errors.As(rerr, &ce) || ce.Reason != wantReason {
			t.Fatalf("close reason: got %q want %q", ce.Reason, wantReason)
		}
	}

	// Case 1: a revoked share → "share revoked".
	token, shareID := createGuestShare(t, ctx, hs.URL, claim.Credentials.AccessToken, "/tmp", "")
	revokeReq, _ := http.NewRequestWithContext(ctx, http.MethodDelete, hs.URL+"/v1/share/"+shareID, nil)
	revokeReq.Header.Set("authorization", "Bearer "+claim.Credentials.AccessToken)
	revokeResp, err := http.DefaultClient.Do(revokeReq)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	_ = revokeResp.Body.Close()
	if revokeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke status %d", revokeResp.StatusCode)
	}
	expectClose(token, "share revoked")

	// Case 2: an expired guest token (signed with the test server's secret) →
	// the auth layer rejects it before claims are trusted, but the unverified
	// kind peek routes it onto the same close-frame path.
	signer, err := auth.NewSigner([]byte("0123456789abcdef0123456789abcdef"), "coding-anywhere/relay", nil)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := signer.Issue("acc-x", "share-x", auth.DeviceGuest, -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	expectClose(expired, "token expired or invalid")

	// Case 3: a syntactically valid guest token whose share never existed →
	// "unknown share" (e.g. the relay database was reset).
	ghost, err := signer.Issue("acc-x", "share-ghost", auth.DeviceGuest, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	expectClose(ghost, "unknown share")
}
