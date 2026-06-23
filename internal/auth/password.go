package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Password hashing uses argon2id with the parameters below. They are encoded
// into every hash (PHC string format) so a future tuning of the cost can be
// applied to new hashes while old ones still verify against their own params.
//
// Defaults target a single-operator self-host: ~64 MiB, 2 passes. Plenty for
// a login that runs at human frequency, cheap enough not to stall the relay.
const (
	argonMemoryKiB  = 64 * 1024 // 64 MiB
	argonTime       = 2
	argonThreads    = 1
	argonSaltLength = 16
	argonKeyLength  = 32
)

// ErrPasswordMismatch is returned by VerifyPassword when the password does
// not match the stored hash.
var ErrPasswordMismatch = errors.New("password mismatch")

// HashPassword hashes a plaintext password with argon2id and returns a
// self-describing PHC-format string:
//
//	$argon2id$v=19$m=65536,t=2,p=1$<base64 salt>$<base64 hash>
func HashPassword(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("password must not be empty")
	}
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("rand salt: %w", err)
	}
	hash := argon2.IDKey([]byte(plain), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLength)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(hash)), nil
}

// VerifyPassword reports whether `plain` matches the argon2id hash in
// `encoded`. It recomputes the hash using the parameters embedded in the
// encoded string, so it keeps verifying old hashes after the cost is retuned.
// Returns nil on a match, ErrPasswordMismatch on a mismatch, or a parse error
// when `encoded` is malformed.
func VerifyPassword(encoded, plain string) error {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", "<salt>", "<hash>"]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return errors.New("invalid password hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return fmt.Errorf("parse version: %w", err)
	}
	if version != argon2.Version {
		return fmt.Errorf("unsupported argon2 version %d", version)
	}
	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return fmt.Errorf("parse params: %w", err)
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return fmt.Errorf("decode salt: %w", err)
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return fmt.Errorf("decode hash: %w", err)
	}
	got := argon2.IDKey([]byte(plain), salt, time, memory, threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrPasswordMismatch
	}
	return nil
}
