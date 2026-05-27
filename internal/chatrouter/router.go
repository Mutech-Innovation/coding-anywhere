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

	mu         sync.Mutex
	devin      *devin.Adapter // nil until first session
	sessions   map[string]*session
	defaultCwd string

	pendingMu sync.Mutex
	pending   map[string]*pendingPermission // request_id → channel

	eventSeq atomic.Uint64
}

type session struct {
	wire.ChatSession
	cancelCtx context.CancelFunc

	// modes captured from session/new, updated by current_mode_update.
	modes         []wire.SessionMode
	currentModeID string

	// snapshot of every session/update we've forwarded, plus the user
	// prompts we've sent into the session. Both share a per-session
	// monotonically increasing sequence so the client can interleave
	// them when rebuilding the timeline.
	mu        sync.Mutex
	seq       uint64
	updates   []json.RawMessage
	updateSeq []uint64
	prompts   []wire.ChatPromptEntry
}

func (s *session) appendUpdate(update json.RawMessage) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	s.updates = append(s.updates, append([]byte(nil), update...))
	s.updateSeq = append(s.updateSeq, s.seq)
	return s.seq
}

func (s *session) appendPrompt(text string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	s.prompts = append(s.prompts, wire.ChatPromptEntry{
		Text:       text,
		CreatedAt:  time.Now().UTC(),
		SequenceID: s.seq,
	})
	return s.seq
}

func (s *session) snapshot() ([]json.RawMessage, []wire.ChatPromptEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	upd := make([]json.RawMessage, len(s.updates))
	for i, u := range s.updates {
		upd[i] = append([]byte(nil), u...)
	}
	prompts := make([]wire.ChatPromptEntry, len(s.prompts))
	copy(prompts, s.prompts)
	return upd, prompts
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
//
// defaultCwd, if non-empty, is used when a session is created without an
// explicit Cwd. Pass an empty value (or omit the arg) to require clients to
// always pick a cwd — useful when the daemon's files workspace points at a
// broad root like $HOME that Devin would refuse to operate on.
func New(cli *relayclient.Client, logger *slog.Logger, builder AgentBuilder, defaultCwd ...string) *Router {
	if builder == nil {
		builder = DefaultDevinBuilder
	}
	cwd := ""
	if len(defaultCwd) > 0 {
		cwd = defaultCwd[0]
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Router{
		logger:       logger,
		cli:          cli,
		devinBuilder: builder,
		runCtx:       ctx,
		runCancel:    cancel,
		sessions:     make(map[string]*session),
		defaultCwd:   cwd,
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
	case wire.ChatMethodSetMode:
		var req wire.ChatSetModeRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		r.handleSetMode(env, req)
	case wire.ChatMethodGetHistory:
		var req wire.ChatGetHistoryRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		r.handleGetHistory(env, req)
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
		r.mu.Lock()
		req.Cwd = r.defaultCwd
		r.mu.Unlock()
		if req.Cwd == "" {
			r.respondErr(env, "bad_request", "cwd required")
			return
		}
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
	nativeID, modes, err := a.NewSession(callCtx, req.Cwd, nil)
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
	if modes != nil {
		s.modes = make([]wire.SessionMode, len(modes.AvailableModes))
		for i, m := range modes.AvailableModes {
			s.modes[i] = wire.SessionMode{ID: m.ID, Name: m.Name, Description: m.Description}
		}
		s.currentModeID = modes.CurrentModeID
	}
	r.mu.Lock()
	r.sessions[id] = s
	r.mu.Unlock()

	r.respondOK(env, wire.ChatCreateSessionResponse{
		Session:        s.ChatSession,
		AvailableModes: s.modes,
		CurrentModeID:  s.currentModeID,
	})
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

	// Record the user prompt before sending so the snapshot retains the
	// originating message even if the agent closes mid-turn.
	s.appendPrompt(req.Text)

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
			r.handleAgentUpdate(sessionID, update)
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

// handleAgentUpdate persists a session/update notification on the matching
// session and broadcasts it to clients. Updates that mutate session-level
// state (e.g. current_mode_update) are also folded into the session struct
// so chat.list_sessions and chat.get_history reflect the latest values.
func (r *Router) handleAgentUpdate(nativeSessionID string, update json.RawMessage) {
	daemonID := r.daemonSessionID(nativeSessionID)
	r.mu.Lock()
	s, ok := r.sessions[daemonID]
	r.mu.Unlock()
	var seq uint64
	if ok {
		seq = s.appendUpdate(update)
		// Best-effort decode of the discriminator so we can pick out
		// state-mutating updates without forcing a full schema.
		var head struct {
			Kind  string  `json:"sessionUpdate"`
			Mode  string  `json:"currentModeId"`
			Title *string `json:"title"`
		}
		if err := json.Unmarshal(update, &head); err == nil {
			switch head.Kind {
			case "current_mode_update":
				if head.Mode != "" {
					r.mu.Lock()
					s.currentModeID = head.Mode
					r.mu.Unlock()
				}
			case "session_info_update":
				if head.Title != nil {
					r.mu.Lock()
					s.Title = *head.Title
					r.mu.Unlock()
				}
			}
		}
	}
	r.broadcastEvent(wire.ChatEventUpdate, wire.ChatUpdateEvent{
		SessionID:  daemonID,
		Update:     update,
		SequenceID: seq,
	})
}

func (r *Router) handleSetMode(env wire.Envelope, req wire.ChatSetModeRequest) {
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
	if req.ModeID == "" {
		r.respondErr(env, "bad_request", "mode_id required")
		return
	}
	ctx, cancel := context.WithTimeout(r.runCtx, 10*time.Second)
	defer cancel()
	if err := r.devin.SetMode(ctx, s.NativeID, req.ModeID); err != nil {
		r.respondErr(env, "agent_set_mode_failed", err.Error())
		return
	}
	// Optimistically reflect the client's choice. The agent will likely
	// also send a current_mode_update which will overwrite if different.
	r.mu.Lock()
	s.currentModeID = req.ModeID
	r.mu.Unlock()
	r.respondOK(env, map[string]string{"mode_id": req.ModeID})
}

func (r *Router) handleGetHistory(env wire.Envelope, req wire.ChatGetHistoryRequest) {
	r.mu.Lock()
	s, ok := r.sessions[req.SessionID]
	r.mu.Unlock()
	if !ok {
		r.respondErr(env, "not_found", "unknown session")
		return
	}
	updates, prompts := s.snapshot()
	r.respondOK(env, wire.ChatGetHistoryResponse{
		Session:        s.ChatSession,
		Updates:        updates,
		AvailableModes: s.modes,
		CurrentModeID:  s.currentModeID,
		UserPrompts:    prompts,
	})
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
	prev := s.Status
	s.Status = status
	id := s.ID
	title := s.Title
	r.mu.Unlock()
	r.broadcastEvent(wire.ChatEventSessionStatus, wire.ChatSessionStatusEvent{
		SessionID: id, Status: status,
	})
	// Push a Web Push when the agent finishes a turn so backgrounded
	// devices see the result without keeping the tab open.
	if status == "idle" && (prev == "thinking" || prev == "awaiting_permission") {
		body := "Devin has finished thinking."
		if title != "" {
			body = title + " · " + body
		}
		r.broadcastEventOnChannel(wire.ChannelNotifications, wire.NotificationsMethodSend,
			wire.NotificationsSendEvent{
				Title:     "Coding Anywhere",
				Body:      body,
				Tag:       "chat-" + id,
				SessionID: id,
				URL:       "/", // future: deep-link to /chat/<id>
			},
		)
	}
}

// broadcastEventOnChannel emits an event on a non-default channel; used to
// route notifications.send onto the relay's notifications channel where the
// Web Push dispatcher picks it up.
func (r *Router) broadcastEventOnChannel(channel wire.Channel, method string, payload any) {
	r.eventSeq.Add(1)
	id := fmt.Sprintf("%s-%d", ulid.Make().String(), r.eventSeq.Load())
	ev, err := wire.MakeEvent(id, channel, method, payload)
	if err != nil {
		r.logger.Warn("MakeEvent", "channel", channel, "method", method, "err", err)
		return
	}
	r.cli.Send(ev)
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
