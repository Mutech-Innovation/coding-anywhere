package server

import (
	"errors"
	"net/http"
	"strings"
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

// loginRequest is the body of POST /v1/auth/login. The client presents an
// account key plus a label for the new device. Login is currently only
// supported for client-kind devices (daemons obtain credentials via the
// pairing init flow).
type loginRequest struct {
	AccountKey  string `json:"account_key"`
	DeviceLabel string `json:"device_label"`
	DeviceKind  string `json:"device_kind,omitempty"`
}

// handleAuthLogin trades a plaintext account key for a freshly issued
// credential bundle. A new device row is created on every successful login
// so each browser appears separately in the device list and can be revoked
// independently.
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()
	var req loginRequest
	if err := decodeJSON(r.Body, &req, 4096); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.AccountKey == "" || !validAccountKeyShape(req.AccountKey) {
		writeError(w, http.StatusBadRequest, "bad_request", "account_key required")
		return
	}
	label := strings.TrimSpace(req.DeviceLabel)
	if label == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "device_label required")
		return
	}
	kind := store.DeviceClient
	if req.DeviceKind != "" {
		k := store.DeviceKind(req.DeviceKind)
		if k != store.DeviceClient {
			writeError(w, http.StatusBadRequest, "bad_request",
				"login currently only supports device_kind=client; daemons must use pair init")
			return
		}
		kind = k
	}

	acc, err := s.store.GetAccountByKeyHash(r.Context(), hashAccountKey(req.AccountKey))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "invalid_account_key", "unknown or revoked account key")
		return
	}
	if err != nil {
		s.logger.Error("GetAccountByKeyHash", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "store")
		return
	}

	dev := store.Device{
		ID:        newULID(),
		AccountID: acc.ID,
		Kind:      kind,
		Label:     label,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateDevice(r.Context(), dev); err != nil {
		s.logger.Error("CreateDevice login", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "create_device")
		return
	}

	creds, err := s.issueCredentials(r.Context(), acc.ID, dev)
	if err != nil {
		s.logger.Error("issueCredentials login", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "issue")
		return
	}
	// Never echo the account key back in the login response — the caller
	// already has it. If they want a new one, they call /v1/auth/key/rotate.
	creds.AccountKey = ""
	writeJSON(w, http.StatusOK, struct {
		Credentials CredentialBundle `json:"credentials"`
	}{Credentials: creds})
}

// rotateKeyResponse is returned by POST /v1/auth/key/rotate. The plaintext
// AccountKey is only ever shown here — re-rotate (or revoke) if you lose it.
type rotateKeyResponse struct {
	AccountKey string `json:"account_key"`
}

// handleAuthKeyRotate generates a new account key (invalidating the old one)
// for the calling account. The caller's own access/refresh tokens are
// untouched — they can keep working on this session. Other devices already
// holding credentials continue to work via their refresh tokens; only the
// "log in with the old key" flow is broken.
func (s *Server) handleAuthKeyRotate(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()
	claims, err := s.authenticateBearer(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	key, err := randomAccountKey()
	if err != nil {
		s.logger.Error("randomAccountKey rotate", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "key")
		return
	}
	if err := s.store.SetAccountKey(r.Context(), claims.AccountID, hashAccountKey(key)); err != nil {
		s.logger.Error("SetAccountKey rotate", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "key_persist")
		return
	}
	writeJSON(w, http.StatusOK, rotateKeyResponse{AccountKey: key})
}

// handleAuthKeyRevoke clears the account key entirely AND revokes every
// refresh token on the account, forcing all sessions to drop. Use this when
// you suspect the key (or any device's credentials) has been compromised.
//
// After a revoke the only way back into the account is a fresh pair flow.
func (s *Server) handleAuthKeyRevoke(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()
	claims, err := s.authenticateBearer(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	if err := s.store.SetAccountKey(r.Context(), claims.AccountID, ""); err != nil {
		s.logger.Error("SetAccountKey revoke", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "key_clear")
		return
	}
	if err := s.store.RevokeRefreshTokensForAccount(r.Context(), claims.AccountID); err != nil {
		s.logger.Error("RevokeRefreshTokensForAccount", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "revoke")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
