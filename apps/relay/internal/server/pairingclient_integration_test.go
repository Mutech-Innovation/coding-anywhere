package server_test

// Integration tests that drive the relay's pairing endpoints through the
// shared internal/pairingclient package. These live alongside the server's
// unit tests because pairingclient lives at the module root (and is therefore
// importable here) while the server package's internals are not importable
// from internal/pairingclient.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/pairingclient"
)

func TestPairingClientFullFlow(t *testing.T) {
	hs, _ := newPairingTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pc := pairingclient.New(hs.URL)

	init, err := pc.Init(ctx, "daemon", "laptop", "")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if init.PairingToken == "" || len(init.Code) != 6 || init.ExpiresIn <= 0 {
		t.Fatalf("init: %+v", init)
	}

	// Status before claim.
	st, err := pc.Status(ctx, init.PairingToken)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Status != "pending" {
		t.Fatalf("status: %s", st.Status)
	}

	// Concurrent claim.
	claimDone := make(chan error, 1)
	go func() {
		_, err := pc.Claim(ctx, init.Code, "client", "phone", "")
		claimDone <- err
	}()

	// Wait for claim to complete.
	cred, err := pc.WaitClaimed(ctx, init.PairingToken, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitClaimed: %v", err)
	}
	if cred.AccessToken == "" || cred.RefreshToken == "" || cred.DeviceKind != "daemon" {
		t.Fatalf("cred: %+v", cred)
	}
	if err := <-claimDone; err != nil {
		t.Fatalf("claim: %v", err)
	}

	// Refresh access token using daemon's refresh.
	access, ttl, err := pc.Refresh(ctx, cred.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if access == "" || ttl <= 0 {
		t.Fatalf("refresh response: token=%q ttl=%d", access, ttl)
	}
}

func TestPairingClientExpiredCode(t *testing.T) {
	hs, srv := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pc := pairingclient.New(hs.URL)
	init, err := pc.Init(ctx, "daemon", "laptop", "")
	if err != nil {
		t.Fatal(err)
	}

	// Force expiry.
	_, err = srv.Store().DB().ExecContext(ctx,
		`UPDATE pairings SET expires_at = ? WHERE pairing_token = ?`,
		time.Now().Add(-time.Hour).UnixMilli(), init.PairingToken)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pc.WaitClaimed(ctx, init.PairingToken, 100*time.Millisecond); !errors.Is(err, pairingclient.ErrPairingExpired) {
		t.Fatalf("expected ErrPairingExpired, got %v", err)
	}
}

func TestPairingClientUnknownCode(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pc := pairingclient.New(hs.URL)
	if _, err := pc.Claim(ctx, "999999", "client", "phone", ""); err == nil {
		t.Fatal("expected error for unknown code")
	} else if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
}
