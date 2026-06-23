package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// CreateUser inserts a new user row. The caller supplies the id (a ULID),
// a username (unique, case-insensitive), and a pre-computed password hash.
// A UNIQUE-constraint error surfaces when the username is taken.
func (s *Store) CreateUser(ctx context.Context, u User) error {
	if u.ID == "" || u.Username == "" || u.PasswordHash == "" {
		return errors.New("user id/username/password_hash required")
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users(id, username, password_hash, created_at)
		VALUES(?, ?, ?, ?)`,
		u.ID, u.Username, u.PasswordHash, u.CreatedAt.UnixMilli())
	return err
}

// GetUserByUsername looks up a user by username (case-insensitive), or
// ErrNotFound.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	return s.getUser(ctx, "username", username)
}

// GetUserByID looks up a user by id, or ErrNotFound.
func (s *Store) GetUserByID(ctx context.Context, id string) (User, error) {
	return s.getUser(ctx, "id", id)
}

func (s *Store) getUser(ctx context.Context, col, value string) (User, error) {
	var q string
	switch col {
	case "username":
		q = `SELECT id, username, password_hash, created_at FROM users WHERE username = ? COLLATE NOCASE`
	case "id":
		q = `SELECT id, username, password_hash, created_at FROM users WHERE id = ?`
	default:
		return User{}, fmt.Errorf("invalid user lookup column: %s", col)
	}
	var u User
	var createdAt int64
	err := s.db.QueryRowContext(ctx, q, value).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.CreatedAt = time.UnixMilli(createdAt).UTC()
	return u, nil
}

// SetUserPassword replaces a user's password hash. Returns ErrNotFound when
// no row matches.
func (s *Store) SetUserPassword(ctx context.Context, userID, passwordHash string) error {
	if passwordHash == "" {
		return errors.New("password_hash required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID)
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

// LinkAccountOwner records that a user owns an account. Idempotent: linking
// an already-linked pair is a no-op.
func (s *Store) LinkAccountOwner(ctx context.Context, userID, accountID string) error {
	if userID == "" || accountID == "" {
		return errors.New("user_id/account_id required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO account_owners(user_id, account_id, created_at)
		VALUES(?, ?, ?)
		ON CONFLICT(user_id, account_id) DO NOTHING`,
		userID, accountID, time.Now().UTC().UnixMilli())
	return err
}

// ListAccountsForUser returns every account a user owns, oldest link first.
func (s *Store) ListAccountsForUser(ctx context.Context, userID string) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.created_at, a.account_key_hash
		FROM account_owners o
		JOIN accounts a ON a.id = o.account_id
		WHERE o.user_id = ?
		ORDER BY o.created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Account
	for rows.Next() {
		var a Account
		var createdAt int64
		var keyHash sql.NullString
		if err := rows.Scan(&a.ID, &createdAt, &keyHash); err != nil {
			return nil, err
		}
		a.CreatedAt = time.UnixMilli(createdAt).UTC()
		a.AccountKeyHash = stringPtr(keyHash)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListSiblingAccounts returns every account that shares an owner with the
// given account (including the account itself), oldest first. This powers the
// in-app machine switcher: from the account a client is currently on, it can
// enumerate the other machines the same user owns without re-entering a
// password.
func (s *Store) ListSiblingAccounts(ctx context.Context, accountID string) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT a.id, a.created_at, a.account_key_hash
		FROM account_owners o1
		JOIN account_owners o2 ON o1.user_id = o2.user_id
		JOIN accounts a ON a.id = o2.account_id
		WHERE o1.account_id = ?
		ORDER BY a.created_at ASC`, accountID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Account
	for rows.Next() {
		var a Account
		var createdAt int64
		var keyHash sql.NullString
		if err := rows.Scan(&a.ID, &createdAt, &keyHash); err != nil {
			return nil, err
		}
		a.CreatedAt = time.UnixMilli(createdAt).UTC()
		a.AccountKeyHash = stringPtr(keyHash)
		out = append(out, a)
	}
	return out, rows.Err()
}

// UserOwnsAccount reports whether the given user owns the given account.
func (s *Store) UserOwnsAccount(ctx context.Context, userID, accountID string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM account_owners WHERE user_id = ? AND account_id = ?`,
		userID, accountID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
