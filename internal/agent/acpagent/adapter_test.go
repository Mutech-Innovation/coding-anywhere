package acpagent_test

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
	"github.com/Mutech-Innovation/coding-anywhere/internal/agent/acpagent"
)

// mockACPServer is a minimal in-process ACP peer used as an agent stand-in.
// It reads JSON-RPC frames from its stdin, replies to initialize and
// session/* methods, and exposes hooks for tests to push notifications.
type mockACPServer struct {
	t      *testing.T
	in     *io.PipeReader // server reads from here
	out    *io.PipeWriter // server writes here
	writer *bufio.Writer
	wMu    sync.Mutex

	mu sync.Mutex
	// Last params seen for assertions.
	lastInit    *acp.InitializeRequest
	lastNewCwd  string
	lastPrompt  []acp.ContentBlock
	lastCancel  string
	lastSetMode struct {
		sessionID string
		modeID    string
	}
	lastSetModel struct {
		sessionID string
		modelID   string
	}
	lastSetConfigOption struct {
		sessionID string
		configID  string
		value     string
	}
	lastAuthMethod string
	cancelCh       chan struct{}
	promptResp     string
	// modesOnNew / modelsOnNew are returned in the session/new response when set.
	modesOnNew  *acp.SessionModeState
	modelsOnNew *acp.SessionModelState
	// authMethods advertised at initialize; requireAuth rejects session/new
	// until authenticate has been called (mimics cursor-agent).
	authMethods   []map[string]any
	requireAuth   bool
	authenticated bool
}

func newMockACPServer(t *testing.T) (*mockACPServer, *io.PipeReader, *io.PipeWriter) {
	t.Helper()
	// adapter side: stdin = adapter writes to server, stdout = adapter reads from server
	adapterStdinR, adapterStdinW := io.Pipe()   // adapter→server
	adapterStdoutR, adapterStdoutW := io.Pipe() // server→adapter
	srv := &mockACPServer{
		t:          t,
		in:         adapterStdinR,
		out:        adapterStdoutW,
		writer:     bufio.NewWriter(adapterStdoutW),
		cancelCh:   make(chan struct{}, 4),
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
			authMethods := s.authMethods
			s.mu.Unlock()
			result := map[string]any{
				"protocolVersion": 1,
				"agentCapabilities": map[string]any{
					"loadSession":         true,
					"promptCapabilities":  map[string]any{"image": false},
					"sessionCapabilities": map[string]any{"resume": map[string]any{}},
				},
				"agentInfo": map[string]any{
					"name":    "mock-agent",
					"title":   "Mock Agent",
					"version": "0.0.1",
				},
			}
			if len(authMethods) > 0 {
				result["authMethods"] = authMethods
			}
			s.respond(idVal, result)
		case "authenticate":
			b, _ := json.Marshal(msg["params"])
			var req acp.AuthenticateRequest
			_ = json.Unmarshal(b, &req)
			s.mu.Lock()
			s.lastAuthMethod = req.MethodID
			s.authenticated = true
			s.mu.Unlock()
			s.respond(idVal, map[string]any{})
		case "session/new":
			b, _ := json.Marshal(msg["params"])
			var req acp.SessionNewRequest
			_ = json.Unmarshal(b, &req)
			s.mu.Lock()
			s.lastNewCwd = req.Cwd
			modes := s.modesOnNew
			models := s.modelsOnNew
			authOK := !s.requireAuth || s.authenticated
			s.mu.Unlock()
			if !authOK {
				s.respondError(idVal, -32000, "Authentication required")
				continue
			}
			result := map[string]any{"sessionId": "sess-mock-1"}
			if modes != nil {
				result["modes"] = modes
			}
			if models != nil {
				result["models"] = models
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
		case "session/set_model":
			b, _ := json.Marshal(msg["params"])
			var req acp.SessionSetModelRequest
			_ = json.Unmarshal(b, &req)
			s.mu.Lock()
			s.lastSetModel.sessionID = req.SessionID
			s.lastSetModel.modelID = req.ModelID
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
			// Mimic Devin's response: it echoes back the new full
			// configOptions snapshot in the result body (no
			// session/update follow-up — callers fan that out
			// themselves).
			s.respond(idVal, map[string]any{
				"configOptions": []map[string]any{
					{"id": req.ConfigID, "currentValue": req.Value},
				},
			})
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

func TestAdapterStartAndInitialize(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	_ = srv

	a := acpagent.NewWithStreams(stdin, stdout, acpagent.Options{})
	t.Cleanup(func() { _ = a.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !a.Initialized() {
		t.Fatal("expected initialized")
	}
	if got := a.AgentInfo().Name; got != "mock-agent" {
		t.Fatalf("agent name: %q", got)
	}
	if !a.Capabilities().LoadSession {
		t.Fatal("loadSession capability not parsed")
	}
}

func TestAdapterNewSessionAndPrompt(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	var (
		gotMu     sync.Mutex
		gotEvents []string
	)
	a := acpagent.NewWithStreams(stdin, stdout, acpagent.Options{
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
	res, err := a.NewSession(ctx, "/Users/me/app", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if res.SessionID != "sess-mock-1" {
		t.Fatalf("sessionID: %q", res.SessionID)
	}
	stop, err := a.PromptText(ctx, res.SessionID, "hello")
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

func TestAdapterCancel(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	a := acpagent.NewWithStreams(stdin, stdout, acpagent.Options{})
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

func TestAdapterNewSessionReturnsModes(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	srv.modesOnNew = &acp.SessionModeState{
		CurrentModeID: "ask",
		AvailableModes: []acp.SessionMode{
			{ID: "ask", Name: "Ask", Description: "Plan first"},
			{ID: "code", Name: "Code"},
		},
	}
	a := acpagent.NewWithStreams(stdin, stdout, acpagent.Options{})
	t.Cleanup(func() { _ = a.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := a.NewSession(ctx, "/tmp", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if res.Modes == nil || res.Modes.CurrentModeID != "ask" || len(res.Modes.AvailableModes) != 2 {
		t.Fatalf("modes not parsed: %+v", res.Modes)
	}
}

// TestAdapterNewSessionReturnsModels covers cursor-agent's session-model
// extension: session/new carries a `models` payload that the adapter must
// surface to callers (the chatrouter synthesises a model config option
// from it).
func TestAdapterNewSessionReturnsModels(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	srv.modelsOnNew = &acp.SessionModelState{
		CurrentModelID: "composer-2.5[fast=true]",
		AvailableModels: []acp.SessionModel{
			{ModelID: "default[]", Name: "Auto"},
			{ModelID: "composer-2.5[fast=true]", Name: "composer-2.5"},
		},
	}
	a := acpagent.NewWithStreams(stdin, stdout, acpagent.Options{})
	t.Cleanup(func() { _ = a.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := a.NewSession(ctx, "/tmp", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if res.Models == nil || res.Models.CurrentModelID != "composer-2.5[fast=true]" ||
		len(res.Models.AvailableModels) != 2 {
		t.Fatalf("models not parsed: %+v", res.Models)
	}
}

func TestAdapterSetMode(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	a := acpagent.NewWithStreams(stdin, stdout, acpagent.Options{})
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

func TestAdapterSetModel(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	a := acpagent.NewWithStreams(stdin, stdout, acpagent.Options{})
	t.Cleanup(func() { _ = a.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.SetModel(ctx, "sess-x", "gpt-5.5[reasoning=medium]"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.lastSetModel.sessionID != "sess-x" || srv.lastSetModel.modelID != "gpt-5.5[reasoning=medium]" {
		t.Fatalf("set_model not seen: %+v", srv.lastSetModel)
	}
}

func TestAdapterSetConfigOption(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	a := acpagent.NewWithStreams(stdin, stdout, acpagent.Options{})
	t.Cleanup(func() { _ = a.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	configOptions, err := a.SetConfigOption(ctx, "sess-y", "model", "claude-opus-4-7-max")
	if err != nil {
		t.Fatalf("SetConfigOption: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	got := srv.lastSetConfigOption
	if got.sessionID != "sess-y" || got.configID != "model" || got.value != "claude-opus-4-7-max" {
		t.Fatalf("set_config_option not seen: %+v", got)
	}
	if !strings.Contains(string(configOptions), `"claude-opus-4-7-max"`) {
		t.Fatalf("expected configOptions snapshot in response, got %q", string(configOptions))
	}
}

// TestAdapterAuthenticate covers the cursor-agent style handshake: the agent
// advertises an auth method, rejects session/new until authenticate has been
// called, and the adapter authenticates transparently during Start.
func TestAdapterAuthenticate(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	srv.authMethods = []map[string]any{
		{"id": "cursor_login", "name": "Cursor Login"},
	}
	srv.requireAuth = true

	a := acpagent.NewWithStreams(stdin, stdout, acpagent.Options{
		AuthMethodID: "cursor_login",
	})
	t.Cleanup(func() { _ = a.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	srv.mu.Lock()
	authMethod := srv.lastAuthMethod
	srv.mu.Unlock()
	if authMethod != "cursor_login" {
		t.Fatalf("expected authenticate(cursor_login), got %q", authMethod)
	}
	// session/new must now pass the mock's auth gate.
	if _, err := a.NewSession(ctx, "/tmp", nil); err != nil {
		t.Fatalf("NewSession after authenticate: %v", err)
	}
}

// TestAdapterAuthenticateSkippedWhenNotAdvertised pins that the adapter does
// NOT call authenticate when the agent doesn't advertise the configured
// method (e.g. pointing the cursor builder at a Devin binary).
func TestAdapterAuthenticateSkippedWhenNotAdvertised(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)

	a := acpagent.NewWithStreams(stdin, stdout, acpagent.Options{
		AuthMethodID: "cursor_login",
	})
	t.Cleanup(func() { _ = a.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.lastAuthMethod != "" {
		t.Fatalf("authenticate should not be called, saw method %q", srv.lastAuthMethod)
	}
}

func TestAdapterPermissionCallback(t *testing.T) {
	srv, stdout, stdin := newMockACPServer(t)
	called := make(chan struct{}, 1)
	a := acpagent.NewWithStreams(stdin, stdout, acpagent.Options{
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
