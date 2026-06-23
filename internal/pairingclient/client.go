// Package pairingclient is the HTTP client for the relay's pairing endpoints.
//
// It is shared between the daemon (which calls it during `coding-anywhere
// init`) and any future client tooling. Pairing flows always involve two
// parties — an initiator and a claimer — and either side can use this client
// to drive its half of the handshake.
package pairingclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CredentialBundle mirrors apps/relay/internal/server.CredentialBundle. We
// duplicate the type here so packages can depend on pairingclient without
// pulling in relay internals.
type CredentialBundle struct {
	AccountID             string `json:"account_id"`
	DeviceID              string `json:"device_id"`
	DeviceKind            string `json:"device_kind"`
	DeviceLabel           string `json:"device_label"`
	AccessToken           string `json:"access_token"`
	AccessTokenExpiresIn  int64  `json:"access_token_expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	RelayProtocolVersion  int    `json:"relay_protocol_version"`
	// AccountKey is only populated on first-time pairing or after an
	// explicit rotate.
	AccountKey string `json:"account_key,omitempty"`
}

// InitResponse is what /v1/pair/init returns.
type InitResponse struct {
	PairingToken string `json:"pairing_token"`
	Code         string `json:"code"`
	ExpiresIn    int64  `json:"expires_in"`
}

// StatusResponse is what /v1/pair/status returns. `Status` is one of
// "pending", "claimed", "expired".
type StatusResponse struct {
	Status      string            `json:"status"`
	Credentials *CredentialBundle `json:"credentials,omitempty"`
}

// ClaimResponse is what /v1/pair/claim returns to the claimer.
type ClaimResponse struct {
	Credentials CredentialBundle `json:"credentials"`
}

// ErrorResponse is the relay's standard JSON error body.
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (e ErrorResponse) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Code
}

// Client is a thin HTTP wrapper around the relay's pairing endpoints.
type Client struct {
	relayHTTP string
	http      *http.Client
}

// New builds a pairing client. `relayURL` may be either an http(s):// or
// ws(s):// form; the client converts to HTTP for these REST calls.
func New(relayURL string) *Client {
	return &Client{
		relayHTTP: toHTTPURL(relayURL),
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetHTTPClient overrides the underlying *http.Client (e.g. for tests).
func (c *Client) SetHTTPClient(h *http.Client) {
	c.http = h
}

// Init starts a pairing flow as the initiator (typically a daemon).
func (c *Client) Init(ctx context.Context, deviceKind, deviceLabel string, bearer string) (*InitResponse, error) {
	body := map[string]string{"device_kind": deviceKind, "device_label": deviceLabel}
	var out InitResponse
	if err := c.post(ctx, "/v1/pair/init", body, bearer, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Status polls the pairing status. The returned status is "pending",
// "claimed" (with credentials populated), or "expired".
func (c *Client) Status(ctx context.Context, pairingToken string) (*StatusResponse, error) {
	u := c.relayHTTP + "/v1/pair/status?pairing_token=" + pairingToken
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()

	switch res.StatusCode {
	case 200:
		var out StatusResponse
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			return nil, err
		}
		return &out, nil
	case 410:
		var out StatusResponse
		_ = json.NewDecoder(res.Body).Decode(&out)
		return &StatusResponse{Status: "expired"}, nil
	default:
		return nil, decodeError(res)
	}
}

// WaitClaimed polls Status until the pairing is claimed, returns expired,
// or ctx is cancelled. Polls every `interval`. Defaults to 2s if 0.
func (c *Client) WaitClaimed(ctx context.Context, pairingToken string, interval time.Duration) (*CredentialBundle, error) {
	if interval == 0 {
		interval = 2 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		s, err := c.Status(ctx, pairingToken)
		if err != nil {
			return nil, err
		}
		switch s.Status {
		case "claimed":
			if s.Credentials == nil {
				return nil, errors.New("status=claimed but no credentials")
			}
			return s.Credentials, nil
		case "expired":
			return nil, ErrPairingExpired
		case "pending":
			// continue
		default:
			return nil, fmt.Errorf("unexpected status %q", s.Status)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}

// Claim claims a pairing code as the claimer (typically a web/mobile client).
func (c *Client) Claim(ctx context.Context, code, deviceKind, deviceLabel string, bearer string) (*ClaimResponse, error) {
	body := map[string]string{"code": code, "device_kind": deviceKind, "device_label": deviceLabel}
	var out ClaimResponse
	if err := c.post(ctx, "/v1/pair/claim", body, bearer, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Refresh exchanges a refresh token for a fresh access token.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (string, int64, error) {
	body := map[string]string{"refresh_token": refreshToken}
	var out struct {
		AccessToken          string `json:"access_token"`
		AccessTokenExpiresIn int64  `json:"access_token_expires_in"`
	}
	if err := c.post(ctx, "/v1/auth/refresh", body, "", &out); err != nil {
		return "", 0, err
	}
	return out.AccessToken, out.AccessTokenExpiresIn, nil
}

// LoginRequest is the body of POST /v1/auth/login.
type LoginRequest struct {
	AccountKey  string `json:"account_key"`
	DeviceLabel string `json:"device_label"`
	DeviceKind  string `json:"device_kind,omitempty"`
}

// Login trades an account key for a freshly issued credential bundle.
// Currently only client-kind devices may log in via this flow.
func (c *Client) Login(ctx context.Context, req LoginRequest) (*CredentialBundle, error) {
	var out struct {
		Credentials CredentialBundle `json:"credentials"`
	}
	if err := c.post(ctx, "/v1/auth/login", req, "", &out); err != nil {
		return nil, err
	}
	return &out.Credentials, nil
}

// RegisterResponse is what POST /v1/auth/register returns.
type RegisterResponse struct {
	UserID          string `json:"user_id"`
	Username        string `json:"username"`
	LinkedAccountID string `json:"linked_account_id"`
}

// Register creates (or, for an existing username, password-verifies) a user
// and links the authenticated account to it. The Bearer proves the caller
// already owns a paired machine — that is what authorises minting the login.
func (c *Client) Register(ctx context.Context, bearer, username, password string) (*RegisterResponse, error) {
	body := map[string]string{"username": username, "password": password}
	var out RegisterResponse
	if err := c.post(ctx, "/v1/auth/register", body, bearer, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RotateAccountKey rotates the account key for the authenticated account
// and returns the new plaintext value. The new key is only visible in this
// response — callers must persist it.
func (c *Client) RotateAccountKey(ctx context.Context, bearer string) (string, error) {
	var out struct {
		AccountKey string `json:"account_key"`
	}
	if err := c.post(ctx, "/v1/auth/key/rotate", struct{}{}, bearer, &out); err != nil {
		return "", err
	}
	return out.AccountKey, nil
}

// RevokeAccountKey clears the account key AND revokes every refresh token
// on the account, forcing all sessions to drop. Use only when the key (or
// any device's credentials) is suspected compromised.
func (c *Client) RevokeAccountKey(ctx context.Context, bearer string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.relayHTTP+"/v1/auth/key", nil)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 400 {
		return decodeError(res)
	}
	return nil
}

func (c *Client) post(ctx context.Context, path string, body any, bearer string, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.relayHTTP+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 400 {
		return decodeError(res)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func decodeError(res *http.Response) error {
	body, _ := io.ReadAll(res.Body)
	var e ErrorResponse
	if err := json.Unmarshal(body, &e); err != nil {
		return fmt.Errorf("relay error %d: %s", res.StatusCode, string(body))
	}
	return fmt.Errorf("relay error %d: %w", res.StatusCode, e)
}

// ErrPairingExpired is returned by WaitClaimed when the pairing's TTL elapses
// before a claim arrives.
var ErrPairingExpired = errors.New("pairing expired")

// toHTTPURL maps ws(s):// → http(s):// so the same configured relay base
// works for both REST and WebSocket flavours.
func toHTTPURL(u string) string {
	switch {
	case strings.HasPrefix(u, "ws://"):
		return "http://" + strings.TrimPrefix(u, "ws://")
	case strings.HasPrefix(u, "wss://"):
		return "https://" + strings.TrimPrefix(u, "wss://")
	}
	return u
}
