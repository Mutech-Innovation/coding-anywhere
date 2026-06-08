package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/apps/relay/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "relay.db")
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestGuestShareCRUD(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if _, err := s.CreateAccount(ctx, "acc-g"); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	share := store.GuestShare{
		ID:          "share-1",
		AccountID:   "acc-g",
		CreatedAt:   now,
		ExpiresAt:   now.Add(30 * time.Minute),
		PromptLimit: 20,
		MaxSessions: 1,
		Cwd:         "/home/me/project",
		Model:       "gpt-5-high",
	}
	if err := s.PutGuestShare(ctx, share); err != nil {
		t.Fatalf("PutGuestShare: %v", err)
	}

	got, err := s.GetGuestShare(ctx, "share-1")
	if err != nil {
		t.Fatalf("GetGuestShare: %v", err)
	}
	if got.AccountID != "acc-g" || got.PromptLimit != 20 || got.MaxSessions != 1 || got.Revoked {
		t.Fatalf("unexpected share: %+v", got)
	}
	if got.Cwd != "/home/me/project" {
		t.Fatalf("cwd mismatch: got %q", got.Cwd)
	}
	if got.Model != "gpt-5-high" {
		t.Fatalf("model mismatch: got %q", got.Model)
	}
	if !got.ExpiresAt.Equal(share.ExpiresAt) {
		t.Fatalf("expires mismatch: got %v want %v", got.ExpiresAt, share.ExpiresAt)
	}

	if err := s.RevokeGuestShare(ctx, "share-1"); err != nil {
		t.Fatalf("RevokeGuestShare: %v", err)
	}
	got, err = s.GetGuestShare(ctx, "share-1")
	if err != nil {
		t.Fatalf("GetGuestShare after revoke: %v", err)
	}
	if !got.Revoked {
		t.Fatal("expected share to be revoked")
	}

	// MaxSessions defaults to 1 when unset.
	if err := s.PutGuestShare(ctx, store.GuestShare{
		ID: "share-2", AccountID: "acc-g", ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutGuestShare #2: %v", err)
	}
	g2, _ := s.GetGuestShare(ctx, "share-2")
	if g2.MaxSessions != 1 {
		t.Fatalf("expected default MaxSessions=1, got %d", g2.MaxSessions)
	}

	if _, err := s.GetGuestShare(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Purge removes expired shares.
	n, err := s.PurgeExpiredGuestShares(ctx, now.Add(45*time.Minute))
	if err != nil {
		t.Fatalf("PurgeExpiredGuestShares: %v", err)
	}
	if n != 1 { // share-1 (30m) expired; share-2 (1h) survives
		t.Fatalf("expected 1 purged, got %d", n)
	}
}

func TestMigrationsApplyOnce(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "relay.db")
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open #1: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close #1: %v", err)
	}
	// Reopening should be idempotent.
	s2, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open #2: %v", err)
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("Close #2: %v", err)
	}
}

func TestAccountAndDeviceCRUD(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	if _, err := s.CreateAccount(ctx, "acc-1"); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	got, err := s.GetAccount(ctx, "acc-1")
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if got.ID != "acc-1" {
		t.Fatalf("id: %s", got.ID)
	}
	if _, err := s.GetAccount(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	if err := s.CreateDevice(ctx, store.Device{
		ID: "dev-1", AccountID: "acc-1", Kind: store.DeviceDaemon, Label: "laptop",
	}); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	if err := s.CreateDevice(ctx, store.Device{
		ID: "dev-2", AccountID: "acc-1", Kind: store.DeviceClient, Label: "phone",
	}); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	devs, err := s.ListDevices(ctx, "acc-1")
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(devs) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devs))
	}

	if err := s.TouchDevice(ctx, "dev-1"); err != nil {
		t.Fatalf("TouchDevice: %v", err)
	}
	d, err := s.GetDevice(ctx, "dev-1")
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if d.LastSeenAt == nil {
		t.Fatal("LastSeenAt should be set after TouchDevice")
	}
}

func TestPairingFlow(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	now := time.Now().UTC()

	pairing := store.Pairing{
		PairingToken:         "pt-abc",
		Code:                 "123456",
		InitiatorDeviceKind:  store.DeviceDaemon,
		InitiatorDeviceLabel: "laptop",
		CreatedAt:            now,
		ExpiresAt:            now.Add(5 * time.Minute),
	}
	if err := s.CreatePairing(ctx, pairing); err != nil {
		t.Fatalf("CreatePairing: %v", err)
	}

	gotByToken, err := s.GetPairingByToken(ctx, "pt-abc")
	if err != nil {
		t.Fatalf("GetPairingByToken: %v", err)
	}
	if gotByToken.Code != "123456" || gotByToken.Claimed {
		t.Fatalf("unexpected: %+v", gotByToken)
	}

	gotByCode, err := s.GetPairingByCode(ctx, "123456")
	if err != nil {
		t.Fatalf("GetPairingByCode: %v", err)
	}
	if gotByCode.PairingToken != "pt-abc" {
		t.Fatalf("unexpected: %+v", gotByCode)
	}

	// Need account + devices to satisfy FK on claim.
	if _, err := s.CreateAccount(ctx, "acc-1"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"dev-init", "dev-claim"} {
		if err := s.CreateDevice(ctx, store.Device{
			ID: id, AccountID: "acc-1", Kind: store.DeviceDaemon, Label: id,
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.MarkPairingClaimed(ctx, "pt-abc", "dev-init", "acc-1", "dev-claim", `{"x":1}`); err != nil {
		t.Fatalf("MarkPairingClaimed: %v", err)
	}
	// Second call must be a no-op (idempotency).
	if err := s.MarkPairingClaimed(ctx, "pt-abc", "dev-init", "acc-1", "dev-claim", `{"x":1}`); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on double claim, got %v", err)
	}

	pairAfter, _ := s.GetPairingByToken(ctx, "pt-abc")
	if !pairAfter.Claimed || pairAfter.ClaimedDeviceID == nil || *pairAfter.ClaimedDeviceID != "dev-claim" {
		t.Fatalf("post-claim state: %+v", pairAfter)
	}

	// Purge expired (none yet).
	n, err := s.PurgeExpiredPairings(ctx, now)
	if err != nil {
		t.Fatalf("PurgeExpiredPairings: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 purged, got %d", n)
	}

	// Move cutoff past expiry.
	n, err = s.PurgeExpiredPairings(ctx, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("PurgeExpiredPairings #2: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 purged, got %d", n)
	}
}

func TestRefreshTokenLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if _, err := s.CreateAccount(ctx, "acc-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateDevice(ctx, store.Device{
		ID: "dev-1", AccountID: "acc-1", Kind: store.DeviceClient, Label: "phone",
	}); err != nil {
		t.Fatal(err)
	}

	rt := store.RefreshToken{
		TokenHash: "h1", AccountID: "acc-1", DeviceID: "dev-1",
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
	if err := s.PutRefreshToken(ctx, rt); err != nil {
		t.Fatalf("PutRefreshToken: %v", err)
	}
	got, err := s.GetRefreshToken(ctx, "h1")
	if err != nil {
		t.Fatalf("GetRefreshToken: %v", err)
	}
	if got.Revoked {
		t.Fatal("expected not revoked")
	}
	if err := s.RevokeRefreshToken(ctx, "h1"); err != nil {
		t.Fatalf("RevokeRefreshToken: %v", err)
	}
	got, _ = s.GetRefreshToken(ctx, "h1")
	if !got.Revoked {
		t.Fatal("expected revoked")
	}
}
