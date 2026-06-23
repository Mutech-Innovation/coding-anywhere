package auth_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Mutech-Innovation/coding-anywhere/internal/auth"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("unexpected hash format: %q", hash)
	}

	if err := auth.VerifyPassword(hash, "correct horse battery staple"); err != nil {
		t.Fatalf("VerifyPassword (correct): %v", err)
	}

	if err := auth.VerifyPassword(hash, "wrong password"); !errors.Is(err, auth.ErrPasswordMismatch) {
		t.Fatalf("expected ErrPasswordMismatch, got %v", err)
	}
}

func TestHashPasswordSaltsAreUnique(t *testing.T) {
	a, err := auth.HashPassword("same")
	if err != nil {
		t.Fatal(err)
	}
	b, err := auth.HashPassword("same")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("expected distinct hashes for the same password (random salt)")
	}
}

func TestHashPasswordRejectsEmpty(t *testing.T) {
	if _, err := auth.HashPassword(""); err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestVerifyPasswordRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"", "plaintext", "$argon2id$bad", "$bcrypt$v=19$m=1,t=1,p=1$x$y"} {
		if err := auth.VerifyPassword(bad, "x"); err == nil {
			t.Fatalf("expected error for malformed hash %q", bad)
		}
	}
}
