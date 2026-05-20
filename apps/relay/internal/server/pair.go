package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/apps/relay/internal/store"
	"github.com/Mutech-Innovation/coding-anywhere/internal/auth"
)

// Ctx is a small alias to keep handler signatures readable.
type Ctx = context.Context

// pairInitRequest is the body of POST /v1/pair/init.
type pairInitRequest struct {
	DeviceKind  string `json:"device_kind"`
	DeviceLabel string `json:"device_label"`
}

type pairInitResponse struct {
	PairingToken string `json:"pairing_token"`
	Code         string `json:"code"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (s *Server) handlePairInit(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()
	var req pairInitRequest
	if err := decodeJSON(r.Body, &req, 1024); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	kind := store.DeviceKind(req.DeviceKind)
	if kind != store.DeviceDaemon && kind != store.DeviceClient {
		writeError(w, http.StatusBadRequest, "bad_request", "device_kind must be daemon or client")
		return
	}
	if strings.TrimSpace(req.DeviceLabel) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "device_label required")
		return
	}

	// Optional Bearer for "add device to my account" flow.
	var initiatorAccountID *string
	if claims, err := s.authenticateBearer(r); err == nil {
		acc := claims.AccountID
		initiatorAccountID = &acc
	} else if !errors.Is(err, errNoBearer) {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	now := time.Now().UTC()
	pairing := store.Pairing{
		PairingToken:         newULID(),
		InitiatorAccountID:   initiatorAccountID,
		InitiatorDeviceKind:  kind,
		InitiatorDeviceLabel: strings.TrimSpace(req.DeviceLabel),
		CreatedAt:            now,
		ExpiresAt:            now.Add(s.cfg.PairingTTL),
	}

	// Try a few times to find a unique 6-digit code (collisions over the
	// active TTL window are unlikely but handle them).
	for i := 0; i < 5; i++ {
		code, err := random6DigitCode()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "rand")
			return
		}
		pairing.Code = code
		if err := s.store.CreatePairing(r.Context(), pairing); err != nil {
			if isUniqueConflict(err) {
				continue
			}
			s.logger.Error("CreatePairing", "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "store")
			return
		}
		writeJSON(w, http.StatusOK, pairInitResponse{
			PairingToken: pairing.PairingToken,
			Code:         pairing.Code,
			ExpiresIn:    int64(s.cfg.PairingTTL.Seconds()),
		})
		return
	}
	writeError(w, http.StatusServiceUnavailable, "code_unavailable", "could not allocate a pairing code")
}

// pairStatusResponse is one of three discriminated shapes; we send all three
// shapes via the same JSON envelope so the client can branch on `status`.
type pairStatusResponse struct {
	Status      string            `json:"status"`
	Credentials *CredentialBundle `json:"credentials,omitempty"`
}

func (s *Server) handlePairStatus(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("pairing_token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "pairing_token required")
		return
	}
	p, err := s.store.GetPairingByToken(r.Context(), token)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "unknown pairing")
		return
	}
	if err != nil {
		s.logger.Error("GetPairingByToken", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "store")
		return
	}
	now := time.Now().UTC()
	if !p.Claimed && now.After(p.ExpiresAt) {
		_ = s.store.DeletePairing(r.Context(), p.PairingToken)
		writeJSON(w, http.StatusGone, pairStatusResponse{Status: "expired"})
		return
	}
	if !p.Claimed {
		writeJSON(w, http.StatusOK, pairStatusResponse{Status: "pending"})
		return
	}

	// Claimed → return credentials and delete the row so subsequent polls
	// fail. The initiator only ever reads credentials once.
	var cred CredentialBundle
	if p.ClaimedCredentialsJSON != nil {
		if err := json.Unmarshal([]byte(*p.ClaimedCredentialsJSON), &cred); err != nil {
			s.logger.Error("decode claimed credentials", "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "decode")
			return
		}
	}
	if err := s.store.DeletePairing(r.Context(), p.PairingToken); err != nil {
		s.logger.Warn("delete pairing", "err", err)
	}
	writeJSON(w, http.StatusOK, pairStatusResponse{Status: "claimed", Credentials: &cred})
}

// pairClaimRequest is the body of POST /v1/pair/claim.
type pairClaimRequest struct {
	Code        string `json:"code"`
	DeviceKind  string `json:"device_kind"`
	DeviceLabel string `json:"device_label"`
}

type pairClaimResponse struct {
	Credentials CredentialBundle `json:"credentials"`
}

func (s *Server) handlePairClaim(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()
	var req pairClaimRequest
	if err := decodeJSON(r.Body, &req, 1024); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	claimerKind := store.DeviceKind(req.DeviceKind)
	if claimerKind != store.DeviceDaemon && claimerKind != store.DeviceClient {
		writeError(w, http.StatusBadRequest, "bad_request", "device_kind must be daemon or client")
		return
	}
	if strings.TrimSpace(req.DeviceLabel) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "device_label required")
		return
	}
	if len(req.Code) != 6 {
		writeError(w, http.StatusBadRequest, "bad_request", "code must be 6 digits")
		return
	}

	var claimerAccountID *string
	if claims, err := s.authenticateBearer(r); err == nil {
		acc := claims.AccountID
		claimerAccountID = &acc
	} else if !errors.Is(err, errNoBearer) {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	p, err := s.store.GetPairingByCode(r.Context(), req.Code)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "unknown code")
		return
	}
	if err != nil {
		s.logger.Error("GetPairingByCode", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "store")
		return
	}
	if p.Claimed {
		writeError(w, http.StatusConflict, "already_claimed", "code already used")
		return
	}
	if time.Now().UTC().After(p.ExpiresAt) {
		writeError(w, http.StatusGone, "expired", "code expired")
		return
	}

	// Resolve the account: prefer the claimer's existing account, then the
	// initiator's, otherwise create a fresh one.
	var accountID string
	switch {
	case claimerAccountID != nil && p.InitiatorAccountID != nil && *claimerAccountID != *p.InitiatorAccountID:
		writeError(w, http.StatusConflict, "account_mismatch", "initiator and claimer belong to different accounts")
		return
	case claimerAccountID != nil:
		accountID = *claimerAccountID
	case p.InitiatorAccountID != nil:
		accountID = *p.InitiatorAccountID
	default:
		accountID = newULID()
		if _, err := s.store.CreateAccount(r.Context(), accountID); err != nil {
			s.logger.Error("CreateAccount", "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "create_account")
			return
		}
	}

	// Materialise both devices.
	initiator := store.Device{
		ID:        newULID(),
		AccountID: accountID,
		Kind:      p.InitiatorDeviceKind,
		Label:     p.InitiatorDeviceLabel,
		CreatedAt: time.Now().UTC(),
	}
	if p.InitiatorAccountID == nil {
		if err := s.store.CreateDevice(r.Context(), initiator); err != nil {
			s.logger.Error("CreateDevice initiator", "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "create_device")
			return
		}
	} else {
		// The initiator already has an account. Materialise a *new* device on
		// it for this pairing.
		if err := s.store.CreateDevice(r.Context(), initiator); err != nil {
			s.logger.Error("CreateDevice initiator", "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "create_device")
			return
		}
	}
	claimer := store.Device{
		ID:        newULID(),
		AccountID: accountID,
		Kind:      claimerKind,
		Label:     strings.TrimSpace(req.DeviceLabel),
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateDevice(r.Context(), claimer); err != nil {
		s.logger.Error("CreateDevice claimer", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "create_device")
		return
	}

	// Issue credentials for both. Initiator's are stashed on the pairing row
	// for later status-poll retrieval; claimer's go in the response.
	initiatorCreds, err := s.issueCredentials(r.Context(), accountID, initiator)
	if err != nil {
		s.logger.Error("issueCredentials initiator", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "issue")
		return
	}
	claimerCreds, err := s.issueCredentials(r.Context(), accountID, claimer)
	if err != nil {
		s.logger.Error("issueCredentials claimer", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "issue")
		return
	}
	credJSON, err := json.Marshal(initiatorCreds)
	if err != nil {
		s.logger.Error("encode initiator credentials", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "encode")
		return
	}
	if err := s.store.MarkPairingClaimed(r.Context(), p.PairingToken,
		initiator.ID, accountID, claimer.ID, string(credJSON)); err != nil {
		s.logger.Error("MarkPairingClaimed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "claim")
		return
	}
	writeJSON(w, http.StatusOK, pairClaimResponse{Credentials: claimerCreds})
}

// errNoBearer signals there is no Bearer header — distinct from a present-
// but-invalid one.
var errNoBearer = errors.New("no bearer")

func (s *Server) authenticateBearer(r *http.Request) (*auth.Claims, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return nil, errNoBearer
	}
	if !strings.HasPrefix(h, "Bearer ") {
		return nil, errors.New("expected Bearer auth")
	}
	return s.signer.Verify(strings.TrimPrefix(h, "Bearer "))
}

func decodeJSON(r io.Reader, v any, max int64) error {
	limited := io.LimitReader(r, max)
	dec := json.NewDecoder(limited)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

type errBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errBody{Code: code, Message: msg})
}

func isUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	// modernc.org/sqlite returns errors with "constraint failed: UNIQUE" in
	// the message. Match conservatively.
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "unique")
}
