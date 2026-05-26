package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/apps/relay/internal/store"
	"github.com/Mutech-Innovation/coding-anywhere/internal/auth"
	"github.com/oklog/ulid/v2"
)

// CredentialBundle is the response body shape returned to a freshly paired
// device. The access token is short-lived; the refresh token is opaque, only
// shown once, stored hashed, and exchanged for new access tokens via
// POST /v1/auth/refresh.
//
// AccountKey is populated only on first-time pairing (when the account is
// newly created) and after an explicit rotate. It is omitted on add-device
// pairs, /v1/auth/refresh, and /v1/auth/login responses.
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
	AccountKey            string `json:"account_key,omitempty"`
}

const protocolVersion = 1

// issueCredentials builds a fresh CredentialBundle for a device, signs the
// access token, and persists the hashed refresh token.
func (s *Server) issueCredentials(ctx Ctx, accountID string, dev store.Device) (CredentialBundle, error) {
	now := time.Now().UTC()

	accessTTL := s.cfg.ClientTokenTTL
	refreshTTL := 90 * 24 * time.Hour
	if dev.Kind == store.DeviceDaemon {
		accessTTL = s.cfg.DaemonTokenTTL
	}

	access, err := s.signer.Issue(accountID, dev.ID, auth.DeviceKind(dev.Kind), accessTTL)
	if err != nil {
		return CredentialBundle{}, fmt.Errorf("issue access: %w", err)
	}

	refreshPlain, err := randomBase64URL(32)
	if err != nil {
		return CredentialBundle{}, fmt.Errorf("rand: %w", err)
	}
	refreshHash := sha256Hex(refreshPlain)
	if err := s.store.PutRefreshToken(ctx, store.RefreshToken{
		TokenHash: refreshHash,
		AccountID: accountID,
		DeviceID:  dev.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(refreshTTL),
	}); err != nil {
		return CredentialBundle{}, fmt.Errorf("persist refresh: %w", err)
	}

	return CredentialBundle{
		AccountID:             accountID,
		DeviceID:              dev.ID,
		DeviceKind:            string(dev.Kind),
		DeviceLabel:           dev.Label,
		AccessToken:           access,
		AccessTokenExpiresIn:  int64(accessTTL.Seconds()),
		RefreshToken:          refreshPlain,
		RefreshTokenExpiresIn: int64(refreshTTL.Seconds()),
		RelayProtocolVersion:  protocolVersion,
	}, nil
}

// randomBase64URL returns a base64url-encoded random string with `n` random bytes.
func randomBase64URL(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// AccountKeyPrefix is the human-recognisable prefix on every account key.
// Format: `ca_key_<base64url(32 random bytes)>`.
const AccountKeyPrefix = "ca_key_"

// randomAccountKey mints a fresh account key in the canonical
// `ca_key_<43 char base64url>` shape. 32 random bytes give us ~256 bits of
// entropy, plenty for a long-lived password-equivalent secret.
func randomAccountKey() (string, error) {
	body, err := randomBase64URL(32)
	if err != nil {
		return "", err
	}
	return AccountKeyPrefix + body, nil
}

// hashAccountKey returns the SHA-256 hex digest used to look up an account
// key on the relay side. The plaintext is never persisted.
func hashAccountKey(plain string) string {
	return sha256Hex(plain)
}

// validAccountKeyShape does a cheap structural check before hitting the
// database. It only ensures the canonical prefix and a reasonable length
// for the base64url body; cryptographic verification happens by lookup.
func validAccountKeyShape(plain string) bool {
	if len(plain) < len(AccountKeyPrefix)+32 {
		return false
	}
	if plain[:len(AccountKeyPrefix)] != AccountKeyPrefix {
		return false
	}
	body := plain[len(AccountKeyPrefix):]
	for _, r := range body {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

// sha256Hex hashes the input as SHA-256 and returns the lowercase hex digest.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// random6DigitCode returns a uniformly distributed 6-digit code (100000..999999).
func random6DigitCode() (string, error) {
	max := big.NewInt(900000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()+100000), nil
}

// newULID returns a fresh ULID using crypto/rand for entropy.
func newULID() string {
	return ulid.Make().String()
}
