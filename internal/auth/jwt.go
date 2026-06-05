// Package auth provides JWT issuance and verification used by the relay to
// authenticate device WebSocket connections, plus device-pairing primitives.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// DeviceKind distinguishes a daemon (running on the dev machine) from a
// client (web/mobile) on the same account.
type DeviceKind string

const (
	DeviceDaemon DeviceKind = "daemon"
	DeviceClient DeviceKind = "client"
	// DeviceGuest is a short-lived, read-only "ask"-mode share. Guests have
	// no refresh token; their access token expires and cannot be renewed.
	// The relay restricts guest connections to a safe chat subset (see the
	// WS read loop) and the daemon forces ask mode + auto-denies permission
	// requests on guest-originated sessions.
	DeviceGuest DeviceKind = "guest"
)

// Claims is the JWT payload we sign. Account groups all devices that share a
// session graph; DeviceID identifies a specific connection within an account.
type Claims struct {
	AccountID string     `json:"acc"`
	DeviceID  string     `json:"sub"`
	Kind      DeviceKind `json:"kind"`
	jwt.RegisteredClaims
}

// Signer issues and verifies JWTs with a shared HMAC secret.
//
// Tokens are short-lived (default: 1h for clients, 30 days for daemons) and
// are refreshed by the long-lived refresh tokens issued at pair-time.
type Signer struct {
	secret []byte
	issuer string
	now    func() time.Time
}

// NewSigner returns a Signer that signs HS256 tokens. `secret` must be at
// least 32 bytes; pass `nil` for `now` to use real time.
func NewSigner(secret []byte, issuer string, now func() time.Time) (*Signer, error) {
	if len(secret) < 32 {
		return nil, errors.New("jwt secret must be at least 32 bytes")
	}
	if now == nil {
		now = time.Now
	}
	return &Signer{secret: secret, issuer: issuer, now: now}, nil
}

// Issue creates and signs a token for the given device. The token expires
// `ttl` after the signer's current time.
func (s *Signer) Issue(accountID, deviceID string, kind DeviceKind, ttl time.Duration) (string, error) {
	now := s.now()
	claims := Claims{
		AccountID: accountID,
		DeviceID:  deviceID,
		Kind:      kind,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   deviceID,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(s.secret)
}

// Verify parses and validates a signed token. Errors are wrapped with
// `ErrTokenInvalid` so callers can match without depending on jwt internals.
func (s *Signer) Verify(token string) (*Claims, error) {
	c := &Claims{}
	parsed, err := jwt.ParseWithClaims(token, c, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	}, jwt.WithIssuer(s.issuer), jwt.WithTimeFunc(s.now))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}
	if !parsed.Valid {
		return nil, ErrTokenInvalid
	}
	if c.AccountID == "" || c.DeviceID == "" ||
		(c.Kind != DeviceDaemon && c.Kind != DeviceClient && c.Kind != DeviceGuest) {
		return nil, fmt.Errorf("%w: missing claims", ErrTokenInvalid)
	}
	return c, nil
}

// ErrTokenInvalid wraps any token failure (signature, expiry, missing claim).
var ErrTokenInvalid = errors.New("token invalid")
