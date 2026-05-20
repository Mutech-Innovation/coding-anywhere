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
	ChatMethodListSessions   = "list_sessions"
	ChatMethodCreateSession  = "create_session"
	ChatMethodPrompt         = "prompt"
	ChatMethodCancel         = "cancel"
	ChatMethodCloseSession   = "close_session"
	ChatMethodAgents         = "agents"
	ChatMethodResolvePermit  = "resolve_permission"

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
	ID        string    `json:"id"`         // daemon-side ulid
	AgentID   string    `json:"agent_id"`   // "devin", "claude-code", ...
	NativeID  string    `json:"native_id"`  // agent-specific id
	Cwd       string    `json:"cwd"`
	Title     string    `json:"title,omitempty"`
	Status    string    `json:"status"`     // "idle" | "thinking" | "awaiting_permission" | "closed"
	StartedAt time.Time `json:"started_at"`
}

// --- Requests ---

type ChatListSessionsResponse struct {
	Sessions []ChatSession `json:"sessions"`
}

type ChatCreateSessionRequest struct {
	AgentID string `json:"agent_id"`
	Cwd     string `json:"cwd"`
	Title   string `json:"title,omitempty"`
}

type ChatCreateSessionResponse struct {
	Session ChatSession `json:"session"`
}

type ChatPromptRequest struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
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
