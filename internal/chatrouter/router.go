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
	"strings"
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
	// earlyUpdates buffers session/update notifications that arrive while a
	// `chat.create_session` (and the underlying ACP `session/new`) is still
	// in flight — Devin sends configOptions / modes / availableCommands
	// before answering session/new, so we'd otherwise drop them on the
	// floor (no daemon-side session record yet) and the web UI would never
	// see the initial model / mode metadata. Keyed by native session id.
	// Drained by handleCreateSession once the session is registered.
	earlyUpdates map[string][]json.RawMessage

	pendingMu sync.Mutex
	pending   map[string]*pendingPermission // request_id → channel

	eventSeq atomic.Uint64
}

type session struct {
	wire.ChatSession
	cancelCtx context.CancelFunc

	// guest marks a session created by a read-only "ask"-mode share link
	// (relay-stamped Origin=guest). Such sessions are forced into ask mode at
	// creation and auto-deny every agent permission request.
	guest bool

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

func (s *session) appendPrompt(text string, attachments []wire.Attachment) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	s.prompts = append(s.prompts, wire.ChatPromptEntry{
		Text:        text,
		CreatedAt:   time.Now().UTC(),
		Attachments: attachments,
		SequenceID:  s.seq,
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
		earlyUpdates: make(map[string][]json.RawMessage),
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
	case wire.ChatMethodSetConfigOption:
		var req wire.ChatSetConfigOptionRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		r.handleSetConfigOption(env, req)
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
		// Drop any buffered updates for this native id — there's no
		// session to attach them to.
		r.mu.Lock()
		delete(r.earlyUpdates, nativeID)
		r.mu.Unlock()
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

	// Read-only guest ("ask"-share) sessions are forced into ask mode at
	// creation so the agent cannot make changes, no matter what mode it
	// defaulted to. The relay also blocks guests from calling set_mode.
	s.guest = env.Origin == wire.OriginGuest
	if s.guest {
		if askID := askModeID(s.modes); askID != "" && askID != s.currentModeID {
			setCtx, setCancel := context.WithTimeout(r.runCtx, 10*time.Second)
			if err := a.SetMode(setCtx, nativeID, askID); err != nil {
				r.logger.Warn("force ask mode for guest session failed",
					"native", nativeID, "err", err)
			} else {
				s.currentModeID = askID
			}
			setCancel()
		}
	}

	// Atomically claim any buffered updates and register the session.
	// Holding r.mu across both steps ensures no further notification can
	// be buffered under this native id after we drain — the next call to
	// handleAgentUpdate will find the registered session and process
	// normally.
	r.mu.Lock()
	buffered := r.earlyUpdates[nativeID]
	delete(r.earlyUpdates, nativeID)
	r.sessions[id] = s
	r.mu.Unlock()

	// Replay buffered updates onto the snapshot + state. We don't
	// broadcast here because the client hasn't yet been told about the
	// session; instead we ship them inline with the create_session
	// response below, and the client replays them through its own
	// ingest pipeline after registering the session locally.
	for _, u := range buffered {
		s.appendUpdate(u)
		r.applyUpdateStateMutations(s, u)
	}

	r.respondOK(env, wire.ChatCreateSessionResponse{
		Session:        s.ChatSession,
		AvailableModes: s.modes,
		CurrentModeID:  s.currentModeID,
		PendingUpdates: buffered,
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
	s.appendPrompt(req.Text, req.Attachments)

	// Convert attachments to ACP ContentBlocks
	contentBlocks := r.attachmentsToContentBlocks(req.Text, req.Attachments)

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
		stop, err := r.devin.Prompt(ctx, s.NativeID, contentBlocks)
		if err != nil {
			r.respondErr(env, "prompt_failed", err.Error())
			r.setStatus(s, "idle")
			return
		}
		r.respondOK(env, wire.ChatPromptResponse{StopReason: stop})
		r.setStatus(s, "idle")
	}()
}

// attachmentsToContentBlocks converts wire attachments to ACP ContentBlocks.
//
// All ACP field names are camelCase (`mimeType`, not `mime_type`). The
// agent's deserialiser rejects snake_case with a -32602 Invalid params
// error and `missing field "mimeType"`, so the wire-side Attachment's
// snake_case `mime_type` JSON tag is only an artefact of our daemon ↔
// web protocol — we translate to camelCase here on the way into the
// agent.
//
// ACP `image` blocks REQUIRE both `data` (base64 string) and `mimeType`.
// URI-only image references are not valid. ACP `resource_link` blocks
// require `uri`; `name` and `mimeType` are optional.
func (r *Router) attachmentsToContentBlocks(text string, attachments []wire.Attachment) []acp.ContentBlock {
	var blocks []acp.ContentBlock
	if text != "" {
		blocks = append(blocks, acp.ContentBlock{Type: "text", Text: text})
	}
	for _, att := range attachments {
		switch att.Type {
		case "image":
			if att.Data == "" || att.MimeType == "" {
				r.logger.Warn("dropping image attachment without data+mimeType",
					"name", att.Name)
				continue
			}
			blocks = append(blocks, acp.ContentBlock{
				Type: "image",
				Extra: map[string]any{
					"data":     att.Data,
					"mimeType": att.MimeType,
				},
			})
		case "file":
			// Phase 1: agents without a file-upload API only see a
			// synthetic summary for inline data; URI-only files become
			// `resource_link` blocks (the agent can fetch via its own
			// fs/read_text_file capability if it supports it).
			if att.Data != "" {
				blocks = append(blocks, acp.ContentBlock{
					Type: "text",
					Text: fmt.Sprintf("[File: %s, Type: %s, Size: %d bytes (base64 encoded)]",
						att.Name, att.MimeType, len(att.Data)),
				})
			} else if att.URI != "" {
				extra := map[string]any{}
				if att.Name != "" {
					extra["name"] = att.Name
				}
				if att.MimeType != "" {
					extra["mimeType"] = att.MimeType
				}
				blocks = append(blocks, acp.ContentBlock{
					Type:  "resource_link",
					URI:   att.URI,
					Extra: extra,
				})
			}
		}
	}
	return blocks
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
//
// Notifications can arrive while a `chat.create_session` is still in flight
// (Devin emits configOptions / modes / availableCommands before answering
// session/new). For those, we don't yet have a daemon session record to
// route to, so the update is buffered keyed by native session id and
// drained by handleCreateSession after registration.
func (r *Router) handleAgentUpdate(nativeSessionID string, update json.RawMessage) {
	r.mu.Lock()
	var (
		s        *session
		daemonID string
	)
	for _, sess := range r.sessions {
		if sess.NativeID == nativeSessionID {
			s = sess
			daemonID = sess.ID
			break
		}
	}
	if s == nil {
		// Session not registered yet — buffer for handleCreateSession to
		// drain. We copy the bytes because json.RawMessage from the ACP
		// decoder is reused across notifications.
		buf := append([]byte(nil), update...)
		r.earlyUpdates[nativeSessionID] = append(r.earlyUpdates[nativeSessionID], buf)
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()

	seq := s.appendUpdate(update)
	r.applyUpdateStateMutations(s, update)
	r.broadcastEvent(wire.ChatEventUpdate, wire.ChatUpdateEvent{
		SessionID:  daemonID,
		Update:     update,
		SequenceID: seq,
	})
}

// applyUpdateStateMutations folds the known state-mutating session/update
// kinds (current_mode_update, session_info_update) onto the session struct
// so chat.list_sessions and chat.get_history reflect the latest values.
func (r *Router) applyUpdateStateMutations(s *session, update json.RawMessage) {
	var head struct {
		Kind  string  `json:"sessionUpdate"`
		Mode  string  `json:"currentModeId"`
		Title *string `json:"title"`
	}
	if err := json.Unmarshal(update, &head); err != nil {
		return
	}
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

func (r *Router) handleSetConfigOption(env wire.Envelope, req wire.ChatSetConfigOptionRequest) {
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
	if req.ConfigID == "" {
		r.respondErr(env, "bad_request", "config_id required")
		return
	}
	ctx, cancel := context.WithTimeout(r.runCtx, 10*time.Second)
	defer cancel()
	configOptions, err := r.devin.SetConfigOption(ctx, s.NativeID, req.ConfigID, req.Value)
	if err != nil {
		r.respondErr(env, "agent_set_config_option_failed", err.Error())
		return
	}
	// Devin's ACP returns the new configOptions snapshot in the response
	// but — unlike most state changes — does not follow up with a
	// `config_option_update` session/update notification. Synthesise one
	// from the response so the per-session snapshot stays honest (page
	// reload via chat.get_history replays from the snapshot) and other
	// connected clients get a real-time broadcast just as if the agent
	// had volunteered the event itself.
	if len(configOptions) > 0 {
		synthetic, err := json.Marshal(struct {
			SessionUpdate string          `json:"sessionUpdate"`
			ConfigOptions json.RawMessage `json:"configOptions"`
		}{
			SessionUpdate: "config_option_update",
			ConfigOptions: configOptions,
		})
		if err != nil {
			r.logger.Warn("synthesise config_option_update", "err", err)
		} else {
			seq := s.appendUpdate(synthetic)
			r.broadcastEvent(wire.ChatEventUpdate, wire.ChatUpdateEvent{
				SessionID:  s.ID,
				Update:     synthetic,
				SequenceID: seq,
			})
		}
	}
	r.respondOK(env, map[string]string{"config_id": req.ConfigID, "value": req.Value})
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

// askModeID returns the agent mode id that corresponds to a read-only "ask"
// mode, or "" if the agent advertises no such mode. Matches by id first
// (e.g. "ask"), then falls back to a name containing "ask".
func askModeID(modes []wire.SessionMode) string {
	for _, m := range modes {
		if strings.EqualFold(m.ID, "ask") {
			return m.ID
		}
	}
	for _, m := range modes {
		if strings.Contains(strings.ToLower(m.Name), "ask") {
			return m.ID
		}
	}
	return ""
}

// guestDenyDecision is the ACP request_permission outcome we return for
// read-only guest sessions: a "cancelled" outcome the agent treats as "user
// did not grant", without surfacing the prompt to the (powerless) guest.
var guestDenyDecision = json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`)

func (r *Router) handleAgentPermissionRequest(nativeSessionID string, params json.RawMessage) (json.RawMessage, *acp.RPCError) {
	daemonID := r.daemonSessionID(nativeSessionID)

	// Guest (read-only share) sessions never get to approve actions — the
	// relay won't even forward a resolve_permission from a guest. Auto-deny
	// immediately so the agent stays read-only and doesn't hang.
	r.mu.Lock()
	gs, ok := r.sessions[daemonID]
	guestSession := ok && gs.guest
	r.mu.Unlock()
	if guestSession {
		return guestDenyDecision, nil
	}

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
