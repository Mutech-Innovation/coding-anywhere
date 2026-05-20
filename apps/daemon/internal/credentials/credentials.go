// Package credentials persists and loads the daemon's relay credentials.
//
// On *nix the credentials file lives at:
//
//	$XDG_DATA_HOME/coding-anywhere/credentials.json
//	~/.local/share/coding-anywhere/credentials.json
//
// On macOS the same path is used (we don't write to ~/Library/Application
// Support yet — the XDG-style layout is friendlier to scripted setups).
package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Bundle mirrors apps/relay/internal/server.CredentialBundle but lives in the
// daemon. Fields here are everything the daemon needs to dial the relay and
// later refresh its access token.
type Bundle struct {
	RelayURL              string    `json:"relay_url"`
	AccountID             string    `json:"account_id"`
	DeviceID              string    `json:"device_id"`
	DeviceKind            string    `json:"device_kind"`
	DeviceLabel           string    `json:"device_label"`
	AccessToken           string    `json:"access_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
	RelayProtocolVersion  int       `json:"relay_protocol_version"`
}

// Validate checks that the bundle has the fields required to dial the relay.
func (b *Bundle) Validate() error {
	if b.RelayURL == "" {
		return errors.New("relay_url required")
	}
	if b.AccessToken == "" {
		return errors.New("access_token required")
	}
	if b.DeviceID == "" {
		return errors.New("device_id required")
	}
	if b.AccountID == "" {
		return errors.New("account_id required")
	}
	return nil
}

// DefaultPath returns the canonical credentials file location, honouring
// XDG_DATA_HOME and falling back to ~/.local/share.
func DefaultPath() (string, error) {
	if v := os.Getenv("CA_CREDENTIALS_FILE"); v != "" {
		return v, nil
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home dir: %w", err)
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "coding-anywhere", "credentials.json"), nil
}

// Load reads the credentials file. Returns os.ErrNotExist when the file
// is absent so callers can treat first-run specially.
func Load(path string) (*Bundle, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b Bundle
	if err := json.Unmarshal(buf, &b); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	if err := b.Validate(); err != nil {
		return nil, fmt.Errorf("validate credentials: %w", err)
	}
	return &b, nil
}

// Save atomically writes the bundle to `path` with 0600 permissions.
func Save(path string, b *Bundle) error {
	if err := b.Validate(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	buf, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "credentials-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(append(buf, '\n')); err != nil {
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}

// Clear removes the credentials file. Idempotent.
func Clear(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
