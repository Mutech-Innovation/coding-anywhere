// Package chatrouter wires the daemon's `chat` WebSocket channel onto
// concrete agent adapters (Devin first; Claude Code, Codex, cursor-agent in
// v2). It owns the per-session state, translates between wire envelopes and
// the agent's ACP-style API, and bridges permission requests back to the
// user.
package chatrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/acp"
	"github.com/Mutech-Innovation/coding-anywhere/internal/agent/devin"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/oklog/ulid/v2"
)

// AgentBuilder lazily produces a configured Devin adapter. The router calls
// Build the first time a session is opened against an agent. If the adapter
// crashes, the router will rebuild on the next session.
type AgentBuilder func(opts devin.Options) (*devin.Adapter, error)

// DefaultDevinBuilder wraps `devin acp` as a subprocess.
func DefaultDevinBuilder(opts devin.Options) (*devin.Adapter, error) {
	return devin.NewSubprocess(devin.DefaultBinary(), opts)
}

// Router owns chat state and fans daemon ↔ relay ↔ ACP traffic.
type Router struct {
	logger *slog.Logger
	cli    *relayclient.Client

	devinBuilder AgentBuilder

	// runCtx lives as long as the router. ACP read loops are pinned to it
	// so that per-request timeouts (e.g. on session/new) don't kill the
	// long-running stdio session.
	runCtx    context.Context
	runCancel context.CancelFunc

	mu       sync.Mutex
	devin    *devin.Adapter // nil until first session
	sessions map[string]*session

	pendingMu sync.Mutex
	pending   map[string]*pendingPermission // request_id → channel

	eventSeq atomic.Uint64
}

type session struct {
	wire.ChatSession
	cancelCtx context.CancelFunc
}

type pendingPermission struct {
	resCh chan resolution
}

type resolution struct {
	body json.RawMessage
	err  *acp.RPCError
}

// New constructs a router and registers a `chat` channel handler. The
// router owns its own context for long-lived agent processes — Close
// cancels it.
func New(cli *relayclient.Client, logger *slog.Logger, builder AgentBuilder) *Router {
	if builder == nil {
		builder = DefaultDevinBuilder
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Router{
		logger:       logger,
		cli:          cli,
		devinBuilder: builder,
		runCtx:       ctx,
		runCancel:    cancel,
		sessions:     make(map[string]*session),
		pending:      make(map[string]*pendingPermission),
	}
	cli.Handle(wire.ChannelChat, r.handle)
	return r
}

// Close terminates any open agent sessions and shuts down the underlying
// agent adapter.
func (r *Router) Close() error {
	r.runCancel()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		if s.cancelCtx != nil {
			s.cancelCtx()
		}
	}
	r.sessions = map[string]*session{}
	if r.devin != nil {
		err := r.devin.Close()
		r.devin = nil
		return err
	}
	return nil
}

func (r *Router) handle(env wire.Envelope) error {
	if env.Type != wire.TypeRequest {
		return nil
	}
	switch env.Method {
	case wire.ChatMethodAgents:
		r.handleAgents(env)
	case wire.ChatMethodListSessions:
		r.handleListSessions(env)
	case wire.ChatMethodCreateSession:
		var req wire.ChatCreateSessionRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		r.handleCreateSession(env, req)
	case wire.ChatMethodPrompt:
		var req wire.ChatPromptRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		r.handlePrompt(env, req)
	case wire.ChatMethodCancel:
		var req wire.ChatCancelRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		r.handleCancel(env, req)
	case wire.ChatMethodCloseSession:
		var req wire.ChatCloseSessionRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		r.handleCloseSession(env, req)
	case wire.ChatMethodResolvePermit:
		var req wire.ChatResolvePermissionRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		r.handleResolvePermission(env, req)
	default:
		r.respondErr(env, "unknown_method", fmt.Sprintf("unknown chat method %q", env.Method))
	}
	return nil
}

func (r *Router) handleAgents(env wire.Envelope) {
	r.respondOK(env, wire.ChatAgentsResponse{
		Agents: []wire.AgentDescriptor{
			{ID: "devin", Name: "Devin for Terminal", Available: true},
			{ID: "claude-code", Name: "Claude Code", Available: false, Reason: "adapter lands in v2"},
			{ID: "codex", Name: "Codex", Available: false, Reason: "adapter lands in v2"},
			{ID: "cursor-cli", Name: "cursor-agent", Available: false, Reason: "adapter lands in v2"},
		},
	})
}

func (r *Router) handleListSessions(env wire.Envelope) {
	r.mu.Lock()
	out := make([]wire.ChatSession, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s.ChatSession)
	}
	r.mu.Unlock()
	r.respondOK(env, wire.ChatListSessionsResponse{Sessions: out})
}

func (r *Router) handleCreateSession(env wire.Envelope, req wire.ChatCreateSessionRequest) {
	if req.AgentID == "" {
		req.AgentID = "devin"
	}
	if req.AgentID != "devin" {
		r.respondErr(env, "agent_unavailable",
			fmt.Sprintf("agent %q is not available yet (v2 roadmap)", req.AgentID))
		return
	}
	if req.Cwd == "" {
		r.respondErr(env, "bad_request", "cwd required")
		return
	}
	a, err := r.ensureDevin()
	if err != nil {
		r.respondErr(env, "agent_start_failed", err.Error())
		return
	}
	// Bound the new-session call but keep the agent's run loop on the
	// long-lived router context.
	callCtx, cancel := context.WithTimeout(r.runCtx, 30*time.Second)
	defer cancel()
	nativeID, err := a.NewSession(callCtx, req.Cwd, nil)
	if err != nil {
		r.respondErr(env, "agent_new_session_failed", err.Error())
		return
	}
	id := ulid.Make().String()
	s := &session{
		ChatSession: wire.ChatSession{
			ID:        id,
			AgentID:   req.AgentID,
			NativeID:  nativeID,
			Cwd:       req.Cwd,
			Title:     req.Title,
			Status:    "idle",
			StartedAt: time.Now().UTC(),
		},
	}
	r.mu.Lock()
	r.sessions[id] = s
	r.mu.Unlock()

	r.respondOK(env, wire.ChatCreateSessionResponse{Session: s.ChatSession})
	r.broadcastEvent(wire.ChatEventSessionCreated, wire.ChatSessionCreatedEvent{Session: s.ChatSession})
}

func (r *Router) handlePrompt(env wire.Envelope, req wire.ChatPromptRequest) {
	r.mu.Lock()
	s, ok := r.sessions[req.SessionID]
	r.mu.Unlock()
	if !ok {
		r.respondErr(env, "not_found", "unknown session")
		return
	}
	if r.devin == nil {
		r.respondErr(env, "agent_not_started", "devin adapter not initialized")
		return
	}

	r.setStatus(s, "thinking")
	go func() {
		ctx, cancel := context.WithCancel(r.runCtx)
		r.mu.Lock()
		s.cancelCtx = cancel
		r.mu.Unlock()
		defer func() {
			r.mu.Lock()
			s.cancelCtx = nil
			r.mu.Unlock()
		}()
		stop, err := r.devin.PromptText(ctx, s.NativeID, req.Text)
		if err != nil {
			r.respondErr(env, "prompt_failed", err.Error())
			r.setStatus(s, "idle")
			return
		}
		r.respondOK(env, wire.ChatPromptResponse{StopReason: stop})
		r.setStatus(s, "idle")
	}()
}

func (r *Router) handleCancel(env wire.Envelope, req wire.ChatCancelRequest) {
	r.mu.Lock()
	s, ok := r.sessions[req.SessionID]
	r.mu.Unlock()
	if !ok {
		r.respondErr(env, "not_found", "unknown session")
		return
	}
	if r.devin != nil {
		_ = r.devin.Cancel(s.NativeID)
	}
	r.respondOK(env, map[string]bool{"ok": true})
}

func (r *Router) handleCloseSession(env wire.Envelope, req wire.ChatCloseSessionRequest) {
	r.mu.Lock()
	s, ok := r.sessions[req.SessionID]
	if ok {
		delete(r.sessions, req.SessionID)
	}
	r.mu.Unlock()
	if !ok {
		r.respondErr(env, "not_found", "unknown session")
		return
	}
	if s.cancelCtx != nil {
		s.cancelCtx()
	}
	r.respondOK(env, map[string]bool{"ok": true})
	r.broadcastEvent(wire.ChatEventSessionClosed, wire.ChatSessionClosedEvent{SessionID: s.ID})
}

func (r *Router) handleResolvePermission(env wire.Envelope, req wire.ChatResolvePermissionRequest) {
	r.pendingMu.Lock()
	p, ok := r.pending[req.RequestID]
	if ok {
		delete(r.pending, req.RequestID)
	}
	r.pendingMu.Unlock()
	if !ok {
		r.respondErr(env, "not_found", "unknown permission request")
		return
	}
	p.resCh <- resolution{body: req.Decision}
	r.respondOK(env, map[string]bool{"ok": true})
}

// ensureDevin lazily creates and starts the Devin adapter on first use. The
// adapter's run loop is pinned to r.runCtx so it survives request-level
// timeouts.
func (r *Router) ensureDevin() (*devin.Adapter, error) {
	r.mu.Lock()
	if r.devin != nil {
		a := r.devin
		r.mu.Unlock()
		return a, nil
	}
	r.mu.Unlock()

	a, err := r.devinBuilder(devin.Options{
		Logger: r.logger,
		OnSessionUpdate: func(sessionID string, update json.RawMessage) {
			r.broadcastEvent(wire.ChatEventUpdate, wire.ChatUpdateEvent{
				SessionID: r.daemonSessionID(sessionID),
				Update:    update,
			})
		},
		OnPermissionRequest: func(_ context.Context, sessionID string, params json.RawMessage) (json.RawMessage, *acp.RPCError) {
			return r.handleAgentPermissionRequest(sessionID, params)
		},
	})
	if err != nil {
		return nil, err
	}
	if err := a.Start(r.runCtx); err != nil {
		_ = a.Close()
		return nil, err
	}
	r.mu.Lock()
	r.devin = a
	r.mu.Unlock()
	return a, nil
}

// daemonSessionID maps an ACP-native session id back to our daemon-side id.
func (r *Router) daemonSessionID(nativeID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		if s.NativeID == nativeID {
			return s.ID
		}
	}
	return nativeID
}

func (r *Router) handleAgentPermissionRequest(nativeSessionID string, params json.RawMessage) (json.RawMessage, *acp.RPCError) {
	daemonID := r.daemonSessionID(nativeSessionID)
	requestID := ulid.Make().String()
	pp := &pendingPermission{resCh: make(chan resolution, 1)}
	r.pendingMu.Lock()
	r.pending[requestID] = pp
	r.pendingMu.Unlock()
	r.broadcastEvent(wire.ChatEventPermissionRequest, wire.ChatPermissionRequestEvent{
		SessionID: daemonID,
		RequestID: requestID,
		Params:    params,
	})

	// Update session status so any client UI can show "awaiting permission".
	r.mu.Lock()
	s, ok := r.sessions[daemonID]
	r.mu.Unlock()
	if ok {
		r.setStatus(s, "awaiting_permission")
		defer r.setStatus(s, "thinking")
	}

	// Wait up to 10 minutes for a decision.
	timer := time.NewTimer(10 * time.Minute)
	defer timer.Stop()
	select {
	case res := <-pp.resCh:
		r.broadcastEvent(wire.ChatEventPermissionResolved, wire.ChatPermissionResolvedEvent{
			SessionID: daemonID,
			RequestID: requestID,
		})
		if res.err != nil {
			return nil, res.err
		}
		return res.body, nil
	case <-timer.C:
		r.pendingMu.Lock()
		delete(r.pending, requestID)
		r.pendingMu.Unlock()
		return nil, &acp.RPCError{Code: -32000, Message: "permission request timed out"}
	}
}

func (r *Router) setStatus(s *session, status string) {
	r.mu.Lock()
	if s.Status == status {
		r.mu.Unlock()
		return
	}
	s.Status = status
	id := s.ID
	r.mu.Unlock()
	r.broadcastEvent(wire.ChatEventSessionStatus, wire.ChatSessionStatusEvent{
		SessionID: id, Status: status,
	})
}

// --- helpers ---

func (r *Router) parse(env wire.Envelope, out any) error {
	if len(env.Payload) == 0 {
		return errors.New("empty payload")
	}
	if err := json.Unmarshal(env.Payload, out); err != nil {
		r.respondErr(env, "bad_payload", err.Error())
		return err
	}
	return nil
}

func (r *Router) respondOK(env wire.Envelope, payload any) {
	res, err := wire.MakeResponse(env.ID, env.Channel, payload, nil)
	if err != nil {
		r.logger.Warn("respondOK encode", "err", err)
		return
	}
	r.cli.Send(res)
}

func (r *Router) respondErr(env wire.Envelope, code, msg string) {
	res, err := wire.MakeResponse(env.ID, env.Channel, nil, &wire.EnvelopeError{
		Code: code, Message: msg,
	})
	if err != nil {
		return
	}
	r.cli.Send(res)
}

func (r *Router) broadcastEvent(method string, payload any) {
	r.eventSeq.Add(1)
	id := fmt.Sprintf("%s-%d", ulid.Make().String(), r.eventSeq.Load())
	ev, err := wire.MakeEvent(id, wire.ChannelChat, method, payload)
	if err != nil {
		r.logger.Warn("MakeEvent chat", "method", method, "err", err)
		return
	}
	r.cli.Send(ev)
}
