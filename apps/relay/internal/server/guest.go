package server

import (
	"context"
	"encoding/json"

	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
)

// newGuestState builds the per-connection bookkeeping for a guest, seeded with
// the share's limits, owner-chosen working directory, and locked model.
func newGuestState(promptLimit, maxSessions int, cwd, model string) *guestState {
	if maxSessions <= 0 {
		maxSessions = 1
	}
	return &guestState{
		owned:       make(map[string]bool),
		pending:     make(map[string]string),
		promptLimit: promptLimit,
		maxSessions: maxSessions,
		cwd:         cwd,
		model:       model,
	}
}

func (gs *guestState) owns(sid string) bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	return gs.owned[sid]
}

func (gs *guestState) addOwned(sid string) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.owned[sid] = true
}

func (gs *guestState) addPending(id, method string) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.pending[id] = method
}

func (gs *guestState) takePending(id string) (string, bool) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	m, ok := gs.pending[id]
	if ok {
		delete(gs.pending, id)
	}
	return m, ok
}

// tryCreateSession reserves a session slot if the per-link cap allows it.
func (gs *guestState) tryCreateSession() bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	if gs.sessionsCreated >= gs.maxSessions {
		return false
	}
	gs.sessionsCreated++
	return true
}

// releaseSessionSlot returns a reserved slot, e.g. after a create_session
// request failed on the daemon so the guest can retry.
func (gs *guestState) releaseSessionSlot() {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	if gs.sessionsCreated > 0 {
		gs.sessionsCreated--
	}
}

// tryPrompt consumes one prompt against the per-link cap (0 == unlimited).
func (gs *guestState) tryPrompt() bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	if gs.promptLimit > 0 && gs.promptsUsed >= gs.promptLimit {
		return false
	}
	gs.promptsUsed++
	return true
}

// guestMayReceive decides whether a daemon-originated envelope should be
// forwarded to the given guest, enforcing session isolation. It also learns
// session ownership from a create_session response. Called from the hub's
// fan-out path.
func guestMayReceive(g *Conn, env wire.Envelope) bool {
	gs := g.guest
	if gs == nil || env.Channel != wire.ChannelChat {
		return false
	}
	switch env.Type {
	case wire.TypeResponse:
		method, ok := gs.takePending(env.ID)
		if !ok {
			return false // not a response to this guest's request
		}
		if method == wire.ChatMethodCreateSession {
			if env.Error != nil {
				gs.releaseSessionSlot()
				return true
			}
			if sid := sessionIDFromPayload(env.Payload); sid != "" {
				gs.addOwned(sid)
			}
		}
		return true
	case wire.TypeEvent:
		sid := sessionIDFromPayload(env.Payload)
		return sid != "" && gs.owns(sid)
	default:
		return false
	}
}

// guestGate enforces the outbound (guest → daemon) policy: chat-only, a safe
// method subset, per-session ownership, and per-link rate limits. It returns
// true when the envelope may be forwarded (and stamps Origin=guest so the
// daemon forces ask mode + auto-denies permissions); false when it has been
// rejected locally.
func (s *Server) guestGate(ctx context.Context, conn *Conn, env *wire.Envelope) bool {
	gs := conn.guest
	if env.Channel != wire.ChannelChat {
		s.rejectGuest(ctx, conn, *env, "forbidden", "guests may only use chat")
		return false
	}
	if env.Type != wire.TypeRequest {
		return false // guests never originate responses/events
	}
	switch env.Method {
	case wire.ChatMethodCreateSession:
		if !gs.tryCreateSession() {
			s.rejectGuest(ctx, conn, *env, "limit_reached", "session limit reached for this link")
			return false
		}
		// Force the owner-chosen cwd and model onto the session: guests have
		// no directory/model picker and must not be able to pick/omit either.
		// Empty share values leave that field untouched (legacy shares /
		// default model).
		if gs.cwd != "" || gs.model != "" {
			payload, err := injectCreateFields(env.Payload, gs.cwd, gs.model)
			if err != nil {
				gs.releaseSessionSlot()
				s.rejectGuest(ctx, conn, *env, "bad_request", "invalid create_session payload")
				return false
			}
			env.Payload = payload
		}
		gs.addPending(env.ID, env.Method)
	case wire.ChatMethodPrompt:
		if sid := sessionIDFromPayload(env.Payload); sid == "" || !gs.owns(sid) {
			s.rejectGuest(ctx, conn, *env, "forbidden", "unknown session")
			return false
		}
		if !gs.tryPrompt() {
			s.rejectGuest(ctx, conn, *env, "limit_reached", "prompt limit reached for this link")
			return false
		}
		gs.addPending(env.ID, env.Method)
	case wire.ChatMethodCancel, wire.ChatMethodGetHistory:
		if sid := sessionIDFromPayload(env.Payload); sid == "" || !gs.owns(sid) {
			s.rejectGuest(ctx, conn, *env, "forbidden", "unknown session")
			return false
		}
		gs.addPending(env.ID, env.Method)
	default:
		s.rejectGuest(ctx, conn, *env, "forbidden", "method not allowed for guests")
		return false
	}
	env.Origin = wire.OriginGuest
	return true
}

// rejectGuest sends an error response back to the guest for a rejected request.
func (s *Server) rejectGuest(ctx context.Context, conn *Conn, env wire.Envelope, code, msg string) {
	if env.Type != wire.TypeRequest || env.ID == "" {
		return
	}
	conn.deliver(ctx, mustEnvelope(wire.MakeResponse(env.ID, env.Channel, nil, &wire.EnvelopeError{
		Code:    code,
		Message: msg,
	})))
}

// injectCreateFields overwrites the "cwd" and/or "model" fields of a
// create_session payload with the owner-chosen values, preserving any other
// fields the guest sent (agent_id, title). Empty values are left untouched. It
// returns an error only when the payload is present but not a JSON object.
func injectCreateFields(payload json.RawMessage, cwd, model string) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &fields); err != nil {
			return nil, err
		}
	}
	for key, value := range map[string]string{"cwd": cwd, "model": model} {
		if value == "" {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		fields[key] = encoded
	}
	return json.Marshal(fields)
}

// sessionIDFromPayload extracts a chat session id from an envelope payload,
// handling both the flat `session_id` shape (most events/requests) and the
// nested `session.id` shape (create_session response, session_created event).
func sessionIDFromPayload(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var p struct {
		SessionID string `json:"session_id"`
		Session   struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return ""
	}
	if p.SessionID != "" {
		return p.SessionID
	}
	return p.Session.ID
}
