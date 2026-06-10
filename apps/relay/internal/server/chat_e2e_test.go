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

	"github.com/Mutech-Innovation/coding-anywhere/internal/agent/acpagent"
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
	in *io.PipeReader // adapter→server
	wr *bufio.Writer  // server→adapter
	mu sync.Mutex

	respMu      sync.Mutex
	respWaiters map[string]chan map[string]any // id (as string) → channel
	lastNewCwd  string
}

func newChatMockACP(t *testing.T) (*chatMockACP, *io.PipeReader, *io.PipeWriter) {
	t.Helper()
	adapterStdinR, adapterStdinW := io.Pipe()   // adapter writes here, server reads
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
			params, _ := msg["params"].(map[string]any)
			m.mu.Lock()
			m.lastNewCwd, _ = params["cwd"].(string)
			m.mu.Unlock()
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
	cr := chatrouter.New(dc, logger, chatrouter.Builders{chatrouter.AgentDevin: func(opts acpagent.Options) (*acpagent.Adapter, error) {
		return acpagent.NewWithStreams(stdin, stdout, opts), nil
	}}, "/Users/me/app")
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
		wire.ChatCreateSessionRequest{AgentID: "devin"})
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
	if createRes.Session.Cwd != "/Users/me/app" {
		t.Fatalf("response cwd: %s", createRes.Session.Cwd)
	}
	mock.mu.Lock()
	if mock.lastNewCwd != "/Users/me/app" {
		mock.mu.Unlock()
		t.Fatalf("cwd: %s", mock.lastNewCwd)
	}
	mock.mu.Unlock()

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
}

// TestChatHistoryAndSetMode exercises the persistence + mode controls added
// in P4 deepening. It wires up a mock that returns initial modes from
// session/new, pushes a sequence of session/update notifications, and
// asserts that:
//   - chat.create_session surfaces the modes,
//   - chat.set_mode round-trips into ACP session/set_mode,
//   - chat.get_history returns the prompts and updates we recorded.
func TestChatHistoryAndSetMode(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	pc := pairingclient.New(hs.URL)
	init, _ := pc.Init(ctx, "daemon", "lap", "")
	claim, _ := pc.Claim(ctx, init.Code, "client", "phone", "")
	dc, _ := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond)

	mock := &snapshotMockACP{
		t:           t,
		respWaiters: map[string]chan map[string]any{},
		modes: map[string]any{
			"currentModeId": "ask",
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

	clientWS, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+claim.Credentials.AccessToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientWS.Close(websocket.StatusNormalClosure, "") })

	envCh := make(chan wire.Envelope, 64)
	go func() {
		for {
			var env wire.Envelope
			if err := wsjson.Read(ctx, clientWS, &env); err != nil {
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
	awaitEvent := func(method string, sessionID string) wire.Envelope {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case env := <-envCh:
				if env.Channel == wire.ChannelChat && env.Method == method &&
					(sessionID == "" || strings.Contains(string(env.Payload), sessionID)) {
					return env
				}
			case <-deadline:
				t.Fatalf("timeout waiting for chat event %q", method)
			}
		}
	}

	// 1. create_session — modes must come back.
	req, _ := wire.MakeRequest("c-1", wire.ChannelChat, wire.ChatMethodCreateSession,
		wire.ChatCreateSessionRequest{AgentID: "devin", Cwd: "/tmp"})
	_ = wsjson.Write(ctx, clientWS, req)
	res := awaitResp("c-1")
	var createRes wire.ChatCreateSessionResponse
	if err := json.Unmarshal(res.Payload, &createRes); err != nil {
		t.Fatalf("create payload: %v", err)
	}
	sessionID := createRes.Session.ID
	if len(createRes.AvailableModes) != 2 || createRes.CurrentModeID != "ask" {
		t.Fatalf("modes not surfaced: %+v", createRes)
	}

	// 2. set_mode — mock must see the call.
	req, _ = wire.MakeRequest("m-1", wire.ChannelChat, wire.ChatMethodSetMode,
		wire.ChatSetModeRequest{SessionID: sessionID, ModeID: "code"})
	_ = wsjson.Write(ctx, clientWS, req)
	res = awaitResp("m-1")
	if res.Error != nil {
		t.Fatalf("set_mode error: %+v", res.Error)
	}
	mock.mu.Lock()
	if mock.lastSetModeID != "code" {
		mock.mu.Unlock()
		t.Fatalf("mock didn't see set_mode: %s", mock.lastSetModeID)
	}
	mock.mu.Unlock()

	// 3. send a prompt; mock pushes two updates and a current_mode_update.
	req, _ = wire.MakeRequest("p-1", wire.ChannelChat, wire.ChatMethodPrompt,
		wire.ChatPromptRequest{SessionID: sessionID, Text: "hello"})
	_ = wsjson.Write(ctx, clientWS, req)
	awaitEvent(wire.ChatEventUpdate, sessionID) // chunk #1
	awaitEvent(wire.ChatEventUpdate, sessionID) // chunk #2 / mode update
	awaitResp("p-1")

	// 4. get_history: must contain >=2 updates + 1 prompt + the current mode.
	req, _ = wire.MakeRequest("h-1", wire.ChannelChat, wire.ChatMethodGetHistory,
		wire.ChatGetHistoryRequest{SessionID: sessionID})
	_ = wsjson.Write(ctx, clientWS, req)
	res = awaitResp("h-1")
	var hist wire.ChatGetHistoryResponse
	if err := json.Unmarshal(res.Payload, &hist); err != nil {
		t.Fatalf("history payload: %v", err)
	}
	if len(hist.Updates) < 2 {
		t.Fatalf("expected >=2 updates in history, got %d", len(hist.Updates))
	}
	if len(hist.UserPrompts) != 1 || hist.UserPrompts[0].Text != "hello" {
		t.Fatalf("user prompts: %+v", hist.UserPrompts)
	}
	if hist.CurrentModeID != "code" {
		t.Fatalf("expected mode 'code' after current_mode_update, got %q", hist.CurrentModeID)
	}
}

// snapshotMockACP is a slimmer ACP mock used by TestChatHistoryAndSetMode.
type snapshotMockACP struct {
	t  *testing.T
	in *io.PipeReader
	wr *bufio.Writer

	mu      sync.Mutex
	writeMu sync.Mutex
	modes   map[string]any
	// earlyNotifs, if set, are fired as session/update notifications
	// *before* responding to session/new. Used to reproduce the
	// real-world Devin behaviour where configOptions /
	// availableCommands arrive while session/new is still in flight.
	earlyNotifs        []map[string]any
	lastSetModeID      string
	lastSetConfigID    string
	lastSetConfigValue string
	respWaiters        map[string]chan map[string]any
}

func (m *snapshotMockACP) run() {
	dec := json.NewDecoder(m.in)
	for {
		var msg map[string]any
		if err := dec.Decode(&msg); err != nil {
			return
		}
		method, _ := msg["method"].(string)
		idVal := msg["id"]
		switch method {
		case "initialize":
			m.respond(idVal, map[string]any{
				"protocolVersion":   1,
				"agentCapabilities": map[string]any{"loadSession": false},
				"agentInfo":         map[string]any{"name": "mock", "version": "0"},
			})
		case "session/new":
			result := map[string]any{"sessionId": "sess-acp-snap"}
			m.mu.Lock()
			modes := m.modes
			early := m.earlyNotifs
			m.mu.Unlock()
			if modes != nil {
				result["modes"] = modes
			}
			// Fire any early-update notifications synchronously before
			// answering session/new — same ordering as real Devin.
			for _, body := range early {
				m.notify("session/update", map[string]any{
					"sessionId": "sess-acp-snap",
					"update":    body,
				})
			}
			m.respond(idVal, result)
		case "session/set_mode":
			params, _ := msg["params"].(map[string]any)
			m.mu.Lock()
			m.lastSetModeID, _ = params["modeId"].(string)
			m.mu.Unlock()
			m.respond(idVal, map[string]any{})
		case "session/set_config_option":
			params, _ := msg["params"].(map[string]any)
			cfg, _ := params["configId"].(string)
			val, _ := params["value"].(string)
			m.mu.Lock()
			m.lastSetConfigID = cfg
			m.lastSetConfigValue = val
			m.mu.Unlock()
			// Real Devin echoes the new full configOptions snapshot in
			// the result body and (notably) does NOT emit a follow-up
			// session/update — the chatrouter must synthesise that
			// itself. Mirror exactly that behaviour here.
			m.respond(idVal, map[string]any{
				"configOptions": []map[string]any{
					{
						"id":           cfg,
						"name":         "Model",
						"type":         "select",
						"currentValue": val,
					},
				},
			})
		case "session/prompt":
			go func() {
				m.notify("session/update", map[string]any{
					"sessionId": "sess-acp-snap",
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"content":       map[string]any{"type": "text", "text": "hi"},
					},
				})
				m.notify("session/update", map[string]any{
					"sessionId": "sess-acp-snap",
					"update": map[string]any{
						"sessionUpdate": "current_mode_update",
						"currentModeId": "code",
					},
				})
				m.respond(idVal, map[string]any{"stopReason": "end_turn"})
			}()
		case "session/cancel":
			// notification, no reply
		}
	}
}

func (m *snapshotMockACP) respond(id any, result map[string]any) {
	m.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (m *snapshotMockACP) notify(method string, params map[string]any) {
	m.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (m *snapshotMockACP) write(v any) {
	buf, _ := json.Marshal(v)
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	_, _ = m.wr.Write(buf)
	_, _ = m.wr.Write([]byte("\n"))
	_ = m.wr.Flush()
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
	cr := chatrouter.New(dcli, logger, chatrouter.Builders{chatrouter.AgentDevin: func(opts acpagent.Options) (*acpagent.Adapter, error) {
		// Never used because chat.agents doesn't actually start the adapter.
		return nil, nil
	}})
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

// TestChatCreateSessionPendingUpdates pins down the buffering fix for
// session/update notifications that arrive while session/new is still in
// flight. In production Devin emits config_option_update +
// available_commands_update before answering session/new; before the fix
// they were dropped (no daemon-side session record yet) and the web UI
// never received configOptions, hiding the model picker.
//
// The mock ACP server here mimics that ordering: an
// `available_commands_update` and a `config_option_update` are fired
// *before* the session/new response. The chatrouter must buffer them and
// ship them inline with the create_session response as `pending_updates`.
func TestChatCreateSessionPendingUpdates(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	pc := pairingclient.New(hs.URL)
	init, _ := pc.Init(ctx, "daemon", "lap", "")
	claim, _ := pc.Claim(ctx, init.Code, "client", "phone", "")
	dc, _ := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond)

	mock := &snapshotMockACP{
		t:           t,
		respWaiters: map[string]chan map[string]any{},
		earlyNotifs: []map[string]any{
			{
				"sessionUpdate": "config_option_update",
				"configOptions": []map[string]any{
					{
						"id":           "model",
						"name":         "Model",
						"type":         "select",
						"currentValue": "swe-1-6-fast",
						"options": []map[string]any{
							{"value": "swe-1-6-fast", "name": "SWE-1.6 Fast"},
							{"value": "claude-opus-4-7-max", "name": "Claude Opus 4.7 Max"},
						},
					},
				},
			},
			{
				"sessionUpdate": "available_commands_update",
				"availableCommands": []map[string]any{
					{"name": "compact", "description": "Force conversation compaction"},
				},
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

	clientWS, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+claim.Credentials.AccessToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientWS.Close(websocket.StatusNormalClosure, "") })

	req, _ := wire.MakeRequest("c-1", wire.ChannelChat, wire.ChatMethodCreateSession,
		wire.ChatCreateSessionRequest{AgentID: "devin", Cwd: "/tmp"})
	if err := wsjson.Write(ctx, clientWS, req); err != nil {
		t.Fatal(err)
	}

	// Wait for the create_session response; tolerate the broadcast
	// session_created event arriving on the same socket.
	deadline := time.After(8 * time.Second)
	var res wire.Envelope
	for {
		select {
		case <-deadline:
			t.Fatal("no create_session response within 8s")
		default:
		}
		var env wire.Envelope
		if err := wsjson.Read(ctx, clientWS, &env); err != nil {
			t.Fatalf("read: %v", err)
		}
		if env.ID == "c-1" && env.Type == wire.TypeResponse {
			res = env
			break
		}
	}
	if res.Error != nil {
		t.Fatalf("create_session error: %+v", res.Error)
	}
	var createRes wire.ChatCreateSessionResponse
	if err := json.Unmarshal(res.Payload, &createRes); err != nil {
		t.Fatalf("create payload: %v", err)
	}
	if len(createRes.PendingUpdates) != 2 {
		t.Fatalf("expected 2 pending updates (config_option_update, available_commands_update), got %d: %s",
			len(createRes.PendingUpdates), string(res.Payload))
	}
	kinds := make([]string, 0, len(createRes.PendingUpdates))
	for _, u := range createRes.PendingUpdates {
		var head struct {
			Kind string `json:"sessionUpdate"`
		}
		if err := json.Unmarshal(u, &head); err != nil {
			t.Fatalf("decode pending update: %v", err)
		}
		kinds = append(kinds, head.Kind)
	}
	wantKinds := []string{"config_option_update", "available_commands_update"}
	for i, want := range wantKinds {
		if i >= len(kinds) || kinds[i] != want {
			t.Fatalf("pending update kinds = %v, want %v", kinds, wantKinds)
		}
	}

	// Also verify that the daemon's snapshot picked these up, so a
	// subsequent get_history replays them too (page reload path).
	req, _ = wire.MakeRequest("h-1", wire.ChannelChat, wire.ChatMethodGetHistory,
		wire.ChatGetHistoryRequest{SessionID: createRes.Session.ID})
	if err := wsjson.Write(ctx, clientWS, req); err != nil {
		t.Fatal(err)
	}
	deadline = time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("no get_history response within 5s")
		default:
		}
		var env wire.Envelope
		if err := wsjson.Read(ctx, clientWS, &env); err != nil {
			t.Fatalf("read: %v", err)
		}
		if env.ID != "h-1" || env.Type != wire.TypeResponse {
			continue
		}
		var hist wire.ChatGetHistoryResponse
		if err := json.Unmarshal(env.Payload, &hist); err != nil {
			t.Fatalf("history payload: %v", err)
		}
		if len(hist.Updates) < 2 {
			t.Fatalf("expected at least 2 updates in history, got %d", len(hist.Updates))
		}
		return
	}
}

// TestChatSetConfigOptionFanOut covers the snapshot/broadcast fan-out
// after chat.set_config_option. Devin's ACP server doesn't volunteer a
// `config_option_update` notification of its own — it just stuffs the
// new configOptions snapshot into the response body — so the chatrouter
// must synthesise an update event from that response and feed it through
// both the per-session snapshot (for chat.get_history replay) and the
// live broadcast (for other connected clients). Without this, the model
// the user picked via the picker would silently revert the next time
// they opened the session in a new tab.
func TestChatSetConfigOptionFanOut(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	pc := pairingclient.New(hs.URL)
	init, _ := pc.Init(ctx, "daemon", "lap", "")
	claim, _ := pc.Claim(ctx, init.Code, "client", "phone", "")
	dc, _ := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond)

	mock := &snapshotMockACP{
		t:           t,
		respWaiters: map[string]chan map[string]any{},
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

	clientWS, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+claim.Credentials.AccessToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientWS.Close(websocket.StatusNormalClosure, "") })

	envCh := make(chan wire.Envelope, 64)
	go func() {
		for {
			var env wire.Envelope
			if err := wsjson.Read(ctx, clientWS, &env); err != nil {
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
	awaitChatUpdateContaining := func(needle string) wire.ChatUpdateEvent {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case env := <-envCh:
				if env.Channel != wire.ChannelChat || env.Method != wire.ChatEventUpdate {
					continue
				}
				if !strings.Contains(string(env.Payload), needle) {
					continue
				}
				var ev wire.ChatUpdateEvent
				if err := json.Unmarshal(env.Payload, &ev); err != nil {
					t.Fatalf("update payload: %v", err)
				}
				return ev
			case <-deadline:
				t.Fatalf("timeout waiting for chat update containing %q", needle)
			}
		}
	}

	// 1. Create a session.
	req, _ := wire.MakeRequest("c-1", wire.ChannelChat, wire.ChatMethodCreateSession,
		wire.ChatCreateSessionRequest{AgentID: "devin", Cwd: "/tmp"})
	_ = wsjson.Write(ctx, clientWS, req)
	res := awaitResp("c-1")
	var createRes wire.ChatCreateSessionResponse
	if err := json.Unmarshal(res.Payload, &createRes); err != nil {
		t.Fatalf("create payload: %v", err)
	}
	sessionID := createRes.Session.ID

	// 2. Switch the model via chat.set_config_option.
	req, _ = wire.MakeRequest("sc-1", wire.ChannelChat, wire.ChatMethodSetConfigOption,
		wire.ChatSetConfigOptionRequest{
			SessionID: sessionID,
			ConfigID:  "model",
			Value:     "claude-opus-4-7-max",
		})
	_ = wsjson.Write(ctx, clientWS, req)

	// The router should both:
	//   (a) broadcast a synthetic `config_option_update` event so other
	//       connected clients converge in real time,
	//   (b) include that update in the session snapshot so a fresh
	//       chat.get_history sees it.
	ev := awaitChatUpdateContaining("config_option_update")
	if !strings.Contains(string(ev.Update), "claude-opus-4-7-max") {
		t.Fatalf("synthetic update missing new model: %s", string(ev.Update))
	}
	if ev.SessionID != sessionID {
		t.Fatalf("synthetic update had wrong session id: %s vs %s", ev.SessionID, sessionID)
	}
	awaitResp("sc-1")

	// Verify the mock saw the right ACP call.
	mock.mu.Lock()
	if mock.lastSetConfigID != "model" || mock.lastSetConfigValue != "claude-opus-4-7-max" {
		mock.mu.Unlock()
		t.Fatalf("mock saw wrong set_config_option: %s=%s",
			mock.lastSetConfigID, mock.lastSetConfigValue)
	}
	mock.mu.Unlock()

	// 3. chat.get_history must replay the synthetic update so a tab
	// reload still shows the model the user picked.
	req, _ = wire.MakeRequest("h-1", wire.ChannelChat, wire.ChatMethodGetHistory,
		wire.ChatGetHistoryRequest{SessionID: sessionID})
	_ = wsjson.Write(ctx, clientWS, req)
	res = awaitResp("h-1")
	var hist wire.ChatGetHistoryResponse
	if err := json.Unmarshal(res.Payload, &hist); err != nil {
		t.Fatalf("history payload: %v", err)
	}
	found := false
	for _, u := range hist.Updates {
		if strings.Contains(string(u), "config_option_update") &&
			strings.Contains(string(u), "claude-opus-4-7-max") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("config_option_update with new model not in history snapshot; got %d updates",
			len(hist.Updates))
	}
}

// cursorMockACP mimics `cursor-agent acp`: initialize advertises a
// cursor_login auth method, session/new is rejected until authenticate has
// been called, models arrive inline on the session/new response (no
// config_option_update notifications), and model switching goes through
// session/set_model.
type cursorMockACP struct {
	t  *testing.T
	in *io.PipeReader
	wr *bufio.Writer

	mu               sync.Mutex
	writeMu          sync.Mutex
	authenticated    bool
	lastAuthMethod   string
	lastSetModelID   string
	sawSetConfigCall bool
}

func (m *cursorMockACP) run() {
	dec := json.NewDecoder(m.in)
	for {
		var msg map[string]any
		if err := dec.Decode(&msg); err != nil {
			return
		}
		method, _ := msg["method"].(string)
		idVal := msg["id"]
		switch method {
		case "initialize":
			m.respond(idVal, map[string]any{
				"protocolVersion":   1,
				"agentCapabilities": map[string]any{"loadSession": true},
				"authMethods": []map[string]any{
					{"id": "cursor_login", "name": "Cursor Login"},
				},
			})
		case "authenticate":
			params, _ := msg["params"].(map[string]any)
			m.mu.Lock()
			m.lastAuthMethod, _ = params["methodId"].(string)
			m.authenticated = true
			m.mu.Unlock()
			m.respond(idVal, map[string]any{})
		case "session/new":
			m.mu.Lock()
			ok := m.authenticated
			m.mu.Unlock()
			if !ok {
				m.respondError(idVal, -32000, "Authentication required")
				continue
			}
			m.respond(idVal, map[string]any{
				"sessionId": "sess-cursor-1",
				"modes": map[string]any{
					"currentModeId": "agent",
					"availableModes": []map[string]any{
						{"id": "agent", "name": "Agent"},
						{"id": "plan", "name": "Plan"},
						{"id": "ask", "name": "Ask"},
					},
				},
				"models": map[string]any{
					"currentModelId": "composer-2.5[fast=true]",
					"availableModels": []map[string]any{
						{"modelId": "composer-2.5[fast=true]", "name": "composer-2.5"},
						{"modelId": "gpt-5.5[reasoning=medium]", "name": "gpt-5.5"},
					},
				},
			})
		case "session/set_model":
			params, _ := msg["params"].(map[string]any)
			m.mu.Lock()
			m.lastSetModelID, _ = params["modelId"].(string)
			m.mu.Unlock()
			m.respond(idVal, map[string]any{})
		case "session/set_config_option":
			// cursor-agent has no Devin-style config options; the router
			// must never forward the synthetic "model" option here.
			m.mu.Lock()
			m.sawSetConfigCall = true
			m.mu.Unlock()
			m.respondError(idVal, -32601, "unknown method")
		case "session/prompt":
			m.respond(idVal, map[string]any{"stopReason": "end_turn"})
		case "session/cancel":
			// notification, no reply
		}
	}
}

func (m *cursorMockACP) respond(id any, result map[string]any) {
	m.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (m *cursorMockACP) respondError(id any, code int, msg string) {
	m.write(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
}

func (m *cursorMockACP) write(v any) {
	buf, _ := json.Marshal(v)
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	_, _ = m.wr.Write(buf)
	_, _ = m.wr.Write([]byte("\n"))
	_ = m.wr.Flush()
}

// TestChatCursorAgentModels covers the cursor-agent integration end to end:
//   - the adapter authenticates with cursor_login during Start,
//   - the models payload from session/new is synthesised into a
//     `config_option_update` pending update so the web model picker works,
//   - chat.set_config_option on the synthetic "model" option maps onto ACP
//     session/set_model (never session/set_config_option) and re-broadcasts
//     an updated snapshot.
func TestChatCursorAgentModels(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	pc := pairingclient.New(hs.URL)
	init, _ := pc.Init(ctx, "daemon", "lap", "")
	claim, _ := pc.Claim(ctx, init.Code, "client", "phone", "")
	dc, _ := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond)

	mock := &cursorMockACP{t: t}
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
	cr := chatrouter.New(dcli, logger, chatrouter.Builders{
		chatrouter.AgentCursor: func(opts acpagent.Options) (*acpagent.Adapter, error) {
			// Mirror DefaultCursorBuilder's auth wiring on the
			// stream-injected adapter.
			opts.AuthMethodID = "cursor_login"
			return acpagent.NewWithStreams(stdinW, stdoutR, opts), nil
		},
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

	envCh := make(chan wire.Envelope, 64)
	go func() {
		for {
			var env wire.Envelope
			if err := wsjson.Read(ctx, clientWS, &env); err != nil {
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

	// 1. create_session against the cursor agent.
	req, _ := wire.MakeRequest("c-1", wire.ChannelChat, wire.ChatMethodCreateSession,
		wire.ChatCreateSessionRequest{AgentID: "cursor-cli", Cwd: "/tmp"})
	_ = wsjson.Write(ctx, clientWS, req)
	res := awaitResp("c-1")
	if res.Error != nil {
		t.Fatalf("create_session error: %+v", res.Error)
	}
	var createRes wire.ChatCreateSessionResponse
	if err := json.Unmarshal(res.Payload, &createRes); err != nil {
		t.Fatalf("create payload: %v", err)
	}
	if createRes.Session.AgentID != "cursor-cli" {
		t.Fatalf("agent id: %s", createRes.Session.AgentID)
	}
	if createRes.CurrentModeID != "agent" || len(createRes.AvailableModes) != 3 {
		t.Fatalf("modes not surfaced: %+v", createRes)
	}

	// The adapter must have authenticated before session/new succeeded.
	mock.mu.Lock()
	if mock.lastAuthMethod != "cursor_login" {
		mock.mu.Unlock()
		t.Fatalf("expected authenticate(cursor_login), got %q", mock.lastAuthMethod)
	}
	mock.mu.Unlock()

	// 2. The models payload must surface as a synthetic config_option_update
	// pending update keyed under "model" so the web picker just works.
	foundModels := false
	for _, u := range createRes.PendingUpdates {
		s := string(u)
		if strings.Contains(s, "config_option_update") &&
			strings.Contains(s, `"composer-2.5[fast=true]"`) &&
			strings.Contains(s, `"gpt-5.5[reasoning=medium]"`) {
			foundModels = true
			break
		}
	}
	if !foundModels {
		t.Fatalf("synthetic model config option not in pending updates: %v",
			len(createRes.PendingUpdates))
	}

	// 3. Switching the model goes through session/set_model and
	// re-broadcasts a refreshed snapshot.
	sessionID := createRes.Session.ID
	req, _ = wire.MakeRequest("sc-1", wire.ChannelChat, wire.ChatMethodSetConfigOption,
		wire.ChatSetConfigOptionRequest{
			SessionID: sessionID,
			ConfigID:  "model",
			Value:     "gpt-5.5[reasoning=medium]",
		})
	_ = wsjson.Write(ctx, clientWS, req)
	res = awaitResp("sc-1")
	if res.Error != nil {
		t.Fatalf("set_config_option error: %+v", res.Error)
	}
	mock.mu.Lock()
	if mock.lastSetModelID != "gpt-5.5[reasoning=medium]" {
		mock.mu.Unlock()
		t.Fatalf("mock saw set_model %q", mock.lastSetModelID)
	}
	if mock.sawSetConfigCall {
		mock.mu.Unlock()
		t.Fatal("router must not forward set_config_option to a models-style agent")
	}
	mock.mu.Unlock()

	// 4. History must replay the refreshed snapshot (currentValue = new
	// model) so a page reload keeps the user's pick.
	req, _ = wire.MakeRequest("h-1", wire.ChannelChat, wire.ChatMethodGetHistory,
		wire.ChatGetHistoryRequest{SessionID: sessionID})
	_ = wsjson.Write(ctx, clientWS, req)
	res = awaitResp("h-1")
	var hist wire.ChatGetHistoryResponse
	if err := json.Unmarshal(res.Payload, &hist); err != nil {
		t.Fatalf("history payload: %v", err)
	}
	found := false
	for _, u := range hist.Updates {
		s := string(u)
		if strings.Contains(s, "config_option_update") &&
			strings.Contains(s, `"currentValue":"gpt-5.5[reasoning=medium]"`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("refreshed model snapshot not in history; got %d updates", len(hist.Updates))
	}
}
