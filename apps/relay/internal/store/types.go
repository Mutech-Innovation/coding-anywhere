package store

import "time"

// Account groups the devices that share a session graph (i.e. a single
// human's laptops, phones, and dev machines).
type Account struct {
	ID        string
	CreatedAt time.Time
	// AccountKeyHash is the SHA-256 hex digest of the long-lived account key.
	// Nil means the account currently has no active key; in that state new
	// clients cannot log in via /v1/auth/login until the key is rotated.
	AccountKeyHash *string
}

// User is a human identity that can own one or more accounts. The password
// is stored only as an argon2id PHC-format hash; the plaintext never touches
// the database. A user logs in with username+password to reach every account
// (dev machine) they own without re-pairing or pasting an account key.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

// DeviceKind is mirrored on internal/auth.DeviceKind. Keep both in sync.
type DeviceKind string

const (
	DeviceDaemon DeviceKind = "daemon"
	DeviceClient DeviceKind = "client"
)

// Device is a registered participant on an account. Each PTY session, chat
// session, etc. is bound to exactly one device.
type Device struct {
	ID         string
	AccountID  string
	Kind       DeviceKind
	Label      string
	CreatedAt  time.Time
	LastSeenAt *time.Time
}

// Pairing is a transient handshake row that bridges two devices into one
// account. It carries the 6-digit code shown to the user, the long pairing
// token used to poll status, and (after claim) the credentials the initiator
// will fetch.
type Pairing struct {
	PairingToken           string
	Code                   string
	InitiatorAccountID     *string
	InitiatorDeviceKind    DeviceKind
	InitiatorDeviceLabel   string
	InitiatorDeviceID      *string
	CreatedAt              time.Time
	ExpiresAt              time.Time
	Claimed                bool
	ClaimedAccountID       *string
	ClaimedDeviceID        *string
	ClaimedCredentialsJSON *string
}

// RefreshToken is stored as a SHA-256 hash; the plaintext is only ever shown
// once — at issuance — and exchanged for a fresh access token via
// /v1/auth/refresh.
type RefreshToken struct {
	TokenHash string
	AccountID string
	DeviceID  string
	CreatedAt time.Time
	ExpiresAt time.Time
	Revoked   bool
}

// GuestShare is a short-lived, read-only "ask"-mode share link. Its ID
// doubles as the DeviceID embedded in the guest JWT so the relay can re-check
// revocation and per-link limits on every connect. Guests never get a refresh
// token; the JWT simply expires at ExpiresAt.
type GuestShare struct {
	ID        string
	AccountID string
	CreatedAt time.Time
	ExpiresAt time.Time
	Revoked   bool
	// PromptLimit caps how many prompts a guest may send across the share's
	// lifetime. 0 means unlimited.
	PromptLimit int
	// MaxSessions caps how many chat sessions the guest may create. Values
	// <= 0 are treated as 1 by the relay.
	MaxSessions int
	// Cwd is the owner-chosen working directory the guest's chat session runs
	// in. The relay injects it into the guest's create_session so the guest
	// cannot omit or tamper with it. Empty means "no owner-chosen cwd".
	Cwd string
	// Model is the owner-chosen model locked for the guest session. The relay
	// injects it into create_session and the daemon applies it via
	// set_config_option. Empty means "use the agent's default model".
	Model string
	// AgentID is the owner-chosen chat agent ("devin", "cursor-cli") the
	// guest session runs on. The relay injects it into create_session so the
	// guest cannot pick its own agent. Empty means "daemon default" (devin).
	AgentID string
}
