package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// VAPIDKey holds a single VAPID public/private keypair used by the relay
// to authenticate to push services. We store the private key in plaintext —
// for self-hosted deployments this matches the operator's threat model.
type VAPIDKey struct {
	ID         string
	PublicKey  string
	PrivateKey string
	CreatedAt  time.Time
}

// PutVAPIDKey persists a keypair, replacing any existing row with the same id.
func (s *Store) PutVAPIDKey(ctx context.Context, k VAPIDKey) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vapid_keys(id, public_key, private_key, created_at)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			public_key = excluded.public_key,
			private_key = excluded.private_key,
			created_at = excluded.created_at`,
		k.ID, k.PublicKey, k.PrivateKey, k.CreatedAt.UnixMilli())
	return err
}

// LatestVAPIDKey returns the most recently created keypair.
func (s *Store) LatestVAPIDKey(ctx context.Context) (VAPIDKey, error) {
	var k VAPIDKey
	var created int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, public_key, private_key, created_at
		FROM vapid_keys ORDER BY created_at DESC LIMIT 1`,
	).Scan(&k.ID, &k.PublicKey, &k.PrivateKey, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return VAPIDKey{}, ErrNotFound
	}
	if err != nil {
		return VAPIDKey{}, err
	}
	k.CreatedAt = time.UnixMilli(created).UTC()
	return k, nil
}

// PushSubscription is one stored Web Push subscription belonging to a device.
type PushSubscription struct {
	ID        string
	AccountID string
	DeviceID  string
	Endpoint  string
	P256DHKey string
	AuthKey   string
	UserAgent string
	CreatedAt time.Time
}

// UpsertPushSubscription stores a subscription, deduplicating by
// (account_id, endpoint).
func (s *Store) UpsertPushSubscription(ctx context.Context, sub PushSubscription) error {
	if sub.AccountID == "" || sub.DeviceID == "" || sub.Endpoint == "" {
		return errors.New("subscription requires account_id, device_id, endpoint")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO push_subscriptions(
			id, account_id, device_id, endpoint, p256dh_key, auth_key, user_agent, created_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, endpoint) DO UPDATE SET
			device_id = excluded.device_id,
			p256dh_key = excluded.p256dh_key,
			auth_key = excluded.auth_key,
			user_agent = excluded.user_agent`,
		sub.ID, sub.AccountID, sub.DeviceID, sub.Endpoint,
		sub.P256DHKey, sub.AuthKey, sub.UserAgent, sub.CreatedAt.UnixMilli())
	return err
}

// ListPushSubscriptionsForAccount returns every active subscription for the
// account.
func (s *Store) ListPushSubscriptionsForAccount(ctx context.Context, accountID string) ([]PushSubscription, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, account_id, device_id, endpoint, p256dh_key, auth_key, user_agent, created_at
		FROM push_subscriptions WHERE account_id = ?`, accountID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []PushSubscription
	for rows.Next() {
		var sub PushSubscription
		var created int64
		var ua sql.NullString
		if err := rows.Scan(&sub.ID, &sub.AccountID, &sub.DeviceID, &sub.Endpoint,
			&sub.P256DHKey, &sub.AuthKey, &ua, &created); err != nil {
			return nil, err
		}
		if ua.Valid {
			sub.UserAgent = ua.String
		}
		sub.CreatedAt = time.UnixMilli(created).UTC()
		out = append(out, sub)
	}
	return out, rows.Err()
}

// DeletePushSubscription removes a subscription by endpoint within the account.
func (s *Store) DeletePushSubscription(ctx context.Context, accountID, endpoint string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM push_subscriptions WHERE account_id = ? AND endpoint = ?`,
		accountID, endpoint)
	return err
}
