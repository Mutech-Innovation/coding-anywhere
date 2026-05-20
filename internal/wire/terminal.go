package wire

import "time"

// Method names on the `terminal` channel. Requests come from clients (web,
// mobile) and are answered by the daemon. Events are pushed by the daemon
// when something interesting happens on a session.
const (
	TerminalMethodOpen   = "open"
	TerminalMethodList   = "list"
	TerminalMethodAttach = "attach"
	TerminalMethodDetach = "detach"
	TerminalMethodInput  = "input"
	TerminalMethodResize = "resize"
	TerminalMethodClose  = "close"

	TerminalEventOpened = "opened"
	TerminalEventClosed = "closed"
	TerminalEventData   = "data"
	TerminalEventExit   = "exit"
)

// TerminalSessionInfo mirrors internal/ptymux.SessionInfo as a wire-shape
// that's stable across the network. We keep both types in lockstep.
type TerminalSessionInfo struct {
	ID        string    `json:"id"`
	PID       int       `json:"pid"`
	Cols      uint16    `json:"cols"`
	Rows      uint16    `json:"rows"`
	Command   []string  `json:"command"`
	Cwd       string    `json:"cwd"`
	StartedAt time.Time `json:"started_at"`
}

// --- Requests (client → daemon) ---

// TerminalOpenRequest spawns a fresh PTY-backed shell on the daemon.
type TerminalOpenRequest struct {
	Command []string `json:"command,omitempty"`
	Cwd     string   `json:"cwd,omitempty"`
	Cols    uint16   `json:"cols,omitempty"`
	Rows    uint16   `json:"rows,omitempty"`
}

// TerminalOpenResponse is the answer to TerminalOpenRequest.
type TerminalOpenResponse struct {
	Session TerminalSessionInfo `json:"session"`
}

// TerminalListResponse is returned for the `list` method (request body empty).
type TerminalListResponse struct {
	Sessions []TerminalSessionInfo `json:"sessions"`
}

// TerminalAttachRequest subscribes the caller to a session's output stream.
// The response includes the current scrollback so the caller can render
// history before the first live data event lands.
type TerminalAttachRequest struct {
	ID string `json:"id"`
}

// TerminalAttachResponse carries the current scrollback as a base64 string.
type TerminalAttachResponse struct {
	Session   TerminalSessionInfo `json:"session"`
	Snapshot  string              `json:"snapshot"` // base64 of current scrollback
	Truncated bool                `json:"truncated,omitempty"`
}

// TerminalDetachRequest stops sending TerminalDataEvent for `id` to the
// caller. It does not stop the underlying PTY session.
type TerminalDetachRequest struct {
	ID string `json:"id"`
}

// TerminalInputRequest sends user keystrokes to a session.
type TerminalInputRequest struct {
	ID   string `json:"id"`
	Data string `json:"data"` // base64 of raw bytes
}

// TerminalResizeRequest tells the PTY about a new client viewport.
type TerminalResizeRequest struct {
	ID   string `json:"id"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// TerminalCloseRequest terminates a session. Idempotent.
type TerminalCloseRequest struct {
	ID string `json:"id"`
}

// TerminalAck is a stable shape for all `ok`-style replies.
type TerminalAck struct {
	OK bool `json:"ok"`
}

// --- Events (daemon → client) ---

// TerminalOpenedEvent fires once per newly opened session.
type TerminalOpenedEvent struct {
	Session TerminalSessionInfo `json:"session"`
}

// TerminalClosedEvent fires once per session that has terminated.
type TerminalClosedEvent struct {
	ID       string `json:"id"`
	ExitCode int    `json:"exit_code"`
	Err      string `json:"err,omitempty"`
}

// TerminalDataEvent carries a chunk of PTY output. Bytes are base64-encoded
// because the wire is JSON.
type TerminalDataEvent struct {
	ID   string `json:"id"`
	Data string `json:"data"` // base64 of raw bytes
}

// TerminalExitEvent is delivered when a session's child process exits but
// the session has not yet been closed (the daemon will follow up with a
// TerminalClosedEvent shortly after).
type TerminalExitEvent struct {
	ID       string `json:"id"`
	ExitCode int    `json:"exit_code"`
	Err      string `json:"err,omitempty"`
}
