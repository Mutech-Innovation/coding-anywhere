package store

import "time"

// Account groups the devices that share a session graph (i.e. a single
// human's laptops, phones, and dev machines).
type Account struct {
	ID        string
	CreatedAt time.Time
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
	PairingToken            string
	Code                    string
	InitiatorAccountID      *string
	InitiatorDeviceKind     DeviceKind
	InitiatorDeviceLabel    string
	InitiatorDeviceID       *string
	CreatedAt               time.Time
	ExpiresAt               time.Time
	Claimed                 bool
	ClaimedAccountID        *string
	ClaimedDeviceID         *string
	ClaimedCredentialsJSON  *string
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
