// Package acp implements a minimal client for the Agent Client Protocol
// (https://agentclientprotocol.com).
//
// ACP is a JSON-RPC 2.0 protocol spoken over a transport. For the Devin for
// Terminal integration, the transport is the stdio of a child `devin acp`
// process. This package is intentionally agent-agnostic: an `agent/devin`
// adapter sits on top to handle Devin-specific subprocess lifecycle and
// version pinning.
//
// Coverage scope (MVP):
//   - initialize / initialize.result
//   - session/new
//   - session/prompt
//   - session/cancel (notification)
//   - session/update (notification, agent → client)
//   - session/request_permission (request, agent → client)
//
// The package handles JSON-RPC framing, correlation by id, in-flight cancellation,
// and graceful shutdown. Higher-level concerns (ACP method semantics, content
// block rendering, MCP) live in callers.
package acp

import "encoding/json"

// Common ACP protocol constants.
const (
	ProtocolVersion = 1

	MethodInitialize         = "initialize"
	MethodAuthenticate       = "authenticate"
	MethodSessionNew         = "session/new"
	MethodSessionLoad        = "session/load"
	MethodSessionResume      = "session/resume"
	MethodSessionPrompt      = "session/prompt"
	MethodSessionClose       = "session/close"
	MethodSessionSetMode     = "session/set_mode"
	// MethodSessionSetConfigOption switches a session-scoped config option
	// (e.g. the active model, max-turn-requests). The agent's response
	// returns the new full configOptions snapshot in the result body —
	// note that, unlike most other ACP state changes, the agent does NOT
	// also emit a follow-up `config_option_update` notification, so
	// callers must fan the response out themselves to keep snapshots
	// consistent.
	MethodSessionSetConfigOption = "session/set_config_option"

	NotificationSessionUpdate = "session/update"
	NotificationSessionCancel = "session/cancel"

	// Client-side methods the agent calls back into.
	MethodSessionRequestPermission = "session/request_permission"
	MethodFSReadTextFile           = "fs/read_text_file"
	MethodFSWriteTextFile          = "fs/write_text_file"
)

// InitializeRequest is the body of the initial handshake.
type InitializeRequest struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
	ClientInfo         ImplementationInfo `json:"clientInfo"`
}

// ClientCapabilities advertises what the client (us) supports.
type ClientCapabilities struct {
	FS       *FSCapabilities `json:"fs,omitempty"`
	Terminal bool            `json:"terminal,omitempty"`
}

// FSCapabilities specifies which fs/* methods the client implements.
type FSCapabilities struct {
	ReadTextFile  bool `json:"readTextFile,omitempty"`
	WriteTextFile bool `json:"writeTextFile,omitempty"`
}

// ImplementationInfo identifies the client/agent name+version.
type ImplementationInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

// InitializeResponse is the agent's reply to initialize.
type InitializeResponse struct {
	ProtocolVersion   int                `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities  `json:"agentCapabilities"`
	AgentInfo         ImplementationInfo `json:"agentInfo,omitempty"`
	AuthMethods       []AuthMethod       `json:"authMethods,omitempty"`
}

// AuthMethod describes one authentication option offered by the agent. ACP
// recently widened the `authMethods` field from a list of strings to a list
// of objects with id/name/description.
type AuthMethod struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// AgentCapabilities advertises optional agent features.
type AgentCapabilities struct {
	LoadSession         bool                 `json:"loadSession,omitempty"`
	PromptCapabilities  *PromptCapabilities  `json:"promptCapabilities,omitempty"`
	MCPCapabilities     *MCPCapabilities     `json:"mcpCapabilities,omitempty"`
	SessionCapabilities *SessionCapabilities `json:"sessionCapabilities,omitempty"`
}

// PromptCapabilities lists the optional content kinds in session/prompt.
type PromptCapabilities struct {
	Image           bool `json:"image,omitempty"`
	Audio           bool `json:"audio,omitempty"`
	EmbeddedContext bool `json:"embeddedContext,omitempty"`
}

// MCPCapabilities lists optional MCP transports the agent supports.
type MCPCapabilities struct {
	HTTP bool `json:"http,omitempty"`
	SSE  bool `json:"sse,omitempty"`
}

// SessionCapabilities lists optional session-management features. Newer
// agents (Devin) include extra fields like `list` and `additionalDirectories`
// that we don't model strictly; json.RawMessage round-trips them.
type SessionCapabilities struct {
	Resume               *struct{}       `json:"resume,omitempty"`
	Close                *struct{}       `json:"close,omitempty"`
	List                 json.RawMessage `json:"list,omitempty"`
	AdditionalDirectories json.RawMessage `json:"additionalDirectories,omitempty"`
}

// SessionNewRequest creates a new session in a working directory with an
// optional list of MCP servers to attach. `mcpServers` is intentionally
// always emitted (no `,omitempty`) because the ACP spec requires the field
// to be present.
type SessionNewRequest struct {
	Cwd        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers"`
}

// MCPServer is the connection info for an MCP server (stdio variant covers MVP).
type MCPServer struct {
	Name    string   `json:"name"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	Env     []KV     `json:"env,omitempty"`
	URL     string   `json:"url,omitempty"`     // for http/sse variants
	Headers []KV     `json:"headers,omitempty"` // for http/sse variants
}

// KV is a name/value pair used in MCPServer env / headers.
type KV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// SessionNewResponse returns the new session id.
type SessionNewResponse struct {
	SessionID string            `json:"sessionId"`
	Modes     *SessionModeState `json:"modes,omitempty"`
}

// SessionMode is one mode the agent can operate in (e.g. "ask", "code").
type SessionMode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SessionModeState is the modes payload on a session/new response.
type SessionModeState struct {
	CurrentModeID  string        `json:"currentModeId"`
	AvailableModes []SessionMode `json:"availableModes"`
}

// SessionSetModeRequest switches the active mode for a session.
type SessionSetModeRequest struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

// SessionSetConfigOptionRequest changes one session-scoped config option
// (matched by `id` in the corresponding config_option_update event).
// Value is the new `currentValue`; for `type: "select"` options this is one
// of the advertised `options[].value` strings.
type SessionSetConfigOptionRequest struct {
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

// SessionPromptRequest sends a user turn into a session.
type SessionPromptRequest struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// SessionPromptResponse arrives once the agent has finished its turn.
type SessionPromptResponse struct {
	StopReason string `json:"stopReason"`
}

// SessionUpdateNotification is pushed by the agent during a turn.
type SessionUpdateNotification struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}

// SessionCancelNotification interrupts an in-flight prompt.
type SessionCancelNotification struct {
	SessionID string `json:"sessionId"`
}

// ContentBlock is a piece of message content. We only model the fields we
// actually consume; unknown discriminator values are kept as raw JSON so we
// can pass them through transparently.
type ContentBlock struct {
	Type string `json:"type"`
	// Inline text for type="text".
	Text string `json:"text,omitempty"`
	// Resource link for type="resource_link".
	URI string `json:"uri,omitempty"`
	// Catch-all so we can round-trip unknown shapes.
	Extra map[string]any `json:"-"`
}

// MarshalJSON keeps `Extra` flat alongside our typed fields.
func (c ContentBlock) MarshalJSON() ([]byte, error) {
	out := map[string]any{"type": c.Type}
	if c.Text != "" {
		out["text"] = c.Text
	}
	if c.URI != "" {
		out["uri"] = c.URI
	}
	for k, v := range c.Extra {
		out[k] = v
	}
	return json.Marshal(out)
}

// UnmarshalJSON parses both the typed fields and any extra keys.
func (c *ContentBlock) UnmarshalJSON(data []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["type"]; ok {
		_ = json.Unmarshal(v, &c.Type)
		delete(raw, "type")
	}
	if v, ok := raw["text"]; ok {
		_ = json.Unmarshal(v, &c.Text)
		delete(raw, "text")
	}
	if v, ok := raw["uri"]; ok {
		_ = json.Unmarshal(v, &c.URI)
		delete(raw, "uri")
	}
	if len(raw) > 0 {
		c.Extra = make(map[string]any, len(raw))
		for k, v := range raw {
			var any any
			_ = json.Unmarshal(v, &any)
			c.Extra[k] = any
		}
	}
	return nil
}
