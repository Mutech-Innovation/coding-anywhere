package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/apps/relay/internal/store"
	"github.com/Mutech-Innovation/coding-anywhere/internal/auth"
)

// Username/password bounds. Deliberately lenient (this is a self-host login),
// but capped so a malicious body can't feed argon2 an unbounded password.
const (
	minUsernameLen = 3
	maxUsernameLen = 32
	minPasswordLen = 8
	maxPasswordLen = 200
)

// validUsername allows letters, digits, and a few separators. Case is
// preserved on storage but uniqueness/lookup are case-insensitive (see the
// users table COLLATE NOCASE).
func validUsername(u string) bool {
	if len(u) < minUsernameLen || len(u) > maxUsernameLen {
		return false
	}
	for _, r := range u {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

// registerRequest is the body of POST /v1/auth/register.
type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type registerResponse struct {
	UserID          string `json:"user_id"`
	Username        string `json:"username"`
	LinkedAccountID string `json:"linked_account_id"`
}

// handleAuthRegister creates a user (username + password) and links the
// caller's account to it, OR — if the username already exists — verifies the
// password and links the caller's account to that existing user.
//
// It requires a valid Bearer (the caller must already own an account, i.e.
// have paired a machine). That single rule means we don't need open
// registration: your first daemon's credentials are what authorise minting
// the login, and running this on a second machine attaches that machine to
// the same user. The plaintext password is hashed (argon2id) before storage.
func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()
	claims, err := s.authenticateBearer(r)
	if errors.Is(err, errNoBearer) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "register requires the credentials of a paired device")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	var req registerRequest
	if err := decodeJSON(r.Body, &req, 4096); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if !validUsername(req.Username) {
		writeError(w, http.StatusBadRequest, "bad_request", "username must be 3-32 chars of letters, digits, . _ -")
		return
	}
	if len(req.Password) < minPasswordLen || len(req.Password) > maxPasswordLen {
		writeError(w, http.StatusBadRequest, "bad_request", "password must be 8-200 characters")
		return
	}

	existing, err := s.store.GetUserByUsername(r.Context(), req.Username)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// Fresh user: hash the password and create it.
		hash, herr := auth.HashPassword(req.Password)
		if herr != nil {
			s.logger.Error("HashPassword register", "err", herr)
			writeError(w, http.StatusInternalServerError, "internal_error", "hash")
			return
		}
		user := store.User{ID: newULID(), Username: req.Username, PasswordHash: hash, CreatedAt: time.Now().UTC()}
		if cerr := s.store.CreateUser(r.Context(), user); cerr != nil {
			if isUniqueConflict(cerr) {
				// Lost a race; treat as "taken".
				writeError(w, http.StatusConflict, "username_taken", "username already registered")
				return
			}
			s.logger.Error("CreateUser", "err", cerr)
			writeError(w, http.StatusInternalServerError, "internal_error", "create_user")
			return
		}
		existing = user
	case err != nil:
		s.logger.Error("GetUserByUsername register", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "store")
		return
	default:
		// Username exists: only the password holder may attach a machine to it.
		if verr := auth.VerifyPassword(existing.PasswordHash, req.Password); verr != nil {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "username taken or wrong password")
			return
		}
	}

	if err := s.store.LinkAccountOwner(r.Context(), existing.ID, claims.AccountID); err != nil {
		s.logger.Error("LinkAccountOwner", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "link")
		return
	}
	writeJSON(w, http.StatusOK, registerResponse{
		UserID:          existing.ID,
		Username:        existing.Username,
		LinkedAccountID: claims.AccountID,
	})
}

// userLoginRequest is the body of POST /v1/auth/user/login.
//
// AccountID is optional: when a user owns exactly one account it can be
// omitted and the relay logs straight into it. When a user owns several
// machines and AccountID is empty, the relay returns the account list with
// no credentials so the client can show a picker and re-POST with a choice.
type userLoginRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DeviceLabel string `json:"device_label"`
	AccountID   string `json:"account_id,omitempty"`
}

// userAccountSummary is one entry in the picker the client shows when a user
// owns more than one machine. Label is derived from the account's daemon
// device so the user recognises which machine they're picking.
type userAccountSummary struct {
	AccountID string `json:"account_id"`
	Label     string `json:"label"`
}

type userLoginResponse struct {
	Accounts    []userAccountSummary `json:"accounts"`
	Credentials *CredentialBundle    `json:"credentials,omitempty"`
}

// handleUserLogin trades username+password for a credential bundle, creating
// a fresh client device on the chosen account. This is the "log in from any
// browser" path that replaces pasting an account key.
func (s *Server) handleUserLogin(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()
	var req userLoginRequest
	if err := decodeJSON(r.Body, &req, 4096); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	label := strings.TrimSpace(req.DeviceLabel)
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "username and password required")
		return
	}
	if label == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "device_label required")
		return
	}

	user, err := s.store.GetUserByUsername(r.Context(), req.Username)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "wrong username or password")
		return
	}
	if err != nil {
		s.logger.Error("GetUserByUsername login", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "store")
		return
	}
	if verr := auth.VerifyPassword(user.PasswordHash, req.Password); verr != nil {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "wrong username or password")
		return
	}

	accounts, err := s.store.ListAccountsForUser(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("ListAccountsForUser", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "store")
		return
	}
	if len(accounts) == 0 {
		writeError(w, http.StatusConflict, "no_accounts", "this user has no machines linked yet")
		return
	}

	summaries := s.accountSummaries(r.Context(), accounts)

	// Pick the target account: explicit choice → single account → otherwise
	// ask the client to choose.
	target := req.AccountID
	if target != "" {
		owns, oerr := s.store.UserOwnsAccount(r.Context(), user.ID, target)
		if oerr != nil {
			s.logger.Error("UserOwnsAccount", "err", oerr)
			writeError(w, http.StatusInternalServerError, "internal_error", "store")
			return
		}
		if !owns {
			writeError(w, http.StatusForbidden, "forbidden", "you do not own that account")
			return
		}
	} else if len(accounts) == 1 {
		target = accounts[0].ID
	} else {
		// Multiple machines and no choice yet: return the list, no creds.
		writeJSON(w, http.StatusOK, userLoginResponse{Accounts: summaries})
		return
	}

	dev := store.Device{
		ID:        newULID(),
		AccountID: target,
		Kind:      store.DeviceClient,
		Label:     label,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateDevice(r.Context(), dev); err != nil {
		s.logger.Error("CreateDevice user login", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "create_device")
		return
	}
	creds, err := s.issueCredentials(r.Context(), target, dev)
	if err != nil {
		s.logger.Error("issueCredentials user login", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "issue")
		return
	}
	writeJSON(w, http.StatusOK, userLoginResponse{Accounts: summaries, Credentials: &creds})
}

// userMachinesResponse lists the machines (accounts) reachable from the
// current session — i.e. every account sharing an owner with the account the
// caller's token is for — plus which one is current. Powers the in-app
// machine switcher.
type userMachinesResponse struct {
	Accounts []userAccountSummary `json:"accounts"`
	Current  string               `json:"current"`
}

// handleUserMachines returns the sibling machines of the caller's current
// account (those owned by the same user). Auth is the caller's Bearer; no
// password needed. Works for any logged-in session (pairing or password) as
// long as the current account has an owner with more than one machine.
func (s *Server) handleUserMachines(w http.ResponseWriter, r *http.Request) {
	claims, err := s.authenticateBearer(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	accts, err := s.store.ListSiblingAccounts(r.Context(), claims.AccountID)
	if err != nil {
		s.logger.Error("ListSiblingAccounts", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "store")
		return
	}
	writeJSON(w, http.StatusOK, userMachinesResponse{
		Accounts: s.accountSummaries(r.Context(), accts),
		Current:  claims.AccountID,
	})
}

// switchRequest is the body of POST /v1/auth/user/switch.
type switchRequest struct {
	AccountID   string `json:"account_id"`
	DeviceLabel string `json:"device_label,omitempty"`
}

type switchResponse struct {
	Accounts    []userAccountSummary `json:"accounts"`
	Current     string               `json:"current"`
	Credentials CredentialBundle     `json:"credentials"`
}

// handleUserSwitch issues a fresh credential bundle for another machine the
// same user owns, without re-entering a password. The caller proves identity
// with its current Bearer; the target must share an owner with the caller's
// current account. A new client device is created on the target account.
func (s *Server) handleUserSwitch(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()
	claims, err := s.authenticateBearer(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	var req switchRequest
	if err := decodeJSON(r.Body, &req, 4096); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.AccountID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "account_id required")
		return
	}

	siblings, err := s.store.ListSiblingAccounts(r.Context(), claims.AccountID)
	if err != nil {
		s.logger.Error("ListSiblingAccounts switch", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "store")
		return
	}
	allowed := false
	for _, a := range siblings {
		if a.ID == req.AccountID {
			allowed = true
			break
		}
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "forbidden", "that machine is not on your account")
		return
	}

	label := strings.TrimSpace(req.DeviceLabel)
	if label == "" {
		if dev, derr := s.store.GetDevice(r.Context(), claims.DeviceID); derr == nil && dev.Label != "" {
			label = dev.Label
		} else {
			label = "switched device"
		}
	}
	dev := store.Device{
		ID:        newULID(),
		AccountID: req.AccountID,
		Kind:      store.DeviceClient,
		Label:     label,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateDevice(r.Context(), dev); err != nil {
		s.logger.Error("CreateDevice switch", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "create_device")
		return
	}
	creds, err := s.issueCredentials(r.Context(), req.AccountID, dev)
	if err != nil {
		s.logger.Error("issueCredentials switch", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "issue")
		return
	}
	writeJSON(w, http.StatusOK, switchResponse{
		Accounts:    s.accountSummaries(r.Context(), siblings),
		Current:     req.AccountID,
		Credentials: creds,
	})
}

// accountSummaries maps accounts to picker entries with friendly labels.
func (s *Server) accountSummaries(ctx Ctx, accts []store.Account) []userAccountSummary {
	out := make([]userAccountSummary, 0, len(accts))
	for _, a := range accts {
		out = append(out, userAccountSummary{AccountID: a.ID, Label: s.accountLabel(ctx, a.ID)})
	}
	return out
}

// accountLabel returns a human-friendly name for an account, derived from its
// daemon device's label. Falls back to the first device's label, then to a
// short account-id suffix when the account has no devices yet.
func (s *Server) accountLabel(ctx Ctx, accountID string) string {
	devices, err := s.store.ListDevices(ctx, accountID)
	if err != nil || len(devices) == 0 {
		if len(accountID) > 8 {
			return "account " + accountID[len(accountID)-6:]
		}
		return "account " + accountID
	}
	for _, d := range devices {
		if d.Kind == store.DeviceDaemon {
			return d.Label
		}
	}
	return devices[0].Label
}
