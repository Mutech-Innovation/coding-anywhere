// Package devin is the daemon's adapter for Devin for Terminal.
//
// It owns a `devin acp` subprocess (or an injected stdio pair, for tests),
// runs the ACP client against it, performs the initialize handshake, and
// exposes a small high-level surface (NewSession / Prompt / Cancel) plus a
// notification stream for session/update events.
//
// Higher-level concerns (chat-channel routing, slash command emulation,
// MCP plumbing) live in callers.
package devin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"

	"github.com/Mutech-Innovation/coding-anywhere/internal/acp"
	"github.com/Mutech-Innovation/coding-anywhere/internal/version"
)

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
		// stderr inherits the daemon's so error logs surface.
		a.cmd.Stderr = nil
		if err := a.cmd.Start(); err != nil {
			return fmt.Errorf("start subprocess: %w", err)
		}
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
		return fmt.Errorf("initialize: %w", err)
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
	return nil
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

// NewSession creates a new conversation in `cwd` and returns the session
// id along with the modes the agent advertised (may be nil).
func (a *Adapter) NewSession(ctx context.Context, cwd string, mcp []acp.MCPServer) (string, *acp.SessionModeState, error) {
	if err := a.Start(ctx); err != nil {
		return "", nil, err
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
		return "", nil, err
	}
	return res.SessionID, res.Modes, nil
}

// SetMode forwards an ACP `session/set_mode` request to the agent.
func (a *Adapter) SetMode(ctx context.Context, sessionID, modeID string) error {
	return a.cli.Call(ctx, acp.MethodSessionSetMode, acp.SessionSetModeRequest{
		SessionID: sessionID,
		ModeID:    modeID,
	}, nil)
}

// LoadSession resumes a previously persisted session. Requires the agent
// to advertise the loadSession capability.
func (a *Adapter) LoadSession(ctx context.Context, sessionID, cwd string, mcp []acp.MCPServer) error {
	if !a.Capabilities().LoadSession {
		return ErrUnsupported
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

// DefaultBinary returns the default `devin acp` invocation.
func DefaultBinary() *exec.Cmd { return exec.Command("devin", "acp") }
