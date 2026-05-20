package credentials_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/apps/daemon/internal/credentials"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.json")
	bundle := &credentials.Bundle{
		RelayURL:              "ws://localhost:8443",
		AccountID:             "acc-1",
		DeviceID:              "dev-1",
		DeviceKind:            "daemon",
		DeviceLabel:           "laptop",
		AccessToken:           "at",
		AccessTokenExpiresAt:  time.Now().UTC().Add(time.Hour),
		RefreshToken:          "rt",
		RefreshTokenExpiresAt: time.Now().UTC().Add(24 * time.Hour),
		RelayProtocolVersion:  1,
	}
	if err := credentials.Save(path, bundle); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm: %o", info.Mode().Perm())
	}
	got, err := credentials.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AccountID != "acc-1" || got.DeviceID != "dev-1" {
		t.Fatalf("loaded: %+v", got)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := credentials.Load(filepath.Join(t.TempDir(), "missing.json"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected ErrNotExist, got %v", err)
	}
}

func TestSaveValidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := credentials.Save(path, &credentials.Bundle{}); err == nil {
		t.Fatal("expected error saving empty bundle")
	}
}

func TestClearIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	if err := credentials.Clear(path); err != nil {
		t.Fatalf("clear missing: %v", err)
	}
	bundle := &credentials.Bundle{
		RelayURL:    "ws://x",
		AccountID:   "a",
		DeviceID:    "d",
		AccessToken: "t",
	}
	if err := credentials.Save(path, bundle); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := credentials.Clear(path); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file should be gone, stat err=%v", err)
	}
}

func TestDefaultPathHonoursEnv(t *testing.T) {
	t.Setenv("CA_CREDENTIALS_FILE", "/tmp/elsewhere.json")
	got, err := credentials.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/elsewhere.json" {
		t.Fatalf("got %s", got)
	}
}
