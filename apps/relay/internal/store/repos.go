package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned by lookup methods when no row matches.
var ErrNotFound = errors.New("not found")

// CreateAccount inserts a new account row with the given id (caller-provided
// ULID) and returns the persisted Account.
func (s *Store) CreateAccount(ctx context.Context, id string) (Account, error) {
	now := time.Now().UTC().UnixMilli()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO accounts(id, created_at) VALUES(?, ?)`, id, now); err != nil {
		return Account{}, fmt.Errorf("insert account: %w", err)
	}
	return Account{ID: id, CreatedAt: time.UnixMilli(now).UTC()}, nil
}

// GetAccount returns an account by id, or ErrNotFound.
func (s *Store) GetAccount(ctx context.Context, id string) (Account, error) {
	var a Account
	var createdAt int64
	var keyHash sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, created_at, account_key_hash FROM accounts WHERE id = ?`, id,
	).Scan(&a.ID, &createdAt, &keyHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	a.CreatedAt = time.UnixMilli(createdAt).UTC()
	a.AccountKeyHash = stringPtr(keyHash)
	return a, nil
}

// GetAccountByKeyHash looks up an account by its SHA-256 hex account-key
// digest. Returns ErrNotFound if no row matches (including when the column
// is NULL).
func (s *Store) GetAccountByKeyHash(ctx context.Context, keyHash string) (Account, error) {
	if keyHash == "" {
		return Account{}, ErrNotFound
	}
	var a Account
	var createdAt int64
	var stored sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, created_at, account_key_hash
		 FROM accounts WHERE account_key_hash = ?`, keyHash,
	).Scan(&a.ID, &createdAt, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	a.CreatedAt = time.UnixMilli(createdAt).UTC()
	a.AccountKeyHash = stringPtr(stored)
	return a, nil
}

// SetAccountKey writes (or replaces) the SHA-256 hex digest of an account's
// long-lived key. Pass an empty string to unset the key.
func (s *Store) SetAccountKey(ctx context.Context, accountID, keyHash string) error {
	var arg any
	if keyHash == "" {
		arg = nil
	} else {
		arg = keyHash
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE accounts SET account_key_hash = ? WHERE id = ?`, arg, accountID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeRefreshTokensForAccount marks every refresh token belonging to the
// given account as revoked. Used as the "nuclear option" when an account key
// is revoked entirely.
func (s *Store) RevokeRefreshTokensForAccount(ctx context.Context, accountID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked = 1 WHERE account_id = ?`, accountID)
	return err
}

// CreateDevice inserts a new device row.
func (s *Store) CreateDevice(ctx context.Context, d Device) error {
	if d.ID == "" || d.AccountID == "" || d.Label == "" {
		return errors.New("device id/account/label required")
	}
	if d.Kind != DeviceDaemon && d.Kind != DeviceClient {
		return fmt.Errorf("invalid device kind: %q", d.Kind)
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO devices(id, account_id, kind, label, created_at)
		VALUES(?, ?, ?, ?, ?)`,
		d.ID, d.AccountID, string(d.Kind), d.Label, d.CreatedAt.UnixMilli())
	return err
}

// GetDevice returns a device by id, or ErrNotFound.
func (s *Store) GetDevice(ctx context.Context, id string) (Device, error) {
	var d Device
	var kind, label string
	var createdAt int64
	var lastSeenAt sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, account_id, kind, label, created_at, last_seen_at
		FROM devices WHERE id = ?`, id,
	).Scan(&d.ID, &d.AccountID, &kind, &label, &createdAt, &lastSeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, err
	}
	d.Kind = DeviceKind(kind)
	d.Label = label
	d.CreatedAt = time.UnixMilli(createdAt).UTC()
	if lastSeenAt.Valid {
		t := time.UnixMilli(lastSeenAt.Int64).UTC()
		d.LastSeenAt = &t
	}
	return d, nil
}

// ListDevices returns all devices on an account, oldest first.
func (s *Store) ListDevices(ctx context.Context, accountID string) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, account_id, kind, label, created_at, last_seen_at
		FROM devices WHERE account_id = ? ORDER BY created_at ASC`, accountID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Device
	for rows.Next() {
		var d Device
		var kind, label string
		var createdAt int64
		var lastSeenAt sql.NullInt64
		if err := rows.Scan(&d.ID, &d.AccountID, &kind, &label, &createdAt, &lastSeenAt); err != nil {
			return nil, err
		}
		d.Kind = DeviceKind(kind)
		d.Label = label
		d.CreatedAt = time.UnixMilli(createdAt).UTC()
		if lastSeenAt.Valid {
			t := time.UnixMilli(lastSeenAt.Int64).UTC()
			d.LastSeenAt = &t
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// TouchDevice updates last_seen_at on a device.
func (s *Store) TouchDevice(ctx context.Context, deviceID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE devices SET last_seen_at = ? WHERE id = ?`,
		time.Now().UTC().UnixMilli(), deviceID)
	return err
}

// CreatePairing inserts a fresh pairing row.
func (s *Store) CreatePairing(ctx context.Context, p Pairing) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pairings(
			pairing_token, code, initiator_account_id,
			initiator_device_kind, initiator_device_label,
			created_at, expires_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`,
		p.PairingToken, p.Code, nullableString(p.InitiatorAccountID),
		string(p.InitiatorDeviceKind), p.InitiatorDeviceLabel,
		p.CreatedAt.UnixMilli(), p.ExpiresAt.UnixMilli())
	return err
}

// GetPairingByToken returns a pairing by its long token.
func (s *Store) GetPairingByToken(ctx context.Context, token string) (Pairing, error) {
	return s.getPairing(ctx, "pairing_token", token)
}

// GetPairingByCode returns a pairing by its 6-digit code.
func (s *Store) GetPairingByCode(ctx context.Context, code string) (Pairing, error) {
	return s.getPairing(ctx, "code", code)
}

func (s *Store) getPairing(ctx context.Context, col, value string) (Pairing, error) {
	var q string
	switch col {
	case "pairing_token":
		q = `
		SELECT pairing_token, code, initiator_account_id,
			   initiator_device_kind, initiator_device_label,
			   initiator_device_id, created_at, expires_at,
			   claimed, claimed_account_id, claimed_device_id,
			   claimed_credentials_json
		FROM pairings WHERE pairing_token = ?`
	case "code":
		q = `
		SELECT pairing_token, code, initiator_account_id,
			   initiator_device_kind, initiator_device_label,
			   initiator_device_id, created_at, expires_at,
			   claimed, claimed_account_id, claimed_device_id,
			   claimed_credentials_json
		FROM pairings WHERE code = ?`
	default:
		return Pairing{}, fmt.Errorf("invalid pairing lookup column: %s", col)
	}
	var p Pairing
	var initAcc, initDev, claimedAcc, claimedDev, credJSON sql.NullString
	var kind, label string
	var createdAt, expiresAt int64
	var claimed int
	err := s.db.QueryRowContext(ctx, q, value).Scan(
		&p.PairingToken, &p.Code, &initAcc,
		&kind, &label, &initDev,
		&createdAt, &expiresAt,
		&claimed, &claimedAcc, &claimedDev, &credJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Pairing{}, ErrNotFound
	}
	if err != nil {
		return Pairing{}, err
	}
	p.InitiatorDeviceKind = DeviceKind(kind)
	p.InitiatorDeviceLabel = label
	p.CreatedAt = time.UnixMilli(createdAt).UTC()
	p.ExpiresAt = time.UnixMilli(expiresAt).UTC()
	p.Claimed = claimed != 0
	p.InitiatorAccountID = stringPtr(initAcc)
	p.InitiatorDeviceID = stringPtr(initDev)
	p.ClaimedAccountID = stringPtr(claimedAcc)
	p.ClaimedDeviceID = stringPtr(claimedDev)
	p.ClaimedCredentialsJSON = stringPtr(credJSON)
	return p, nil
}

// MarkPairingClaimed atomically updates a pairing as claimed and stores the
// credentials the initiator will fetch on its next status poll.
func (s *Store) MarkPairingClaimed(ctx context.Context, pairingToken, initiatorDeviceID, claimedAccountID, claimedDeviceID, credentialsJSON string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE pairings SET
			claimed = 1,
			initiator_device_id = ?,
			claimed_account_id = ?,
			claimed_device_id = ?,
			claimed_credentials_json = ?
		WHERE pairing_token = ? AND claimed = 0`,
		initiatorDeviceID, claimedAccountID, claimedDeviceID,
		credentialsJSON, pairingToken)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePairing removes a pairing row (called after the initiator has
// fetched its credentials, or at GC time).
func (s *Store) DeletePairing(ctx context.Context, pairingToken string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pairings WHERE pairing_token = ?`, pairingToken)
	return err
}

// PurgeExpiredPairings deletes pairings whose expires_at is before `cutoff`.
// Returns the number of rows deleted.
func (s *Store) PurgeExpiredPairings(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM pairings WHERE expires_at < ?`, cutoff.UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PutRefreshToken stores a refresh-token row keyed by its hash.
func (s *Store) PutRefreshToken(ctx context.Context, rt RefreshToken) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO refresh_tokens(token_hash, account_id, device_id, created_at, expires_at)
		VALUES(?, ?, ?, ?, ?)`,
		rt.TokenHash, rt.AccountID, rt.DeviceID,
		rt.CreatedAt.UnixMilli(), rt.ExpiresAt.UnixMilli())
	return err
}

// GetRefreshToken looks up a refresh token by its hash.
func (s *Store) GetRefreshToken(ctx context.Context, tokenHash string) (RefreshToken, error) {
	var rt RefreshToken
	var createdAt, expiresAt int64
	var revoked int
	err := s.db.QueryRowContext(ctx, `
		SELECT token_hash, account_id, device_id, created_at, expires_at, revoked
		FROM refresh_tokens WHERE token_hash = ?`, tokenHash,
	).Scan(&rt.TokenHash, &rt.AccountID, &rt.DeviceID, &createdAt, &expiresAt, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return RefreshToken{}, ErrNotFound
	}
	if err != nil {
		return RefreshToken{}, err
	}
	rt.CreatedAt = time.UnixMilli(createdAt).UTC()
	rt.ExpiresAt = time.UnixMilli(expiresAt).UTC()
	rt.Revoked = revoked != 0
	return rt, nil
}

// RevokeRefreshToken marks a single refresh-token row as revoked.
func (s *Store) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked = 1 WHERE token_hash = ?`, tokenHash)
	return err
}

// RevokeRefreshTokensForDevice revokes all refresh tokens for the given device.
func (s *Store) RevokeRefreshTokensForDevice(ctx context.Context, deviceID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked = 1 WHERE device_id = ?`, deviceID)
	return err
}

// PutGuestShare stores a guest-share row. The id is caller-provided (a ULID
// that also becomes the guest JWT's device id).
func (s *Store) PutGuestShare(ctx context.Context, g GuestShare) error {
	if g.ID == "" || g.AccountID == "" {
		return errors.New("guest share id/account required")
	}
	if g.MaxSessions <= 0 {
		g.MaxSessions = 1
	}
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO guest_shares(id, account_id, created_at, expires_at, prompt_limit, max_sessions)
		VALUES(?, ?, ?, ?, ?, ?)`,
		g.ID, g.AccountID, g.CreatedAt.UnixMilli(), g.ExpiresAt.UnixMilli(),
		g.PromptLimit, g.MaxSessions)
	return err
}

// GetGuestShare looks up a guest share by id, or ErrNotFound.
func (s *Store) GetGuestShare(ctx context.Context, id string) (GuestShare, error) {
	var g GuestShare
	var createdAt, expiresAt int64
	var revoked int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, account_id, created_at, expires_at, revoked, prompt_limit, max_sessions
		FROM guest_shares WHERE id = ?`, id,
	).Scan(&g.ID, &g.AccountID, &createdAt, &expiresAt, &revoked, &g.PromptLimit, &g.MaxSessions)
	if errors.Is(err, sql.ErrNoRows) {
		return GuestShare{}, ErrNotFound
	}
	if err != nil {
		return GuestShare{}, err
	}
	g.CreatedAt = time.UnixMilli(createdAt).UTC()
	g.ExpiresAt = time.UnixMilli(expiresAt).UTC()
	g.Revoked = revoked != 0
	return g, nil
}

// RevokeGuestShare marks a guest share as revoked. Returns ErrNotFound when
// no row matches.
func (s *Store) RevokeGuestShare(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE guest_shares SET revoked = 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeExpiredGuestShares deletes guest shares whose expires_at is before
// `cutoff`. Returns the number of rows deleted.
func (s *Store) PurgeExpiredGuestShares(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM guest_shares WHERE expires_at < ?`, cutoff.UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func nullableString(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func stringPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}
