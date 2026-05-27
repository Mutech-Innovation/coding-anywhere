package devin_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/acp"
	"github.com/Mutech-Innovation/coding-anywhere/internal/agent/devin"
)

// mockACPServer is a minimal in-process ACP peer used as a Devin stand-in.
// It reads JSON-RPC frames from its stdin, replies to initialize and
// session/* methods, and exposes hooks for tests to push notifications.
type mockACPServer struct {
	t      *testing.T
	in     *io.PipeReader  // server reads from here
	out    *io.PipeWriter  // server writes here
	writer *bufio.Writer
	wMu    sync.Mutex

	mu sync.Mutex
	// Last params seen for assertions.
	lastInit   *acp.InitializeRequest
	lastNewCwd string
	lastPrompt []acp.ContentBlock
	lastCancel string
	lastSetMode struct {
		sessionID string
		modeID    string
	}
	lastSetConfigOption struct {
		sessionID string
		configID  string
		value     string
	}
	cancelCh   chan struct{}
	promptResp string
	// modesOnNew is returned in the session/new response when set.
	modesOnNew *acp.SessionModeState
}

func newMockACPServer(t *testing.T) (*mockACPServer, *io.PipeReader, *io.PipeWriter) {
	t.Helper()
	// adapter side: stdin = adapter writes to server, stdout = adapter reads from server
	adapterStdinR, adapterStdinW := io.Pipe()  // adapter→server
	adapterStdoutR, adapterStdoutW := io.Pipe() // server→adapter
	srv := &mockACPServer{
		t:        t,
		in:       adapterStdinR,
		out:      adapterStdoutW,
		writer:   bufio.NewWriter(adapterStdoutW),
		cancelCh: make(chan struct{}, 4),
		promptResp: "end_turn",
	}
	go srv.run()
	return srv, adapterStdoutR, adapterStdinW
}

func (s *mockACPServer) run() {
	dec := json.NewDecoder(s.in)
	for {
		var msg map[string]any
		if err := dec.Decode(&msg); err != nil {
			return
		}
		method, _ := msg["method"].(string)
		idVal, hasID := msg["id"]
		switch method {
		case "initialize":
			s.mu.Lock()
			b, _ := json.Marshal(msg["params"])
			var req acp.InitializeRequest
			_ = json.Unmarshal(b, &req)
			s.lastInit = &req
			s.mu.Unlock()
			s.respond(idVal, map[string]any{
				"protocolVersion": 1,
				"agentCapabilities": map[string]any{
					"loadSession":         true,
					"promptCapabilities":  map[string]any{"image": false},
					"sessionCapabilities": map[string]any{"resume": map[string]any{}},
				},
				"agentInfo": map[string]any{
					"name":    "mock-devin",
					"title":   "Mock Devin",
					"version": "0.0.1",
				},
			})
		case "session/new":
			b, _ := json.Marshal(msg["params"])
			var req acp.SessionNewRequest
			_ = json.Unmarshal(b, &req)
			s.mu.Lock()
			s.lastNewCwd = req.Cwd
			modes := s.modesOnNew
			s.mu.Unlock()
			result := map[string]any{"sessionId": "sess-mock-1"}
			if modes != nil {
				result["modes"] = modes
			}
			s.respond(idVal, result)
		case "session/set_mode":
			b, _ := json.Marshal(msg["params"])
			var req acp.SessionSetModeRequest
			_ = json.Unmarshal(b, &req)
			s.mu.Lock()
			s.lastSetMode.sessionID = req.SessionID
			s.lastSetMode.modeID = req.ModeID
			s.mu.Unlock()
			s.respond(idVal, map[string]any{})
		case "session/set_config_option":
			b, _ := json.Marshal(msg["params"])
			var req acp.SessionSetConfigOptionRequest
			_ = json.Unmarshal(b, &req)
			s.mu.Lock()
			s.lastSetConfigOption.sessionID = req.SessionID
			s.lastSetConfigOption.configID = req.ConfigID
			s.lastSetConfigOption.value = req.Value
			s.mu.Unlock()
			s.respond(idVal, map[string]any{"configOptions": []any{}})
		case "session/prompt":
			b, _ := json.Marshal(msg["params"])
			var req acp.SessionPromptRequest
			_ = json.Unmarshal(b, &req)
			s.mu.Lock()
			s.lastPrompt = req.Prompt
			resp := s.promptResp
			s.mu.Unlock()
			// Push some session/update notifications mid-turn.
			s.notify("session/update", map[string]any{
				"sessionId": req.SessionID,
				"update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"content":       map[string]any{"type": "text", "text": "ok"},
				},
			})
			s.respond(idVal, map[string]any{"stopReason": resp})
		case "session/cancel":
			b, _ := json.Marshal(msg["params"])
			var n acp.SessionCancelNotification
			_ = json.Unmarshal(b, &n)
			s.mu.Lock()
			s.lastCancel = n.SessionID
			s.mu.Unlock()
			select {
			case s.cancelCh <- struct{}{}:
			default:
			}
			// notifications get no reply
		default:
			if hasID {
				s.respondError(idVal, -32601, "unknown method "+method)
			}
		}
	}
}

func (s *mockACPServer) respond(id any, result map[string]any) {
	out := map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
	s.write(out)
}

func (s *mockACPServer) respondError(id any, code int, msg string) {
	out := map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}}
	s.write(out)
}

func (s *mockACPServer) notify(method string, params map[string]any) {
	out := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	s.write(out)
}

func (s *mockACPServer) write(v any) {
	buf, _ := json.Marshal(v)
	s.wMu.Lock()
	defer s.wMu.Unlock()
	_, _ = s.writer.Write(buf)
	_, _ = s.writer.Write([]byte("\n"))
	_ = s.writer.Flush()
}

func TestDevinAdapterStartAndInitialize(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	_ = srv

	a := devin.NewWithStreams(stdin, stdout, devin.Options{})
	t.Cleanup(func() { _ = a.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !a.Initialized() {
		t.Fatal("expected initialized")
	}
	if got := a.AgentInfo().Name; got != "mock-devin" {
		t.Fatalf("agent name: %q", got)
	}
	if !a.Capabilities().LoadSession {
		t.Fatal("loadSession capability not parsed")
	}
}

func TestDevinAdapterNewSessionAndPrompt(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	var (
		gotMu     sync.Mutex
		gotEvents []string
	)
	a := devin.NewWithStreams(stdin, stdout, devin.Options{
		OnSessionUpdate: func(sessionID string, update json.RawMessage) {
			gotMu.Lock()
			defer gotMu.Unlock()
			gotEvents = append(gotEvents, sessionID+":"+string(update))
		},
	})
	t.Cleanup(func() { _ = a.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	sessID, modes, err := a.NewSession(ctx, "/Users/me/app", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sessID != "sess-mock-1" {
		t.Fatalf("sessionID: %q", sessID)
	}
	_ = modes // covered separately
	stop, err := a.PromptText(ctx, sessID, "hello")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if stop != "end_turn" {
		t.Fatalf("stopReason: %q", stop)
	}
	gotMu.Lock()
	defer gotMu.Unlock()
	if len(gotEvents) == 0 {
		t.Fatalf("expected at least one session/update event")
	}
	if !strings.Contains(gotEvents[0], "agent_message_chunk") {
		t.Fatalf("event payload: %s", gotEvents[0])
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.lastNewCwd != "/Users/me/app" {
		t.Fatalf("cwd: %s", srv.lastNewCwd)
	}
	if len(srv.lastPrompt) != 1 || srv.lastPrompt[0].Text != "hello" {
		t.Fatalf("prompt content: %+v", srv.lastPrompt)
	}
}

func TestDevinAdapterCancel(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	a := devin.NewWithStreams(stdin, stdout, devin.Options{})
	t.Cleanup(func() { _ = a.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.Cancel("sess-x"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	select {
	case <-srv.cancelCh:
	case <-time.After(2 * time.Second):
		t.Fatal("server didn't see cancel")
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.lastCancel != "sess-x" {
		t.Fatalf("session id: %s", srv.lastCancel)
	}
}

func TestDevinAdapterNewSessionReturnsModes(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	srv.modesOnNew = &acp.SessionModeState{
		CurrentModeID: "ask",
		AvailableModes: []acp.SessionMode{
			{ID: "ask", Name: "Ask", Description: "Plan first"},
			{ID: "code", Name: "Code"},
		},
	}
	a := devin.NewWithStreams(stdin, stdout, devin.Options{})
	t.Cleanup(func() { _ = a.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	_, modes, err := a.NewSession(ctx, "/tmp", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if modes == nil || modes.CurrentModeID != "ask" || len(modes.AvailableModes) != 2 {
		t.Fatalf("modes not parsed: %+v", modes)
	}
}

func TestDevinAdapterSetMode(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	a := devin.NewWithStreams(stdin, stdout, devin.Options{})
	t.Cleanup(func() { _ = a.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.SetMode(ctx, "sess-x", "code"); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.lastSetMode.sessionID != "sess-x" || srv.lastSetMode.modeID != "code" {
		t.Fatalf("set_mode not seen: %+v", srv.lastSetMode)
	}
}

func TestDevinAdapterSetConfigOption(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	a := devin.NewWithStreams(stdin, stdout, devin.Options{})
	t.Cleanup(func() { _ = a.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.SetConfigOption(ctx, "sess-y", "model", "claude-opus-4-7-max"); err != nil {
		t.Fatalf("SetConfigOption: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	got := srv.lastSetConfigOption
	if got.sessionID != "sess-y" || got.configID != "model" || got.value != "claude-opus-4-7-max" {
		t.Fatalf("set_config_option not seen: %+v", got)
	}
}

func TestDevinAdapterPermissionCallback(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	called := make(chan struct{}, 1)
	a := devin.NewWithStreams(stdin, stdout, devin.Options{
		OnPermissionRequest: func(_ context.Context, sessionID string, params json.RawMessage) (json.RawMessage, *acp.RPCError) {
			if sessionID != "sess-1" {
				t.Errorf("session id: %s", sessionID)
			}
			called <- struct{}{}
			return json.RawMessage(`{"decision":"allow"}`), nil
		},
	})
	t.Cleanup(func() { _ = a.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Have the mock push a session/request_permission.
	srv.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      99,
		"method":  acp.MethodSessionRequestPermission,
		"params":  map[string]any{"sessionId": "sess-1"},
	})

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("permission callback not invoked")
	}
}
