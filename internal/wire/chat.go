package wire

import (
	"encoding/json"
	"time"
)

// Methods on the `chat` channel are the user-facing entry points for the
// daemon's AI coding agents (Devin first, others in v2). Events follow the
// ACP session/update model — we forward the agent's update payload mostly
// verbatim and let the client decide how to render it.
const (
	ChatMethodListSessions  = "list_sessions"
	ChatMethodCreateSession = "create_session"
	ChatMethodPrompt        = "prompt"
	ChatMethodCancel        = "cancel"
	ChatMethodCloseSession  = "close_session"
	ChatMethodAgents        = "agents"
	ChatMethodResolvePermit = "resolve_permission"
	// ChatMethodGetHistory returns the persisted state for a session
	// (messages, tool calls, plan, mode, available commands). Lets the
	// web client repopulate after a reconnect or initial page load.
	ChatMethodGetHistory = "get_history"
	// ChatMethodSetMode forwards an ACP `session/set_mode` to the agent.
	ChatMethodSetMode = "set_mode"
	// ChatMethodSetConfigOption forwards an ACP `session/set_config_option`
	// to the agent (used by the web UI's model picker to switch model,
	// max-turn-requests, etc.).
	ChatMethodSetConfigOption = "set_config_option"

	ChatEventSessionCreated     = "session_created"
	ChatEventSessionClosed      = "session_closed"
	ChatEventSessionStatus      = "session_status"
	ChatEventUpdate             = "update"
	ChatEventPermissionRequest  = "permission_request"
	ChatEventPermissionResolved = "permission_resolved"
)

// AgentDescriptor mirrors packages/agents on the TypeScript side. Used by
// chat.agents to tell the client which agents are wired up on this daemon.
type AgentDescriptor struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type ChatAgentsResponse struct {
	Agents []AgentDescriptor `json:"agents"`
}

// ChatSession is the wire-shape of a conversation. We carry both the daemon's
// internal session id and the agent's native session id (e.g. ACP sessionId).
type ChatSession struct {
	ID        string    `json:"id"`        // daemon-side ulid
	AgentID   string    `json:"agent_id"`  // "devin", "claude-code", ...
	NativeID  string    `json:"native_id"` // agent-specific id
	Cwd       string    `json:"cwd"`
	Title     string    `json:"title,omitempty"`
	Status    string    `json:"status"` // "idle" | "thinking" | "awaiting_permission" | "closed"
	StartedAt time.Time `json:"started_at"`
}

// SessionMode mirrors the ACP `SessionMode` value type. We surface modes
// from `session/new` so the client UI can show a mode selector.
type SessionMode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// --- Requests ---

type ChatListSessionsResponse struct {
	Sessions []ChatSession `json:"sessions"`
}

type ChatCreateSessionRequest struct {
	AgentID string `json:"agent_id"`
	Cwd     string `json:"cwd,omitempty"`
	Title   string `json:"title,omitempty"`
}

type ChatCreateSessionResponse struct {
	Session ChatSession `json:"session"`
	// Modes the agent advertised at session/new. Empty when the agent
	// doesn't expose modes.
	AvailableModes []SessionMode `json:"available_modes,omitempty"`
	CurrentModeID  string        `json:"current_mode_id,omitempty"`
	// PendingUpdates carries session/update notifications that the agent
	// pushed while session/new was still in flight (config_option_update,
	// available_commands_update, …). The client replays these through its
	// normal ingest pipeline immediately after registering the session
	// locally so the chat header has model/commands/etc. populated from
	// frame one. Field is the raw ACP `update` body, same shape as
	// ChatUpdateEvent.Update.
	PendingUpdates []json.RawMessage `json:"pending_updates,omitempty"`
}

type ChatPromptRequest struct {
	SessionID   string       `json:"session_id"`
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

type Attachment struct {
	Type     string `json:"type"`     // "image" | "file"
	Name     string `json:"name"`
	Data     string `json:"data,omitempty"`     // base64
	URI      string `json:"uri,omitempty"`      // file://...
	MimeType string `json:"mime_type,omitempty"`
}

type ChatPromptResponse struct {
	StopReason string `json:"stop_reason"`
}

type ChatCancelRequest struct {
	SessionID string `json:"session_id"`
}

type ChatCloseSessionRequest struct {
	SessionID string `json:"session_id"`
}

type ChatResolvePermissionRequest struct {
	RequestID string          `json:"request_id"`
	Decision  json.RawMessage `json:"decision"`
}

// ChatSetModeRequest forwards `session/set_mode` to the agent.
type ChatSetModeRequest struct {
	SessionID string `json:"session_id"`
	ModeID    string `json:"mode_id"`
}

// ChatSetConfigOptionRequest forwards `session/set_config_option` to the
// agent. ConfigID is the option id (e.g. "model"), Value is the new
// `currentValue`.
type ChatSetConfigOptionRequest struct {
	SessionID string `json:"session_id"`
	ConfigID  string `json:"config_id"`
	Value     string `json:"value"`
}

// ChatGetHistoryRequest fetches the daemon's persisted snapshot of one
// session — every session/update we've seen, replayed in order. The web
// UI calls this on attach so a refresh / new browser doesn't lose state.
type ChatGetHistoryRequest struct {
	SessionID string `json:"session_id"`
}

// ChatGetHistoryResponse carries the persisted snapshot. Updates are the
// raw `update` bodies we forwarded as ChatEventUpdate, in order.
type ChatGetHistoryResponse struct {
	Session        ChatSession       `json:"session"`
	Updates        []json.RawMessage `json:"updates"`
	AvailableModes []SessionMode     `json:"available_modes,omitempty"`
	CurrentModeID  string            `json:"current_mode_id,omitempty"`
	UserPrompts    []ChatPromptEntry `json:"user_prompts,omitempty"`
}

// ChatPromptEntry is a persisted user turn. We replay these alongside
// agent updates so the chat transcript matches what the user originally
// saw.
type ChatPromptEntry struct {
	Text        string       `json:"text"`
	CreatedAt   time.Time    `json:"created_at"`
	Attachments []Attachment `json:"attachments,omitempty"`
	// SequenceID gives a strict relative ordering vs. the updates list.
	// Updates have a parallel sequence id; the client interleaves both
	// streams by sequence to reconstruct the timeline.
	SequenceID uint64 `json:"sequence_id"`
}

// --- Events ---

type ChatSessionCreatedEvent struct {
	Session ChatSession `json:"session"`
}

type ChatSessionClosedEvent struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason,omitempty"`
}

type ChatSessionStatusEvent struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

// ChatUpdateEvent wraps an ACP session/update notification — we pass the
// agent's `update` body straight through. The client decodes it.
type ChatUpdateEvent struct {
	SessionID string          `json:"session_id"`
	Update    json.RawMessage `json:"update"`
	// SequenceID is a monotonically increasing counter per session that
	// orders this update against any user prompts that arrive before/after
	// it. Used by the client when rebuilding history.
	SequenceID uint64 `json:"sequence_id,omitempty"`
}

// ChatPermissionRequestEvent surfaces an ACP session/request_permission to
// the client. The client must reply via chat.resolve_permission with the
// same request_id.
type ChatPermissionRequestEvent struct {
	SessionID string          `json:"session_id"`
	RequestID string          `json:"request_id"`
	Params    json.RawMessage `json:"params"`
}

// ChatPermissionResolvedEvent is broadcast after a decision is made — useful
// for other devices on the same account to clear their local "awaiting"
// state.
type ChatPermissionResolvedEvent struct {
	SessionID string `json:"session_id"`
	RequestID string `json:"request_id"`
}
