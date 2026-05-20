package auth_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/auth"
)

const testSecret = "0123456789abcdef0123456789abcdef" // 32 bytes

func newSigner(t *testing.T, now time.Time) *auth.Signer {
	t.Helper()
	s, err := auth.NewSigner([]byte(testSecret), "ca-test", func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return s
}

func TestNewSignerRejectsShortSecret(t *testing.T) {
	if _, err := auth.NewSigner([]byte("short"), "ca-test", nil); err == nil {
		t.Fatal("expected error for short secret")
	}
}

func TestIssueVerifyRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	s := newSigner(t, now)

	tok, err := s.Issue("acc-1", "dev-1", auth.DeviceDaemon, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if !strings.Contains(tok, ".") {
		t.Fatalf("token doesn't look like a JWT: %s", tok)
	}

	c, err := s.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if c.AccountID != "acc-1" || c.DeviceID != "dev-1" || c.Kind != auth.DeviceDaemon {
		t.Fatalf("unexpected claims: %+v", c)
	}
}

func TestVerifyExpiredToken(t *testing.T) {
	issuedAt := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	s := newSigner(t, issuedAt)
	tok, err := s.Issue("acc-1", "dev-1", auth.DeviceClient, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Move the clock forward past expiry.
	later, err := auth.NewSigner([]byte(testSecret), "ca-test", func() time.Time {
		return issuedAt.Add(2 * time.Hour)
	})
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	if _, err := later.Verify(tok); !errors.Is(err, auth.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestVerifyTamperedSignature(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	s := newSigner(t, now)
	tok, err := s.Issue("acc-1", "dev-1", auth.DeviceClient, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %s", tok)
	}
	tampered := parts[0] + "." + parts[1] + "." + "AAAA"
	if _, err := s.Verify(tampered); !errors.Is(err, auth.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestVerifyDifferentSecret(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	signer := newSigner(t, now)
	tok, err := signer.Issue("acc-1", "dev-1", auth.DeviceClient, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	other, err := auth.NewSigner([]byte("ffffffffffffffffffffffffffffffff"), "ca-test", func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	if _, err := other.Verify(tok); !errors.Is(err, auth.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}
