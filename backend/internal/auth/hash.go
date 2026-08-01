package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost controls the computational cost of hashing. bcrypt's
// default (bcrypt.DefaultCost, currently 10) is a reasonable baseline,
// but we set it explicitly rather than relying on the library's default
// constant. This is deliberate: if a future version of golang.org/x/crypto
// changes DefaultCost, we don't want our security posture to silently
// shift with a dependency bump. 12 is a widely-recommended value as of
// 2026 — high enough to meaningfully slow down offline brute-force
// attempts, low enough to keep login latency well under 200ms on
// typical server hardware.
const bcryptCost = 12

// ErrPasswordTooShort is returned by HashPassword when the input
// password does not meet minimum length requirements. Enforcing this
// here — not just client-side — ensures the constraint holds even if
// the frontend validation is bypassed (e.g. direct API calls).
var ErrPasswordTooShort = errors.New("auth: password must be at least 8 characters")

// minPasswordLength is intentionally conservative for Phase 1. This is
// the single place to raise the bar later (e.g. requiring mixed
// character classes) without touching handler code.
const minPasswordLength = 8

// HashPassword hashes a plaintext password using bcrypt. The returned
// string is the full bcrypt-encoded hash (algorithm identifier, cost,
// salt, and hash all combined) — this is what gets stored in the
// password_hash column. bcrypt generates its own random salt internally,
// so no separate salt handling is needed anywhere else in the codebase.
func HashPassword(plaintext string) (string, error) {
	if len(plaintext) < minPasswordLength {
		return "", ErrPasswordTooShort
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("auth: failed to hash password: %w", err)
	}

	return string(hash), nil
}

// CheckPassword compares a plaintext password against a stored bcrypt
// hash. Returns nil if they match, or a non-nil error otherwise.
//
// Callers (specifically the login handler) should treat ANY non-nil
// error here as "invalid credentials" and respond identically whether
// the username didn't exist or the password was wrong. Distinguishing
// those two cases in the HTTP response is a username-enumeration
// vulnerability — this function's error is intentionally generic
// (bcrypt.ErrMismatchedHashAndPassword) to make that easy to get right
// at the call site.
func CheckPassword(plaintext, hash string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext))
	if err != nil {
		return fmt.Errorf("auth: password mismatch: %w", err)
	}
	return nil
}