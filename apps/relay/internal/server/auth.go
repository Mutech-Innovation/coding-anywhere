package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/apps/relay/internal/store"
	"github.com/Mutech-Innovation/coding-anywhere/internal/auth"
)

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type refreshResponse struct {
	AccessToken          string `json:"access_token"`
	AccessTokenExpiresIn int64  `json:"access_token_expires_in"`
}

// handleAuthRefresh exchanges a refresh token for a fresh access token.
//
// Refresh tokens are stored hashed; we look up by SHA-256 hex digest. If the
// token is missing, expired or revoked we return 401 with a stable code.
func (s *Server) handleAuthRefresh(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()
	var req refreshRequest
	if err := decodeJSON(r.Body, &req, 1024); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "refresh_token required")
		return
	}
	rt, err := s.store.GetRefreshToken(r.Context(), sha256Hex(req.RefreshToken))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "invalid_refresh_token", "unknown refresh token")
		return
	}
	if err != nil {
		s.logger.Error("GetRefreshToken", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "store")
		return
	}
	if rt.Revoked {
		writeError(w, http.StatusUnauthorized, "revoked", "refresh token revoked")
		return
	}
	if time.Now().UTC().After(rt.ExpiresAt) {
		writeError(w, http.StatusUnauthorized, "expired", "refresh token expired")
		return
	}
	dev, err := s.store.GetDevice(r.Context(), rt.DeviceID)
	if err != nil {
		s.logger.Error("GetDevice for refresh", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "store")
		return
	}
	ttl := s.cfg.ClientTokenTTL
	if dev.Kind == store.DeviceDaemon {
		ttl = s.cfg.DaemonTokenTTL
	}
	access, err := s.signer.Issue(rt.AccountID, rt.DeviceID, auth.DeviceKind(dev.Kind), ttl)
	if err != nil {
		s.logger.Error("Issue access on refresh", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "issue")
		return
	}
	_ = s.store.TouchDevice(r.Context(), dev.ID)
	writeJSON(w, http.StatusOK, refreshResponse{
		AccessToken:          access,
		AccessTokenExpiresIn: int64(ttl.Seconds()),
	})
}
