// Package chatrouter wires the daemon's `chat` WebSocket channel onto
// concrete agent adapters (Devin for Terminal and cursor-agent today;
// Claude Code, Codex later). It owns the per-session state, translates
// between wire envelopes and the agent's ACP-style API, and bridges
// permission requests back to the user.
package chatrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/acp"
	"github.com/Mutech-Innovation/coding-anywhere/internal/agent/acpagent"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/oklog/ulid/v2"
)

// Agent ids on the chat channel. They match the `AgentId` union in
// packages/agents on the TypeScript side.
const (
	AgentDevin  = "devin"
	AgentCursor = "cursor-cli"
)

// AgentBuilder lazily produces a configured ACP adapter. The router calls
// it the first time a session is opened against an agent. If the adapter
// crashes, the router will rebuild on the next session.
type AgentBuilder func(opts acpagent.Options) (*acpagent.Adapter, error)

// Builders maps agent ids to their adapter builders. Agents without an
// entry are reported as unavailable.
type Builders map[string]AgentBuilder

// DefaultDevinBuilder wraps `devin acp` as a subprocess.
func DefaultDevinBuilder(opts acpagent.Options) (*acpagent.Adapter, error) {
	opts.LogName = "devin acp"
	return acpagent.NewSubprocess(exec.Command("devin", "acp"), opts)
}

// DefaultCursorBuilder wraps `cursor-agent acp` as a subprocess. Cursor's
// ACP server requires an explicit authenticate(cursor_login) call after
// initialize — credentials come from `cursor-agent login` on this machine.
//
// The generation model of a cursor-agent process is fixed at spawn time via
// `--model`; its ACP `session/set_model` only persists a preference for
// FUTURE processes. The router therefore runs one subprocess per cursor
// session (see perSessionProcess) and passes Options.Model here.
func DefaultCursorBuilder(opts acpagent.Options) (*acpagent.Adapter, error) {
	opts.LogName = "cursor-agent acp"
	opts.AuthMethodID = "cursor_login"
	opts.AuthHint = "run `cursor-agent login` on the daemon machine"
	args := []string{"acp"}
	if opts.Model != "" {
		args = []string{"--model", opts.Model, "acp"}
	}
	return acpagent.NewSubprocess(exec.Command("cursor-agent", args...), opts)
}

// DefaultBuilders returns the standard agent wiring (Devin + cursor-agent).
func DefaultBuilders() Builders {
	return Builders{
		AgentDevin:  DefaultDevinBuilder,
		AgentCursor: DefaultCursorBuilder,
	}
}

// Router owns chat state and fans daemon ↔ relay ↔ ACP traffic.
type Router struct {
	logger *slog.Logger
	cli    *relayclient.Client

	builders Builders
	// defaultWiring is true when New was called with nil builders (i.e. the
	// stock subprocess builders). Only then does chat.agents gate
	// availability on the CLI binary being present in PATH — injected
	// builders (tests, embedders) are trusted as-is.
	defaultWiring bool

	// runCtx lives as long as the router. ACP read loops are pinned to it
	// so that per-request timeouts (e.g. on session/new) don't kill the
	// long-running stdio session.
	runCtx    context.Context
	runCancel context.CancelFunc

	mu         sync.Mutex
	adapters   map[string]*acpagent.Adapter // agent id → adapter; nil until first session
	sessions   map[string]*session
	defaultCwd string
	// earlyUpdates buffers session/update notifications that arrive while a
	// `chat.create_session` (and the underlying ACP `session/new`) is still
	// in flight — Devin sends configOptions / modes / availableCommands
	// before answering session/new, so we'd otherwise drop them on the
	// floor (no daemon-side session record yet) and the web UI would never
	// see the initial model / mode metadata. Keyed by agent id + native
	// session id. Drained by handleCreateSession once the session is
	// registered.
	earlyUpdates map[string][]json.RawMessage

	pendingMu sync.Mutex
	pending   map[string]*pendingPermission // request_id → channel

	eventSeq atomic.Uint64
}

type session struct {
	wire.ChatSession
	cancelCtx context.CancelFunc

	// adapter is the ACP adapter this session runs on. Set at creation.
	adapter *acpagent.Adapter

	// guest marks a session created by a read-only "ask"-mode share link
	// (relay-stamped Origin=guest). Such sessions are forced into ask mode at
	// creation and auto-deny every agent permission request.
	guest bool

	// modes captured from session/new, updated by current_mode_update.
	modes         []wire.SessionMode
	currentModeID string

	// models captured from session/new for agents that use the ACP
	// session-model extension (cursor-agent). nil for agents that model
	// the active model as a config option instead (Devin). The router
	// synthesises a `config_option_update` from this state so the web
	// model picker works the same for both styles.
	models *acp.SessionModelState

	// suppressUpdates drops incoming session/update notifications for this
	// session while a model-switch restart replays history via session/load
	// (the replay would otherwise duplicate the transcript on clients).
	suppressUpdates atomic.Bool

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
// builders maps agent ids to adapter builders; nil means DefaultBuilders()
// (Devin + cursor-agent subprocesses).
//
// defaultCwd, if non-empty, is used when a session is created without an
// explicit Cwd. Pass an empty value (or omit the arg) to require clients to
// always pick a cwd — useful when the daemon's files workspace points at a
// broad root like $HOME that Devin would refuse to operate on.
func New(cli *relayclient.Client, logger *slog.Logger, builders Builders, defaultCwd ...string) *Router {
	defaultWiring := builders == nil
	if builders == nil {
		builders = DefaultBuilders()
	}
	cwd := ""
	if len(defaultCwd) > 0 {
		cwd = defaultCwd[0]
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Router{
		logger:        logger,
		cli:           cli,
		builders:      builders,
		defaultWiring: defaultWiring,
		runCtx:        ctx,
		runCancel:     cancel,
		adapters:      make(map[string]*acpagent.Adapter),
		sessions:      make(map[string]*session),
		defaultCwd:    cwd,
		earlyUpdates:  make(map[string][]json.RawMessage),
		pending:       make(map[string]*pendingPermission),
	}
	cli.Handle(wire.ChannelChat, r.handle)
	return r
}

// Close terminates any open agent sessions and shuts down the underlying
// agent adapters (both the shared per-agent ones and any per-session
// subprocesses).
func (r *Router) Close() error {
	r.runCancel()
	r.mu.Lock()
	defer r.mu.Unlock()
	var firstErr error
	for _, s := range r.sessions {
		if s.cancelCtx != nil {
			s.cancelCtx()
		}
		if r.perSessionProcess(s.AgentID) && s.adapter != nil {
			if err := s.adapter.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	r.sessions = map[string]*session{}
	for id, a := range r.adapters {
		if err := a.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(r.adapters, id)
	}
	return firstErr
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
			r.describeAgent(AgentDevin, "Devin for Terminal", "devin"),
			{ID: "claude-code", Name: "Claude Code", Available: false, Reason: "adapter lands in v2"},
			{ID: "codex", Name: "Codex", Available: false, Reason: "adapter lands in v2"},
			r.describeAgent(AgentCursor, "cursor-agent", "cursor-agent"),
		},
	})
}

// describeAgent reports whether a wired agent is usable on this daemon: it
// needs a registered builder and (for the default subprocess builders) the
// CLI binary on PATH.
func (r *Router) describeAgent(id, name, binary string) wire.AgentDescriptor {
	d := wire.AgentDescriptor{ID: id, Name: name}
	if _, ok := r.builders[id]; !ok {
		d.Reason = "not configured on this daemon"
		return d
	}
	if r.defaultWiring {
		if _, err := exec.LookPath(binary); err != nil {
			d.Reason = fmt.Sprintf("%s not found in PATH", binary)
			return d
		}
	}
	d.Available = true
	return d
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
		req.AgentID = AgentDevin
	}
	if _, ok := r.builders[req.AgentID]; !ok {
		r.respondErr(env, "agent_unavailable",
			fmt.Sprintf("agent %q is not available on this daemon", req.AgentID))
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
	var (
		a   *acpagent.Adapter
		err error
	)
	if r.perSessionProcess(req.AgentID) {
		// cursor-agent: one subprocess per session, model pinned at spawn
		// (set_model can't change a running process's generation model).
		a, err = r.buildAdapterForSession(req.AgentID, req.Model)
	} else {
		a, err = r.ensureAgent(req.AgentID)
	}
	if err != nil {
		r.respondErr(env, "agent_start_failed", err.Error())
		return
	}
	// Bound the new-session call but keep the agent's run loop on the
	// long-lived router context.
	callCtx, cancel := context.WithTimeout(r.runCtx, 30*time.Second)
	defer cancel()
	res, err := a.NewSession(callCtx, req.Cwd, nil)
	if err != nil {
		r.respondErr(env, "agent_new_session_failed", err.Error())
		if r.perSessionProcess(req.AgentID) {
			_ = a.Close() // don't leak the dedicated subprocess
		}
		return
	}
	nativeID := res.SessionID
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
		adapter: a,
		models:  res.Models,
	}
	// When the process was spawned pinned to a model (cursor --model), trust
	// the pin over the agent's reported current model — cursor's session/new
	// can still report the stale persisted preference even though generation
	// uses the pinned model.
	if req.Model != "" && r.perSessionProcess(req.AgentID) && s.models != nil {
		s.models.CurrentModelID = req.Model
	}
	if res.Modes != nil {
		s.modes = make([]wire.SessionMode, len(res.Modes.AvailableModes))
		for i, m := range res.Modes.AvailableModes {
			s.modes[i] = wire.SessionMode{ID: m.ID, Name: m.Name, Description: m.Description}
		}
		s.currentModeID = res.Modes.CurrentModeID
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
		// Lock the owner-chosen model (the relay injects it into the guest's
		// create_session). The guest can't change it — the relay blocks
		// set_config_option. Per-session-process agents (cursor) were already
		// spawned with `--model`, so only the shared-process path (Devin)
		// needs a post-create call here.
		if req.Model != "" && !r.perSessionProcess(req.AgentID) {
			setCtx, setCancel := context.WithTimeout(r.runCtx, 10*time.Second)
			if _, err := s.adapter.SetConfigOption(setCtx, nativeID, modelConfigID, req.Model); err != nil {
				r.logger.Warn("force model for guest session failed",
					"native", nativeID, "model", req.Model, "err", err)
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
	bufKey := earlyKey(req.AgentID, nativeID)
	buffered := r.earlyUpdates[bufKey]
	delete(r.earlyUpdates, bufKey)
	r.sessions[id] = s
	r.mu.Unlock()

	// Agents using the ACP session-model extension (cursor-agent) advertise
	// models inline on session/new instead of via a config_option_update
	// notification. Synthesise that update so the web model picker works
	// identically for both styles.
	if synthetic := r.modelConfigUpdate(s); synthetic != nil {
		buffered = append(buffered, synthetic)
	}

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

// perSessionProcess reports whether the agent runs one subprocess per chat
// session. cursor-agent does: the generation model of a running cursor
// process is fixed at spawn (`--model`); its ACP session/set_model only
// persists a preference for future processes. One process per session keeps
// each session's model independent, and a model switch restarts the process
// (resuming the conversation via session/load).
func (r *Router) perSessionProcess(agentID string) bool {
	return agentID == AgentCursor
}

// restartSessionWithModel switches a per-session-process agent (cursor) to a
// new model: spawn a fresh subprocess pinned to the model, kill the old
// process (cursor refuses to session/load a chat that another live process
// still holds open), then resume the same native session in the new process
// via session/load — suppressing the history replay so clients don't see a
// duplicated transcript. If the resume fails, a best-effort restore brings
// the session back on a process pinned to the previous model.
func (r *Router) restartSessionWithModel(ctx context.Context, s *session, model string) error {
	r.mu.Lock()
	if s.Status != "idle" {
		r.mu.Unlock()
		return errors.New("finish the current turn before switching model")
	}
	old := s.adapter
	prevModel := ""
	if s.models != nil {
		prevModel = s.models.CurrentModelID
	}
	r.mu.Unlock()

	a, err := r.buildAdapterForSession(s.AgentID, model)
	if err != nil {
		return err
	}
	if old != nil {
		_ = old.Close()
	}

	load := func(target *acpagent.Adapter) error {
		s.suppressUpdates.Store(true)
		defer s.suppressUpdates.Store(false)
		return target.LoadSession(ctx, s.NativeID, s.Cwd, nil)
	}

	if err := load(a); err != nil {
		_ = a.Close()
		// The old process is already gone — try to restore the session on a
		// process pinned to the previous model so it keeps working.
		if restored, rerr := r.buildAdapterForSession(s.AgentID, prevModel); rerr == nil {
			if lerr := load(restored); lerr == nil {
				r.mu.Lock()
				s.adapter = restored
				r.mu.Unlock()
			} else {
				_ = restored.Close()
			}
		}
		return fmt.Errorf("resume session on new process: %w", err)
	}
	r.mu.Lock()
	s.adapter = a
	if s.models != nil {
		s.models.CurrentModelID = model
	}
	r.mu.Unlock()
	return nil
}

// modelConfigUpdate synthesises a `config_option_update` session/update from
// the session's ACP models state. Returns nil when the agent didn't
// advertise models (Devin pushes real config_option_update events instead).
func (r *Router) modelConfigUpdate(s *session) json.RawMessage {
	r.mu.Lock()
	models := s.models
	r.mu.Unlock()
	if models == nil || len(models.AvailableModels) == 0 {
		return nil
	}
	type choice struct {
		Value       string `json:"value"`
		Label       string `json:"label"`
		Description string `json:"description,omitempty"`
	}
	choices := make([]choice, len(models.AvailableModels))
	for i, m := range models.AvailableModels {
		label := m.Name
		if label == "" {
			label = m.ModelID
		}
		choices[i] = choice{Value: m.ModelID, Label: label, Description: m.Description}
	}
	synthetic, err := json.Marshal(map[string]any{
		"sessionUpdate": "config_option_update",
		"configOptions": []map[string]any{{
			"id":           modelConfigID,
			"name":         "Model",
			"type":         "select",
			"currentValue": models.CurrentModelID,
			"options":      choices,
		}},
	})
	if err != nil {
		r.logger.Warn("synthesise model config_option_update", "err", err)
		return nil
	}
	return synthetic
}

func (r *Router) handlePrompt(env wire.Envelope, req wire.ChatPromptRequest) {
	r.mu.Lock()
	s, ok := r.sessions[req.SessionID]
	r.mu.Unlock()
	if !ok {
		r.respondErr(env, "not_found", "unknown session")
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
		stop, err := s.adapter.Prompt(ctx, s.NativeID, contentBlocks)
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
	if s.adapter != nil {
		_ = s.adapter.Cancel(s.NativeID)
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
	// Per-session-process agents (cursor) own their subprocess — reap it.
	if r.perSessionProcess(s.AgentID) && s.adapter != nil {
		_ = s.adapter.Close()
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

// ensureAgent lazily creates and starts the shared adapter for one agent id
// on first use. The adapter's run loop is pinned to r.runCtx so it survives
// request-level timeouts. Per-session-process agents (cursor) bypass this
// cache and call buildAdapter directly.
func (r *Router) ensureAgent(agentID string) (*acpagent.Adapter, error) {
	r.mu.Lock()
	if a, ok := r.adapters[agentID]; ok {
		r.mu.Unlock()
		return a, nil
	}
	r.mu.Unlock()

	a, err := r.buildAdapter(agentID, "")
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.adapters[agentID] = a
	r.mu.Unlock()
	return a, nil
}

// buildAdapter constructs and starts a fresh adapter for the agent, wiring
// the router's update/permission callbacks. model, when non-empty, is passed
// to the builder so subprocess agents can pin it at spawn (cursor --model).
func (r *Router) buildAdapter(agentID, model string) (*acpagent.Adapter, error) {
	r.mu.Lock()
	builder, ok := r.builders[agentID]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no builder for agent %q", agentID)
	}
	a, err := builder(acpagent.Options{
		Logger: r.logger,
		Model:  model,
		OnSessionUpdate: func(sessionID string, update json.RawMessage) {
			r.handleAgentUpdate(agentID, sessionID, update)
		},
		OnPermissionRequest: func(_ context.Context, sessionID string, params json.RawMessage) (json.RawMessage, *acp.RPCError) {
			return r.handleAgentPermissionRequest(agentID, sessionID, params)
		},
	})
	if err != nil {
		return nil, err
	}
	if err := a.Start(r.runCtx); err != nil {
		_ = a.Close()
		return nil, err
	}
	return a, nil
}

// buildAdapterForSession builds a per-session adapter, retrying with
// alternative model-id spellings when the spawn dies on the requested one.
//
// cursor-agent's `--model` flag speaks a different id dialect
// (`claude-fable-5-thinking-high`) than the decorated ids its ACP advertises
// in availableModels (`claude-fable-5[thinking=true,context=300k,effort=high]`).
// MOST decorated ids happen to parse, but some don't — the process then
// prints "Cannot use this model: ..." and exits, killing the handshake. Try
// the exact id first, then a translated flag spelling, then the bare base.
func (r *Router) buildAdapterForSession(agentID, model string) (*acpagent.Adapter, error) {
	candidates := modelSpawnCandidates(model)
	var firstErr error
	for i, m := range candidates {
		a, err := r.buildAdapter(agentID, m)
		if err == nil {
			if i > 0 {
				r.logger.Info("agent spawn fell back to alternate model id",
					"agent", agentID, "requested", model, "used", m)
			}
			return a, nil
		}
		if firstErr == nil {
			firstErr = err
		}
		r.logger.Warn("agent spawn failed for model candidate",
			"agent", agentID, "model", m, "err", err)
	}
	return nil, firstErr
}

// modelSpawnCandidates expands a decorated ACP model id into the spellings
// worth trying as a spawn-time `--model` value, most specific first:
//
//	claude-fable-5[thinking=true,context=300k,effort=high]
//	  → [ itself, "claude-fable-5-thinking-high", "claude-fable-5" ]
//
// Param translation mirrors cursor's flag dialect: thinking=true →
// "-thinking", effort/reasoning level → "-<level>", fast=true → "-fast".
// Ids without brackets (or an empty model) get no fallbacks.
func modelSpawnCandidates(model string) []string {
	out := []string{model}
	open := strings.IndexByte(model, '[')
	if model == "" || open <= 0 || !strings.HasSuffix(model, "]") {
		return out
	}
	base := model[:open]
	params := map[string]string{}
	for _, kv := range strings.Split(model[open+1:len(model)-1], ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			params[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	translated := base
	if params["thinking"] == "true" {
		translated += "-thinking"
	}
	level := params["effort"]
	if level == "" {
		level = params["reasoning"]
	}
	switch level {
	case "low", "medium", "high", "xhigh", "max":
		translated += "-" + level
	}
	if params["fast"] == "true" {
		translated += "-fast"
	}
	add := func(c string) {
		for _, existing := range out {
			if existing == c {
				return
			}
		}
		out = append(out, c)
	}
	add(translated)
	add(base)
	return out
}

// earlyKey namespaces buffered pre-registration updates by agent so two
// agents can never cross-pollinate sessions, even on native-id collision.
func earlyKey(agentID, nativeID string) string { return agentID + "\x00" + nativeID }

// daemonSessionID maps an agent's native session id back to our daemon-side id.
func (r *Router) daemonSessionID(agentID, nativeID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sessions {
		if s.AgentID == agentID && s.NativeID == nativeID {
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
// route to, so the update is buffered keyed by agent + native session id
// and drained by handleCreateSession after registration.
func (r *Router) handleAgentUpdate(agentID, nativeSessionID string, update json.RawMessage) {
	r.mu.Lock()
	var (
		s        *session
		daemonID string
	)
	for _, sess := range r.sessions {
		if sess.AgentID == agentID && sess.NativeID == nativeSessionID {
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
		key := earlyKey(agentID, nativeSessionID)
		r.earlyUpdates[key] = append(r.earlyUpdates[key], buf)
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()

	// Drop the history replay emitted by session/load during a model-switch
	// restart — clients already have the transcript.
	if s.suppressUpdates.Load() {
		return
	}

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
	if req.ModeID == "" {
		r.respondErr(env, "bad_request", "mode_id required")
		return
	}
	ctx, cancel := context.WithTimeout(r.runCtx, 10*time.Second)
	defer cancel()
	if err := s.adapter.SetMode(ctx, s.NativeID, req.ModeID); err != nil {
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
	if req.ConfigID == "" {
		r.respondErr(env, "bad_request", "config_id required")
		return
	}

	// Agents on the ACP session-model extension (cursor-agent) have no
	// config options; the daemon synthesised the "model" option from the
	// session/new models payload. cursor can't change a running process's
	// model, so a write to that option restarts the session's subprocess
	// pinned to the new model and resumes the conversation via session/load.
	// The restart (spawn + initialize + authenticate + load) takes seconds,
	// hence the larger deadline.
	if s.models != nil && req.ConfigID == modelConfigID {
		ctx, cancel := context.WithTimeout(r.runCtx, 60*time.Second)
		defer cancel()
		if err := r.restartSessionWithModel(ctx, s, req.Value); err != nil {
			r.respondErr(env, "agent_set_config_option_failed", err.Error())
			return
		}
		if synthetic := r.modelConfigUpdate(s); synthetic != nil {
			seq := s.appendUpdate(synthetic)
			r.broadcastEvent(wire.ChatEventUpdate, wire.ChatUpdateEvent{
				SessionID:  s.ID,
				Update:     synthetic,
				SequenceID: seq,
			})
		}
		r.respondOK(env, map[string]string{"config_id": req.ConfigID, "value": req.Value})
		return
	}

	ctx, cancel := context.WithTimeout(r.runCtx, 10*time.Second)
	defer cancel()
	configOptions, err := s.adapter.SetConfigOption(ctx, s.NativeID, req.ConfigID, req.Value)
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

// modelConfigID is the session-scoped config option id Devin uses for the
// active model. The web model picker keys off the same id (lower-cased), and
// guest "ask"-share sessions force the owner-chosen model via this option.
// For agents on the ACP session-model extension (cursor-agent), the daemon
// synthesises a config option under the same id from the session/new models
// payload so the picker works identically.
const modelConfigID = "model"

// agentDisplayName renders an agent id for human-facing notification text.
func agentDisplayName(agentID string) string {
	switch agentID {
	case AgentDevin:
		return "Devin"
	case AgentCursor:
		return "Cursor"
	case "":
		return "The agent"
	default:
		return agentID
	}
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

func (r *Router) handleAgentPermissionRequest(agentID, nativeSessionID string, params json.RawMessage) (json.RawMessage, *acp.RPCError) {
	daemonID := r.daemonSessionID(agentID, nativeSessionID)

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
	agentID := s.AgentID
	r.mu.Unlock()
	r.broadcastEvent(wire.ChatEventSessionStatus, wire.ChatSessionStatusEvent{
		SessionID: id, Status: status,
	})
	// Push a Web Push when the agent finishes a turn so backgrounded
	// devices see the result without keeping the tab open.
	if status == "idle" && (prev == "thinking" || prev == "awaiting_permission") {
		body := agentDisplayName(agentID) + " has finished thinking."
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
