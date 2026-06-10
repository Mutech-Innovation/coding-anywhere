package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/apps/relay/internal/store"
	"github.com/Mutech-Innovation/coding-anywhere/internal/auth"
)

// shareAskRequest is the body of POST /v1/share/ask. All limits are fixed at
// issuance time. A guest token cannot be refreshed; it simply expires.
type shareAskRequest struct {
	// TTLSeconds is the lifetime of the link. 0 → server default; clamped to
	// the configured GuestMaxTTL.
	TTLSeconds int64 `json:"ttl_seconds,omitempty"`
	// PromptLimit caps total prompts the guest may send. 0 → unlimited.
	PromptLimit int `json:"prompt_limit,omitempty"`
	// MaxSessions caps how many chat sessions the guest may create. <=0 → 1.
	MaxSessions int `json:"max_sessions,omitempty"`
	// Cwd is the working directory the guest's chat session runs in. Guests
	// have no directory picker, so the owner picks it here; the relay injects
	// it into the guest's create_session. Empty leaves cwd to the daemon
	// (which generally rejects chat sessions without an explicit cwd).
	Cwd string `json:"cwd,omitempty"`
	// Model, when set, locks the guest session to this model. The relay
	// injects it into create_session and the daemon applies it via
	// set_config_option. Empty uses the agent's default model.
	Model string `json:"model,omitempty"`
	// AgentID, when set, locks the guest session to this chat agent
	// ("devin", "cursor-cli"). The relay injects it into create_session so
	// the guest cannot pick its own agent. Empty uses the daemon default.
	AgentID string `json:"agent_id,omitempty"`
}

type shareAskResponse struct {
	ShareID     string `json:"share_id"`
	Token       string `json:"token"`
	ExpiresAt   int64  `json:"expires_at"` // unix milliseconds
	ExpiresIn   int64  `json:"expires_in"` // seconds
	PromptLimit int    `json:"prompt_limit"`
	MaxSessions int    `json:"max_sessions"`
	// URL is populated only when CA_GUEST_WEB_BASE_URL is configured. The web
	// client otherwise builds the link from its own origin.
	URL string `json:"url,omitempty"`
}

// handleShareAsk mints a short-lived, read-only "ask"-mode guest link for the
// calling account. The caller must present a normal (client/daemon) Bearer
// token. The returned token authenticates a guest WebSocket connection that
// the relay restricts to a safe chat subset.
func (s *Server) handleShareAsk(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()
	claims, err := s.authenticateBearer(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	if claims.Kind == auth.DeviceGuest {
		// Guests cannot mint further guest links.
		writeError(w, http.StatusForbidden, "forbidden", "guests cannot create shares")
		return
	}

	var req shareAskRequest
	if err := decodeJSON(r.Body, &req, 1024); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	ttl := s.cfg.GuestDefaultTTL
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	if s.cfg.GuestMaxTTL > 0 && ttl > s.cfg.GuestMaxTTL {
		ttl = s.cfg.GuestMaxTTL
	}
	if ttl <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "ttl must be positive")
		return
	}
	if req.PromptLimit < 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "prompt_limit must be >= 0")
		return
	}
	maxSessions := req.MaxSessions
	if maxSessions <= 0 {
		maxSessions = 1
	}

	now := time.Now().UTC()
	expiresAt := now.Add(ttl)
	shareID := newULID()

	if err := s.store.PutGuestShare(r.Context(), store.GuestShare{
		ID:          shareID,
		AccountID:   claims.AccountID,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		PromptLimit: req.PromptLimit,
		MaxSessions: maxSessions,
		Cwd:         req.Cwd,
		Model:       req.Model,
		AgentID:     req.AgentID,
	}); err != nil {
		s.logger.Error("PutGuestShare", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "store")
		return
	}

	token, err := s.signer.Issue(claims.AccountID, shareID, auth.DeviceGuest, ttl)
	if err != nil {
		s.logger.Error("Issue guest token", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "issue")
		return
	}

	resp := shareAskResponse{
		ShareID:     shareID,
		Token:       token,
		ExpiresAt:   expiresAt.UnixMilli(),
		ExpiresIn:   int64(ttl.Seconds()),
		PromptLimit: req.PromptLimit,
		MaxSessions: maxSessions,
	}
	if s.cfg.GuestWebBaseURL != "" {
		resp.URL = s.cfg.GuestWebBaseURL + "/guest?token=" + token
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleShareRevoke revokes a guest share by id and drops any live guest
// connection using it. The caller must own the share's account.
func (s *Server) handleShareRevoke(w http.ResponseWriter, r *http.Request) {
	claims, err := s.authenticateBearer(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	shareID := r.PathValue("id")
	if shareID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "share id required")
		return
	}
	share, err := s.store.GetGuestShare(r.Context(), shareID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "unknown share")
		return
	}
	if err != nil {
		s.logger.Error("GetGuestShare revoke", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "store")
		return
	}
	if share.AccountID != claims.AccountID {
		// Don't leak existence of other accounts' shares.
		writeError(w, http.StatusNotFound, "not_found", "unknown share")
		return
	}
	if err := s.store.RevokeGuestShare(r.Context(), shareID); err != nil {
		s.logger.Error("RevokeGuestShare", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "store")
		return
	}
	s.hub.CloseGuest(claims.AccountID, shareID)
	w.WriteHeader(http.StatusNoContent)
}
