package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// tokenLifetime controls how long an issued JWT remains valid.
// 24 hours is a reasonable default for a portfolio site's auth needs —
// short enough to limit the damage of a leaked token, long enough that
// a returning visitor within the same day doesn't have to re-login.
// This is the single place to change that tradeoff later.
const tokenLifetime = 24 * time.Hour

// issuer is embedded in every token's claims as a sanity check during
// validation. It's not a security boundary by itself, but it makes
// tokens self-describing and gives us something to check if we ever
// need to support multiple issuers (e.g. the future RPG backend issuing
// its own tokens with different claims).
const issuer = "operation-advertise-backend"

// ErrInvalidToken is returned for any token that fails validation —
// expired, malformed, wrong signature, wrong issuer, etc. Deliberately
// generic: callers (the auth middleware) should respond with a uniform
// 401 Unauthorized regardless of which specific validation step failed,
// rather than leaking details about why a token was rejected.
var ErrInvalidToken = errors.New("auth: invalid or expired token")

// Claims is the JWT payload structure for this application.
//
// UserID holds the public-facing user_id (the random 8-char string),
// NOT the internal database primary key. This is deliberate: the token
// is handed to the client and potentially inspectable (JWTs are signed,
// not encrypted), so it must never contain anything we wouldn't be
// comfortable exposing — the internal auto-increment ID falls into that
// category (see models.User.ID comments), the public user_id does not.
type Claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

// TokenManager holds the signing secret and provides methods to issue
// and validate tokens. Using a struct (rather than package-level
// functions taking a secret parameter every call) means main.go
// constructs one TokenManager at startup from configuration, and every
// downstream consumer (handlers, middleware) just holds a reference to
// it — the secret itself only needs to be threaded through
// construction, not through every function call.
type TokenManager struct {
	secret []byte
}

// NewTokenManager constructs a TokenManager from a signing secret.
// The secret must be non-empty — an empty secret would make tokens
// trivially forgeable, so we fail loudly at startup rather than at
// first request.
func NewTokenManager(secret string) (*TokenManager, error) {
	if len(secret) == 0 {
		return nil, fmt.Errorf("auth: JWT signing secret must not be empty")
	}
	if len(secret) < 32 {
		// Not a hard cryptographic requirement, but a short secret
		// undermines HMAC-SHA256's security margin. Warning via error
		// here (rather than silently accepting) forces whoever configures
		// this in production to generate a properly random secret rather
		// than typing "mysecret" into an env var.
		return nil, fmt.Errorf("auth: JWT signing secret should be at least 32 bytes, got %d", len(secret))
	}
	return &TokenManager{secret: []byte(secret)}, nil
}

// GenerateToken issues a new signed JWT for the given public user_id.
func (tm *TokenManager) GenerateToken(userID string) (string, error) {
	now := time.Now()

	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenLifetime)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signed, err := token.SignedString(tm.secret)
	if err != nil {
		return "", fmt.Errorf("auth: failed to sign token: %w", err)
	}

	return signed, nil
}

// ValidateToken parses and validates a signed JWT string, returning the
// embedded Claims if valid. It checks:
//   - signature (HMAC-SHA256 against our secret)
//   - expiration (handled automatically by the jwt library via ExpiresAt)
//   - issuer (explicitly checked against our known issuer string)
//
// Any failure at any of these steps collapses to ErrInvalidToken — see
// the doc comment on that variable for why we don't distinguish reasons
// at this layer.
func (tm *TokenManager) ValidateToken(tokenString string) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		// Explicitly verify the signing method matches what we expect.
		// Without this check, a token signed with "alg: none" or a
		// different algorithm could bypass verification entirely — a
		// well-known JWT vulnerability class. We only ever accept HS256.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return tm.secret, nil
	})

	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}

	if claims.Issuer != issuer {
		return nil, ErrInvalidToken
	}

	if claims.UserID == "" {
		return nil, ErrInvalidToken
	}

	return claims, nil
}