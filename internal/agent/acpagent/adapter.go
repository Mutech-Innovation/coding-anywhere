// Package acpagent is the daemon's generic adapter for ACP-speaking coding
// agents (Devin for Terminal, cursor-agent, ...).
//
// It owns an agent subprocess (e.g. `devin acp`, `cursor-agent acp`) or an
// injected stdio pair (for tests), runs the ACP client against it, performs
// the initialize (+ optional authenticate) handshake, and exposes a small
// high-level surface (NewSession / Prompt / Cancel / SetMode / SetModel /
// SetConfigOption) plus a notification stream for session/update events.
//
// Higher-level concerns (chat-channel routing, per-agent model plumbing,
// MCP wiring) live in callers — see internal/chatrouter.
package acpagent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/acp"
	"github.com/Mutech-Innovation/coding-anywhere/internal/version"
)

// authenticateTimeout bounds the post-initialize authenticate call. A
// healthy logged-in agent answers near-instantly; a stale login can hang
// the call forever, which would otherwise wedge the first create_session.
const authenticateTimeout = 30 * time.Second

// AuthFailedError marks a failure of the post-initialize authenticate call
// (bad/stale credentials, or the agent's auth backend being unreachable —
// cursor-agent's authenticate needs the network even when already logged
// in). Callers use it to tell auth failures apart from model-spawn failures:
// retrying with a different --model spelling can fix the latter but never
// the former (see chatrouter's buildAdapterForSession).
type AuthFailedError struct{ Err error }

func (e *AuthFailedError) Error() string { return e.Err.Error() }
func (e *AuthFailedError) Unwrap() error { return e.Err }

// Options configure the adapter.
type Options struct {
	Logger *slog.Logger

	// OnSessionUpdate is called for each session/update notification the
	// agent pushes. The update body is the raw `update` field — callers
	// typically branch on its `sessionUpdate` discriminator.
	OnSessionUpdate func(sessionID string, update json.RawMessage)

	// OnPermissionRequest is invoked for session/request_permission. The
	// callback should return either a JSON result body OR an *acp.RPCError.
	// If the callback is nil, all permission requests are denied with a
	// stable error code.
	OnPermissionRequest func(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, *acp.RPCError)

	// ClientName / ClientTitle are advertised in the initialize handshake.
	ClientName  string
	ClientTitle string

	// LogName labels subprocess stderr lines in the daemon log (e.g.
	// "devin acp", "cursor-agent acp"). Defaults to "acp agent".
	LogName string

	// AuthMethodID, when non-empty, makes Start call `authenticate` with
	// this method id right after initialize — but only if the agent
	// advertised the method in its initialize response. cursor-agent
	// requires this (`cursor_login`) even when `cursor-agent login` has
	// already stored credentials; Devin needs no authenticate call.
	AuthMethodID string

	// AuthHint is appended to authenticate errors to tell the operator how
	// to fix credentials (e.g. "run `cursor-agent login` on this machine").
	AuthHint string

	// Model, when non-empty, asks the builder to pin the agent's generation
	// model at spawn time (e.g. cursor-agent's `--model` flag). Builders for
	// agents that select models per session over ACP (Devin) ignore it.
	//
	// cursor-agent needs this: its ACP `session/set_model` only persists a
	// preference for future processes — the generation model of a running
	// process is fixed when it starts.
	Model string
}

// NewSessionResult carries everything the agent returned from session/new.
type NewSessionResult struct {
	SessionID string
	// Modes the agent advertised (nil if the agent has no mode concept).
	Modes *acp.SessionModeState
	// Models the agent advertised (nil for agents that surface model
	// selection via config options instead, like Devin).
	Models *acp.SessionModelState
}

// Adapter wraps the ACP client + subprocess lifecycle.
type Adapter struct {
	logger *slog.Logger
	opts   Options

	// One of `cmd` (subprocess mode) or `stdin/stdout` (injected mode) is set.
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	cli       *acp.Client
	startOnce sync.Once
	startErr  error

	mu          sync.Mutex
	initialized bool
	initRes     acp.InitializeResponse
	runDone     chan error

	// stderrTail keeps the last few subprocess stderr lines so handshake
	// errors can say WHY the process died (e.g. cursor-agent's "Cannot use
	// this model: ..." exits between initialize and authenticate).
	stderrMu   sync.Mutex
	stderrTail []string
}

// NewSubprocess returns an adapter that, on Start, will spawn the given
// command (e.g. `devin acp`). The command must NOT be started yet.
func NewSubprocess(cmd *exec.Cmd, opts Options) (*Adapter, error) {
	if cmd == nil {
		return nil, errors.New("cmd required")
	}
	a := newAdapter(opts)
	a.cmd = cmd
	return a, nil
}

// NewWithStreams wires the adapter to a pre-existing stdin/stdout pair.
// Useful for tests.
func NewWithStreams(stdin io.WriteCloser, stdout io.ReadCloser, opts Options) *Adapter {
	a := newAdapter(opts)
	a.stdin = stdin
	a.stdout = stdout
	return a
}

func newAdapter(opts Options) *Adapter {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.ClientName == "" {
		opts.ClientName = "coding-anywhere"
	}
	if opts.ClientTitle == "" {
		opts.ClientTitle = "Coding Anywhere"
	}
	if opts.LogName == "" {
		opts.LogName = "acp agent"
	}
	return &Adapter{logger: opts.Logger, opts: opts, runDone: make(chan error, 1)}
}

// Start spawns the subprocess (if applicable), starts the ACP client read
// loop in a goroutine, and performs the initialize handshake. It is safe to
// call Start more than once; subsequent calls return the original result.
func (a *Adapter) Start(ctx context.Context) error {
	a.startOnce.Do(func() {
		a.startErr = a.start(ctx)
	})
	return a.startErr
}

func (a *Adapter) start(ctx context.Context) error {
	if a.cmd != nil {
		stdin, err := a.cmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("stdin pipe: %w", err)
		}
		stdout, err := a.cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("stdout pipe: %w", err)
		}
		// Capture stderr and forward each line to the daemon logger.
		// `devin acp` is fairly chatty (INFO-level startup banner + per-
		// session breadcrumbs) but its WARN/ERROR lines are the only way
		// to see what's wrong when the agent returns a generic JSON-RPC
		// error — e.g. Cognition backend "Permission denied (trace ID: …)"
		// would otherwise be silently dropped.
		stderr, err := a.cmd.StderrPipe()
		if err != nil {
			return fmt.Errorf("stderr pipe: %w", err)
		}
		if err := a.cmd.Start(); err != nil {
			return fmt.Errorf("start subprocess: %w", err)
		}
		go a.pumpStderr(stderr)
		a.stdin = stdin
		a.stdout = stdout
	}

	a.cli = acp.New(a.stdout, a.stdin, acp.Options{
		Logger:         a.logger,
		OnNotification: a.onNotification,
		OnRequest:      a.onRequest,
	})
	go func() { a.runDone <- a.cli.Run(ctx) }()

	// Initialize handshake.
	//
	// We deliberately advertise *no* optional client capabilities for now —
	// the agent will fall back to its built-in tools (its own filesystem
	// read/write, its own terminal). When P5 (file browser) lands we will
	// flip on `fs.readTextFile` / `fs.writeTextFile` and route them through
	// the chat-channel client.
	var initRes acp.InitializeResponse
	if err := a.cli.Call(ctx, acp.MethodInitialize, acp.InitializeRequest{
		ProtocolVersion:    acp.ProtocolVersion,
		ClientCapabilities: acp.ClientCapabilities{},
		ClientInfo: acp.ImplementationInfo{
			Name:    a.opts.ClientName,
			Title:   a.opts.ClientTitle,
			Version: version.Version,
		},
	}, &initRes); err != nil {
		return fmt.Errorf("initialize: %w%s", err, a.stderrSuffix())
	}
	a.mu.Lock()
	a.initialized = true
	a.initRes = initRes
	a.mu.Unlock()
	a.logger.Info("acp initialized",
		"agent", initRes.AgentInfo.Name,
		"agent_version", initRes.AgentInfo.Version,
		"protocol_version", initRes.ProtocolVersion,
	)

	if err := a.authenticate(ctx, initRes); err != nil {
		return err
	}
	return nil
}

// authenticate performs the optional post-initialize authenticate call when
// the adapter is configured with an AuthMethodID the agent advertises.
// cursor-agent rejects session/new with "Authentication required" until this
// runs, even when CLI credentials are already on disk.
func (a *Adapter) authenticate(ctx context.Context, initRes acp.InitializeResponse) error {
	if a.opts.AuthMethodID == "" {
		return nil
	}
	advertised := false
	for _, m := range initRes.AuthMethods {
		if m.ID == a.opts.AuthMethodID {
			advertised = true
			break
		}
	}
	if !advertised {
		a.logger.Debug("agent does not advertise auth method; skipping authenticate",
			"method", a.opts.AuthMethodID)
		return nil
	}
	// A stale CLI login can make authenticate hang forever instead of
	// erroring; bound it so the first create_session fails with a useful
	// message instead of wedging.
	authCtx, cancel := context.WithTimeout(ctx, authenticateTimeout)
	defer cancel()
	if err := a.cli.Call(authCtx, acp.MethodAuthenticate, acp.AuthenticateRequest{
		MethodID: a.opts.AuthMethodID,
	}, nil); err != nil {
		if a.opts.AuthHint != "" {
			return &AuthFailedError{Err: fmt.Errorf("authenticate (%s): %w%s — %s",
				a.opts.AuthMethodID, err, a.stderrSuffix(), a.opts.AuthHint)}
		}
		return &AuthFailedError{Err: fmt.Errorf("authenticate (%s): %w%s",
			a.opts.AuthMethodID, err, a.stderrSuffix())}
	}
	a.logger.Info("acp authenticated", "method", a.opts.AuthMethodID)
	return nil
}

// stderrSuffix renders the subprocess's recent stderr for embedding in
// handshake errors. The process usually prints its reason for dying there
// (e.g. "Cannot use this model: ..."), which would otherwise only be visible
// at debug log level. A short grace period lets the pump drain lines that
// raced with the failure.
func (a *Adapter) stderrSuffix() string {
	if a.cmd == nil {
		return "" // stream-injected (tests) — no subprocess stderr
	}
	time.Sleep(200 * time.Millisecond)
	a.stderrMu.Lock()
	defer a.stderrMu.Unlock()
	if len(a.stderrTail) == 0 {
		return ""
	}
	return " (stderr: " + strings.Join(a.stderrTail, " | ") + ")"
}

// Initialized reports whether the initialize handshake has completed.
func (a *Adapter) Initialized() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.initialized
}

// AgentInfo returns the agent-info response from initialize.
func (a *Adapter) AgentInfo() acp.ImplementationInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.initRes.AgentInfo
}

// Capabilities returns the agent capabilities advertised at initialize.
func (a *Adapter) Capabilities() acp.AgentCapabilities {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.initRes.AgentCapabilities
}

// NewSession creates a new conversation in `cwd` and returns the session id
// along with the modes and models the agent advertised (either may be nil).
func (a *Adapter) NewSession(ctx context.Context, cwd string, mcp []acp.MCPServer) (NewSessionResult, error) {
	if err := a.Start(ctx); err != nil {
		return NewSessionResult{}, err
	}
	if mcp == nil {
		// ACP requires `mcpServers` to be present; an explicit empty slice
		// JSON-marshals to `[]` instead of `null`, which some agents reject.
		mcp = []acp.MCPServer{}
	}
	var res acp.SessionNewResponse
	if err := a.cli.Call(ctx, acp.MethodSessionNew, acp.SessionNewRequest{
		Cwd:        cwd,
		MCPServers: mcp,
	}, &res); err != nil {
		return NewSessionResult{}, err
	}
	return NewSessionResult{SessionID: res.SessionID, Modes: res.Modes, Models: res.Models}, nil
}

// SetMode forwards an ACP `session/set_mode` request to the agent.
func (a *Adapter) SetMode(ctx context.Context, sessionID, modeID string) error {
	return a.cli.Call(ctx, acp.MethodSessionSetMode, acp.SessionSetModeRequest{
		SessionID: sessionID,
		ModeID:    modeID,
	}, nil)
}

// SetModel forwards an ACP `session/set_model` request, switching the
// session's active model (cursor-agent style; Devin uses SetConfigOption).
func (a *Adapter) SetModel(ctx context.Context, sessionID, modelID string) error {
	return a.cli.Call(ctx, acp.MethodSessionSetModel, acp.SessionSetModelRequest{
		SessionID: sessionID,
		ModelID:   modelID,
	}, nil)
}

// SetConfigOption forwards an ACP `session/set_config_option` request,
// changing a single session-scoped option (e.g. the active model).
//
// Devin's ACP server returns the new full configOptions snapshot in the
// response body but — unlike most other state changes — does NOT also
// emit a `config_option_update` session/update notification. Callers
// that want to keep their per-session snapshot honest must therefore
// fan out the returned `configOptions` themselves. We surface the raw
// JSON so the chatrouter can synthesise that event without re-parsing
// every option's `options` list.
//
// Returns the raw `configOptions` JSON value from the response. Empty
// (`nil`) if the agent didn't include the field.
func (a *Adapter) SetConfigOption(ctx context.Context, sessionID, configID, value string) (json.RawMessage, error) {
	var raw struct {
		ConfigOptions json.RawMessage `json:"configOptions"`
	}
	if err := a.cli.Call(ctx, acp.MethodSessionSetConfigOption, acp.SessionSetConfigOptionRequest{
		SessionID: sessionID,
		ConfigID:  configID,
		Value:     value,
	}, &raw); err != nil {
		return nil, err
	}
	return raw.ConfigOptions, nil
}

// LoadSession resumes a previously persisted session. Requires the agent
// to advertise the loadSession capability.
func (a *Adapter) LoadSession(ctx context.Context, sessionID, cwd string, mcp []acp.MCPServer) error {
	if !a.Capabilities().LoadSession {
		return ErrUnsupported
	}
	if mcp == nil {
		// Same as NewSession: `mcpServers` must be present and `null` is
		// rejected by some agents (cursor-agent answers -32603).
		mcp = []acp.MCPServer{}
	}
	return a.cli.Call(ctx, acp.MethodSessionLoad, map[string]any{
		"sessionId":  sessionID,
		"cwd":        cwd,
		"mcpServers": mcp,
	}, nil)
}

// Prompt sends a user turn into the session. Returns the stopReason from the
// final response (e.g. "end_turn", "max_tokens").
func (a *Adapter) Prompt(ctx context.Context, sessionID string, blocks []acp.ContentBlock) (string, error) {
	var res acp.SessionPromptResponse
	if err := a.cli.Call(ctx, acp.MethodSessionPrompt, acp.SessionPromptRequest{
		SessionID: sessionID,
		Prompt:    blocks,
	}, &res); err != nil {
		return "", err
	}
	return res.StopReason, nil
}

// PromptText is a convenience helper for the most common case of a single
// text block.
func (a *Adapter) PromptText(ctx context.Context, sessionID, text string) (string, error) {
	return a.Prompt(ctx, sessionID, []acp.ContentBlock{{Type: "text", Text: text}})
}

// Cancel sends a session/cancel notification. The agent will eventually
// respond to the in-flight session/prompt with a "cancelled" stop reason.
func (a *Adapter) Cancel(sessionID string) error {
	return a.cli.Notify(acp.NotificationSessionCancel, acp.SessionCancelNotification{
		SessionID: sessionID,
	})
}

// Close shuts down the ACP client and (if subprocess mode) terminates the
// child process. Safe to call multiple times.
func (a *Adapter) Close() error {
	if a.cli != nil {
		_ = a.cli.Close()
	}
	if a.stdin != nil {
		_ = a.stdin.Close()
	}
	var procErr error
	if a.cmd != nil && a.cmd.Process != nil {
		_ = a.cmd.Process.Kill()
		procErr = a.cmd.Wait()
	}
	if a.stdout != nil {
		_ = a.stdout.Close()
	}
	// Drain runDone so the goroutine doesn't leak.
	select {
	case <-a.runDone:
	default:
	}
	return procErr
}

// Done returns a channel that's closed when the ACP client's read loop ends.
func (a *Adapter) Done() <-chan error { return a.runDone }

func (a *Adapter) onNotification(method string, params json.RawMessage) {
	switch method {
	case acp.NotificationSessionUpdate:
		var n acp.SessionUpdateNotification
		if err := json.Unmarshal(params, &n); err != nil {
			a.logger.Warn("session/update parse", "err", err)
			return
		}
		if a.opts.OnSessionUpdate != nil {
			a.opts.OnSessionUpdate(n.SessionID, n.Update)
		}
	default:
		a.logger.Debug("acp notification", "method", method)
	}
}

func (a *Adapter) onRequest(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, *acp.RPCError) {
	switch method {
	case acp.MethodSessionRequestPermission:
		if a.opts.OnPermissionRequest == nil {
			return nil, &acp.RPCError{Code: -32601, Message: "permission requests not handled"}
		}
		// session/request_permission has a sessionId parameter. We extract it
		// without forcing the caller to know the full schema.
		var hdr struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(params, &hdr)
		return a.opts.OnPermissionRequest(ctx, hdr.SessionID, params)
	default:
		return nil, &acp.RPCError{Code: -32601, Message: fmt.Sprintf("method %q not implemented", method)}
	}
}

// ErrUnsupported is returned when the agent does not advertise an optional
// capability the caller requires (e.g. loadSession).
var ErrUnsupported = errors.New("agent does not support this capability")

// pumpStderr reads the agent subprocess stderr line by line and forwards
// each line to the daemon logger at a severity inferred from the log prefix.
// We deliberately keep this simple — agent logs are already structured but
// not in slog format, so we just preserve them as-is under a "line" key.
func (a *Adapter) pumpStderr(r io.Reader) {
	scanner := bufio.NewScanner(r)
	// Agents can emit fairly long single-line tracebacks; bump the
	// buffer so we don't truncate them.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// Keep a short tail for handshake error messages.
		const maxLine, maxTail = 240, 5
		clipped := line
		if len(clipped) > maxLine {
			clipped = clipped[:maxLine] + "…"
		}
		a.stderrMu.Lock()
		a.stderrTail = append(a.stderrTail, clipped)
		if len(a.stderrTail) > maxTail {
			a.stderrTail = a.stderrTail[len(a.stderrTail)-maxTail:]
		}
		a.stderrMu.Unlock()
		switch {
		case strings.Contains(line, " ERROR "):
			a.logger.Error(a.opts.LogName, "line", line)
		case strings.Contains(line, " WARN "):
			a.logger.Warn(a.opts.LogName, "line", line)
		default:
			a.logger.Debug(a.opts.LogName, "line", line)
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		a.logger.Debug(a.opts.LogName+" stderr ended", "err", err)
	}
}
