package util

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// userIDAlphabet defines the character set for generated user IDs.
//
// Deliberately excludes visually ambiguous characters: 0/O, 1/I/l are
// NOT excluded here because user_id is never manually typed by end
// users (it's not a login credential — username is used for login, and
// user_id is only ever handled programmatically via JWTs and API
// responses). Restricting the alphabet for typability would be
// solving a problem this field doesn't actually have. Using the full
// alphanumeric set maximizes the ID space instead.
const userIDAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// userIDLength is fixed at 8 characters per the project spec.
const userIDLength = 8

// GenerateUserID produces a random 8-character alphanumeric string
// suitable for use as a public-facing user_id.
//
// It uses crypto/rand, NOT math/rand. This matters even though user_id
// is not a secret (it's returned in API responses and embedded in
// JWTs) — math/rand is deterministic and seedable, and a predictable
// ID generator is a bad habit to have anywhere in an auth-adjacent
// codebase. crypto/rand costs essentially nothing extra here and
// removes the question entirely.
//
// With an alphabet of 62 characters and length 8, the ID space is
// 62^8 (~218 trillion) combinations. Collisions are extremely unlikely
// but not impossible at scale, which is exactly why users.go exposes
// ErrUserIDCollision — callers of GenerateUserID are expected to retry
// on that specific error rather than assume uniqueness is guaranteed
// here.
func GenerateUserID() (string, error) {
	return randomString(userIDLength, userIDAlphabet)
}

// randomString generates a random string of the given length drawn
// from the given alphabet, using crypto/rand for each character
// selection. Factored out from GenerateUserID as a general-purpose
// primitive — if we later need random tokens for other purposes
// (password reset codes, invite codes, etc.), this is the function to
// reuse rather than duplicate.
func randomString(length int, alphabet string) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("util: length must be positive, got %d", length)
	}
	if len(alphabet) == 0 {
		return "", fmt.Errorf("util: alphabet must not be empty")
	}

	result := make([]byte, length)
	alphabetSize := big.NewInt(int64(len(alphabet)))

	for i := 0; i < length; i++ {
		// rand.Int returns a uniformly distributed random value in
		// [0, alphabetSize). Using big.Int here (rather than reading a
		// raw byte and taking a modulus) avoids modulo bias — a subtle
		// correctness issue where byte%N doesn't produce a perfectly
		// uniform distribution unless N divides 256 evenly (62 does
		// not). crypto/rand.Int handles the rejection sampling
		// internally to guarantee true uniformity.
		n, err := rand.Int(rand.Reader, alphabetSize)
		if err != nil {
			return "", fmt.Errorf("util: failed to generate random index: %w", err)
		}
		result[i] = alphabet[n.Int64()]
	}

	return string(result), nil
}