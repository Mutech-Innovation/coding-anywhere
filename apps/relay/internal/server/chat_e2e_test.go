package server_test

// End-to-end test for the `chat` channel.
//
//  raw WS client ─ chat.create_session ──▶ relay ──▶ daemon (relayclient + chatrouter)
//                                                                │
//                                                                ▼
//                                                        mock ACP "devin"
//                                                            (in-process)
//
// The mock pushes session/update notifications and a session/request_permission
// during the prompt; the test asserts those reach the client as
// `chat.update` / `chat.permission_request` events, then the client resolves
// the permission via `chat.resolve_permission` and the mock observes the
// decision over the ACP transport.

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/agent/devin"
	"github.com/Mutech-Innovation/coding-anywhere/internal/chatrouter"
	"github.com/Mutech-Innovation/coding-anywhere/internal/pairingclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// ---------------- mock ACP server ----------------

type chatMockACP struct {
	t  *testing.T
	in *io.PipeReader  // adapter→server
	wr *bufio.Writer   // server→adapter
	mu sync.Mutex

	respMu      sync.Mutex
	respWaiters map[string]chan map[string]any // id (as string) → channel
}

func newChatMockACP(t *testing.T) (*chatMockACP, *io.PipeReader, *io.PipeWriter) {
	t.Helper()
	adapterStdinR, adapterStdinW := io.Pipe()  // adapter writes here, server reads
	adapterStdoutR, adapterStdoutW := io.Pipe() // server writes here, adapter reads
	m := &chatMockACP{
		t:           t,
		in:          adapterStdinR,
		wr:          bufio.NewWriter(adapterStdoutW),
		respWaiters: map[string]chan map[string]any{},
	}
	go m.run()
	return m, adapterStdoutR, adapterStdinW
}

// awaitResponse registers a waiter for a response with the given id (the id
// the mock used when sending a request to the adapter). Must be called
// before the request is written so the response can't race past it.
func (m *chatMockACP) awaitResponse(id any) <-chan map[string]any {
	ch := make(chan map[string]any, 1)
	m.respMu.Lock()
	m.respWaiters[idToString(id)] = ch
	m.respMu.Unlock()
	return ch
}

func idToString(id any) string {
	switch v := id.(type) {
	case string:
		return v
	case float64:
		return jsonNumber(v)
	default:
		buf, _ := json.Marshal(id)
		return string(buf)
	}
}

func jsonNumber(f float64) string {
	buf, _ := json.Marshal(f)
	return string(buf)
}

func (m *chatMockACP) run() {
	dec := json.NewDecoder(m.in)
	for {
		var msg map[string]any
		if err := dec.Decode(&msg); err != nil {
			return
		}
		method, _ := msg["method"].(string)
		idVal := msg["id"]
		switch {
		case method == "initialize":
			m.respond(idVal, map[string]any{
				"protocolVersion":   1,
				"agentCapabilities": map[string]any{"loadSession": false},
				"agentInfo":         map[string]any{"name": "mock-devin", "version": "0.0.1"},
			})
		case method == "session/new":
			m.respond(idVal, map[string]any{"sessionId": "sess-acp-1"})
		case method == "session/prompt":
			// Run prompt handling in its own goroutine so the read loop
			// keeps consuming subsequent messages (notably the
			// session/request_permission response).
			go m.handlePrompt(idVal)
		case method == "session/cancel":
			// notification, no reply
		case method != "":
			// Unknown request from adapter — return method not found if it
			// has an id.
			if idVal != nil {
				m.respondError(idVal, -32601, "unknown method "+method)
			}
		case idVal != nil:
			// Response (no method); deliver to waiter if one is registered.
			m.respMu.Lock()
			ch, ok := m.respWaiters[idToString(idVal)]
			if ok {
				delete(m.respWaiters, idToString(idVal))
			}
			m.respMu.Unlock()
			if ok {
				ch <- msg
			}
		}
	}
}

func (m *chatMockACP) handlePrompt(promptID any) {
	// Push a notification so the chatrouter sees an update event.
	m.notify("session/update", map[string]any{
		"sessionId": "sess-acp-1",
		"update": map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": "thinking..."},
		},
	})

	// Send a session/request_permission and wait for the response.
	const permID = 9001
	wait := m.awaitResponse(permID)
	m.write(map[string]any{
		"jsonrpc": "2.0", "id": permID,
		"method": "session/request_permission",
		"params": map[string]any{
			"sessionId": "sess-acp-1",
			"toolCall":  map[string]any{"name": "edit_file", "input": map[string]any{"path": "/x"}},
		},
	})

	select {
	case resp := <-wait:
		m.t.Logf("mock saw permission decision: %+v", resp)
	case <-time.After(5 * time.Second):
		m.t.Errorf("mock didn't see permission decision in time")
		m.respond(promptID, map[string]any{"stopReason": "error"})
		return
	}
	m.respond(promptID, map[string]any{"stopReason": "end_turn"})
}

func (m *chatMockACP) respond(id any, result map[string]any) {
	m.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (m *chatMockACP) respondError(id any, code int, msg string) {
	m.write(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
}

func (m *chatMockACP) notify(method string, params map[string]any) {
	m.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (m *chatMockACP) write(v any) {
	buf, _ := json.Marshal(v)
	m.mu.Lock()
	defer m.mu.Unlock()
	_, _ = m.wr.Write(buf)
	_, _ = m.wr.Write([]byte("\n"))
	_ = m.wr.Flush()
}

// ---------------- the test ----------------

func TestChatEndToEnd(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// 1. Pair.
	pc := pairingclient.New(hs.URL)
	init, err := pc.Init(ctx, "daemon", "test-laptop", "")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	claim, err := pc.Claim(ctx, init.Code, "client", "test-browser", "")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	clientCred := claim.Credentials
	daemonCred, err := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitClaimed: %v", err)
	}

	// 2. Daemon side: relay client + chatrouter with a stream-injected adapter.
	relayURL := strings.Replace(hs.URL, "http", "ws", 1)
	dc := relayclient.New(relayclient.Config{
		RelayURL: relayURL, AccessToken: daemonCred.AccessToken,
		PingInterval: time.Second, HandshakeTimeout: 3 * time.Second,
	}, logger)
	mock, stdout, stdin := newChatMockACP(t)
	cr := chatrouter.New(dc, logger, func(opts devin.Options) (*devin.Adapter, error) {
		return devin.NewWithStreams(stdin, stdout, opts), nil
	})
	t.Cleanup(func() { _ = cr.Close() })

	go func() { _ = dc.Run(ctx) }()
	if err := dc.WaitConnected(ctx); err != nil {
		t.Fatalf("daemon WaitConnected: %v", err)
	}

	// 3. Client raw WS.
	clientWS, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+clientCred.AccessToken, nil)
	if err != nil {
		t.Fatalf("client Dial: %v", err)
	}
	t.Cleanup(func() { _ = clientWS.Close(websocket.StatusNormalClosure, "") })

	type recv struct {
		env wire.Envelope
		err error
	}
	envCh := make(chan recv, 64)
	go func() {
		for {
			var env wire.Envelope
			if err := wsjson.Read(ctx, clientWS, &env); err != nil {
				envCh <- recv{err: err}
				return
			}
			envCh <- recv{env: env}
		}
	}()

	awaitResp := func(id string) wire.Envelope {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case r := <-envCh:
				if r.err != nil {
					t.Fatalf("recv err: %v", r.err)
				}
				if r.env.ID == id && r.env.Type == wire.TypeResponse {
					return r.env
				}
			case <-deadline:
				t.Fatal("timeout waiting for response", id)
			}
		}
	}

	// Helper: wait for a chat event with a given method.
	awaitEvent := func(method string) wire.Envelope {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case r := <-envCh:
				if r.err != nil {
					t.Fatalf("recv err: %v", r.err)
				}
				if r.env.Channel == wire.ChannelChat && r.env.Method == method {
					return r.env
				}
			case <-deadline:
				t.Fatalf("timeout waiting for chat event %q", method)
			}
		}
	}

	// 4. Create session.
	createReq, _ := wire.MakeRequest("c-1", wire.ChannelChat, wire.ChatMethodCreateSession,
		wire.ChatCreateSessionRequest{AgentID: "devin", Cwd: "/Users/me/app"})
	if err := wsjson.Write(ctx, clientWS, createReq); err != nil {
		t.Fatalf("create write: %v", err)
	}
	res := awaitResp("c-1")
	if res.Error != nil {
		t.Fatalf("create error: %+v", res.Error)
	}
	var createRes wire.ChatCreateSessionResponse
	if err := json.Unmarshal(res.Payload, &createRes); err != nil {
		t.Fatalf("create payload: %v", err)
	}
	sessionID := createRes.Session.ID
	if sessionID == "" || createRes.Session.NativeID != "sess-acp-1" {
		t.Fatalf("create response: %+v", createRes)
	}

	// 5. Send a prompt.
	promptReq, _ := wire.MakeRequest("p-1", wire.ChannelChat, wire.ChatMethodPrompt,
		wire.ChatPromptRequest{SessionID: sessionID, Text: "hello"})
	if err := wsjson.Write(ctx, clientWS, promptReq); err != nil {
		t.Fatalf("prompt write: %v", err)
	}

	// 6. Expect chat.update with the agent_message_chunk.
	upd := awaitEvent(wire.ChatEventUpdate)
	if !strings.Contains(string(upd.Payload), "agent_message_chunk") {
		t.Fatalf("update payload missing chunk: %s", string(upd.Payload))
	}

	// 7. Expect chat.permission_request, then resolve it.
	perm := awaitEvent(wire.ChatEventPermissionRequest)
	var permEv wire.ChatPermissionRequestEvent
	if err := json.Unmarshal(perm.Payload, &permEv); err != nil {
		t.Fatalf("permission payload: %v", err)
	}
	if permEv.SessionID != sessionID || permEv.RequestID == "" {
		t.Fatalf("permission event: %+v", permEv)
	}

	resolveReq, _ := wire.MakeRequest("r-1", wire.ChannelChat, wire.ChatMethodResolvePermit,
		wire.ChatResolvePermissionRequest{
			RequestID: permEv.RequestID,
			Decision:  json.RawMessage(`{"decision":"allow"}`),
		})
	if err := wsjson.Write(ctx, clientWS, resolveReq); err != nil {
		t.Fatalf("resolve write: %v", err)
	}
	awaitResp("r-1")

	// 8. Mock should receive the permission decision back through ACP, then
	// reply to session/prompt with end_turn → client gets the prompt response.
	res = awaitResp("p-1")
	if res.Error != nil {
		t.Fatalf("prompt error: %+v", res.Error)
	}
	var promptRes wire.ChatPromptResponse
	if err := json.Unmarshal(res.Payload, &promptRes); err != nil {
		t.Fatalf("prompt payload: %v", err)
	}
	if promptRes.StopReason != "end_turn" {
		t.Fatalf("stop reason: %s", promptRes.StopReason)
	}
	_ = mock
}

func TestChatAgentsLists(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
	cr := chatrouter.New(dcli, logger, func(opts devin.Options) (*devin.Adapter, error) {
		// Never used because chat.agents doesn't actually start the adapter.
		return nil, nil
	})
	t.Cleanup(func() { _ = cr.Close() })
	go func() { _ = dcli.Run(ctx) }()
	if err := dcli.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}

	clientWS, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+claim.Credentials.AccessToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientWS.Close(websocket.StatusNormalClosure, "") })

	req, _ := wire.MakeRequest("a-1", wire.ChannelChat, wire.ChatMethodAgents, struct{}{})
	if err := wsjson.Write(ctx, clientWS, req); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("no response within 5s")
		default:
			var env wire.Envelope
			if err := wsjson.Read(ctx, clientWS, &env); err != nil {
				t.Fatalf("read: %v", err)
			}
			if env.ID != "a-1" || env.Type != wire.TypeResponse {
				continue
			}
			var ar wire.ChatAgentsResponse
			if err := json.Unmarshal(env.Payload, &ar); err != nil {
				t.Fatalf("payload: %v", err)
			}
			if len(ar.Agents) < 4 {
				t.Fatalf("expected 4 agents, got %d", len(ar.Agents))
			}
			if ar.Agents[0].ID != "devin" || !ar.Agents[0].Available {
				t.Fatalf("first agent: %+v", ar.Agents[0])
			}
			return
		}
	}
}
