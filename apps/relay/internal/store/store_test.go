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
		AgentID:     "cursor-cli",
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
	if got.AgentID != "cursor-cli" {
		t.Fatalf("agent mismatch: got %q", got.AgentID)
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

func TestUserCRUD(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	// Two accounts (each a "dev machine") that one user will own.
	for _, id := range []string{"acc-a", "acc-b"} {
		if _, err := s.CreateAccount(ctx, id); err != nil {
			t.Fatalf("CreateAccount %s: %v", id, err)
		}
	}

	u := store.User{ID: "u1", Username: "Me", PasswordHash: "argon2-hash"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Username uniqueness is case-insensitive.
	if err := s.CreateUser(ctx, store.User{ID: "u2", Username: "me", PasswordHash: "x"}); err == nil {
		t.Fatal("expected duplicate-username error (case-insensitive)")
	}

	// Lookup is case-insensitive too.
	got, err := s.GetUserByUsername(ctx, "ME")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got.ID != "u1" || got.PasswordHash != "argon2-hash" {
		t.Fatalf("unexpected user: %+v", got)
	}

	if _, err := s.GetUserByUsername(ctx, "nobody"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Own two accounts; linking is idempotent.
	if err := s.LinkAccountOwner(ctx, "u1", "acc-a"); err != nil {
		t.Fatalf("LinkAccountOwner a: %v", err)
	}
	if err := s.LinkAccountOwner(ctx, "u1", "acc-a"); err != nil {
		t.Fatalf("LinkAccountOwner a (repeat): %v", err)
	}
	if err := s.LinkAccountOwner(ctx, "u1", "acc-b"); err != nil {
		t.Fatalf("LinkAccountOwner b: %v", err)
	}

	accts, err := s.ListAccountsForUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListAccountsForUser: %v", err)
	}
	if len(accts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accts))
	}

	owns, err := s.UserOwnsAccount(ctx, "u1", "acc-a")
	if err != nil || !owns {
		t.Fatalf("UserOwnsAccount acc-a: owns=%v err=%v", owns, err)
	}
	owns, _ = s.UserOwnsAccount(ctx, "u1", "acc-missing")
	if owns {
		t.Fatal("expected not to own acc-missing")
	}

	if err := s.SetUserPassword(ctx, "u1", "new-hash"); err != nil {
		t.Fatalf("SetUserPassword: %v", err)
	}
	got, _ = s.GetUserByID(ctx, "u1")
	if got.PasswordHash != "new-hash" {
		t.Fatalf("password not updated: %q", got.PasswordHash)
	}
}

func TestListSiblingAccounts(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	for _, id := range []string{"acc-a", "acc-b", "acc-c"} {
		if _, err := s.CreateAccount(ctx, id); err != nil {
			t.Fatalf("CreateAccount %s: %v", id, err)
		}
	}
	// u1 owns a + b; u2 owns c (a different person).
	if err := s.CreateUser(ctx, store.User{ID: "u1", Username: "me", PasswordHash: "h"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUser(ctx, store.User{ID: "u2", Username: "other", PasswordHash: "h"}); err != nil {
		t.Fatal(err)
	}
	for _, acc := range []string{"acc-a", "acc-b"} {
		if err := s.LinkAccountOwner(ctx, "u1", acc); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.LinkAccountOwner(ctx, "u2", "acc-c"); err != nil {
		t.Fatal(err)
	}

	// From acc-a, the siblings are a + b (not c).
	sib, err := s.ListSiblingAccounts(ctx, "acc-a")
	if err != nil {
		t.Fatalf("ListSiblingAccounts: %v", err)
	}
	ids := map[string]bool{}
	for _, a := range sib {
		ids[a.ID] = true
	}
	if len(sib) != 2 || !ids["acc-a"] || !ids["acc-b"] || ids["acc-c"] {
		t.Fatalf("expected {acc-a,acc-b}, got %v", ids)
	}

	// acc-c stands alone.
	sib, err = s.ListSiblingAccounts(ctx, "acc-c")
	if err != nil {
		t.Fatal(err)
	}
	if len(sib) != 1 || sib[0].ID != "acc-c" {
		t.Fatalf("expected {acc-c}, got %+v", sib)
	}
}
