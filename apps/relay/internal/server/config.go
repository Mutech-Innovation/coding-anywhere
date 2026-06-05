// Package server hosts the HTTP/WebSocket entry point of the relay.
package server

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds the runtime knobs of the relay. All fields can be overridden
// via env vars (CA_*).
type Config struct {
	// ListenAddr is the address the HTTP server binds to (e.g. ":8443").
	ListenAddr string
	// JWTSecret is the HMAC secret for issuing/verifying tokens (>=32 bytes).
	JWTSecret []byte
	// JWTIssuer is set as the JWT iss claim.
	JWTIssuer string
	// DataDir is the directory where the relay persists state (sqlite).
	DataDir string
	// PairingTTL bounds how long an unclaimed pairing code lives.
	PairingTTL time.Duration
	// ClientTokenTTL is the access-token lifetime for client (web/mobile)
	// devices; daemons get longer-lived tokens.
	ClientTokenTTL time.Duration
	DaemonTokenTTL time.Duration
	// GuestMaxTTL is the upper bound on a guest ("ask"-share) token's
	// lifetime. The caller chooses the actual TTL at issuance but cannot
	// exceed this.
	GuestMaxTTL time.Duration
	// GuestDefaultTTL is used when a share request omits a TTL.
	GuestDefaultTTL time.Duration
	// GuestWebBaseURL, when set, is prefixed to the generated guest link
	// (e.g. "https://app.example.com" → ".../guest?token=..."). When empty
	// the relay returns just the token and the web client builds the URL
	// from its own origin.
	GuestWebBaseURL string
	// ReadHeaderTimeout protects against slow-loris on plaintext front-ends
	// when the relay isn't behind Caddy.
	ReadHeaderTimeout time.Duration
}

// Defaults returns a Config seeded with sensible defaults.
func Defaults() Config {
	return Config{
		ListenAddr:        ":8443",
		JWTIssuer:         "coding-anywhere/relay",
		DataDir:           "./relay-data",
		PairingTTL:        5 * time.Minute,
		ClientTokenTTL:    1 * time.Hour,
		DaemonTokenTTL:    30 * 24 * time.Hour,
		GuestMaxTTL:       24 * time.Hour,
		GuestDefaultTTL:   30 * time.Minute,
		ReadHeaderTimeout: 15 * time.Second,
	}
}

// FromEnv reads CA_* environment variables on top of Defaults().
func FromEnv() (Config, error) {
	cfg := Defaults()
	if v := os.Getenv("CA_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("CA_RELAY_JWT_SECRET"); v != "" {
		cfg.JWTSecret = []byte(v)
	}
	if v := os.Getenv("CA_RELAY_JWT_ISSUER"); v != "" {
		cfg.JWTIssuer = v
	}
	if v := os.Getenv("CA_RELAY_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("CA_GUEST_MAX_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("CA_GUEST_MAX_TTL: %w", err)
		}
		cfg.GuestMaxTTL = d
	}
	if v := os.Getenv("CA_GUEST_WEB_BASE_URL"); v != "" {
		cfg.GuestWebBaseURL = strings.TrimRight(v, "/")
	}
	return cfg, cfg.Validate()
}

// Validate ensures required fields are set.
func (c Config) Validate() error {
	if c.ListenAddr == "" {
		return errors.New("ListenAddr is required")
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("CA_RELAY_JWT_SECRET must be at least 32 bytes (got %d)", len(c.JWTSecret))
	}
	if strings.TrimSpace(c.JWTIssuer) == "" {
		return errors.New("JWTIssuer is required")
	}
	if c.DataDir == "" {
		return errors.New("DataDir is required")
	}
	return nil
}
