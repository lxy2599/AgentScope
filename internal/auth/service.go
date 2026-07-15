package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// DefaultBcryptCost is the bcrypt cost factor used for password hashing.
// Exported as a constant so callers can inspect it without hardcoding.
const DefaultBcryptCost = 12

// Minimum and maximum password lengths.
const (
	MinPasswordLen = 4
	MaxPasswordLen = 64
)

// HashPassword hashes a plaintext password using bcrypt with DefaultBcryptCost.
// Returns an error if the password is empty, too short, or too long.
func HashPassword(password string) ([]byte, error) {
	if password == "" {
		return nil, errors.New("password must not be empty")
	}
	if len(password) < MinPasswordLen {
		return nil, fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	}
	if len(password) > MaxPasswordLen {
		return nil, fmt.Errorf("password must be at most %d characters", MaxPasswordLen)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), DefaultBcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	return hash, nil
}

// CheckPassword compares a plaintext password against a bcrypt hash.
// Returns true if the password matches the hash.
func CheckPassword(hash []byte, password string) bool {
	err := bcrypt.CompareHashAndPassword(hash, []byte(password))
	return err == nil
}

// SessionSigner creates and verifies signed session cookie values.
// Each signed value is "base64(userID).base64(signature)" where signature
// is HMAC-SHA256 over the userID using the configured secret.
type SessionSigner struct {
	secret []byte
}

// minSignerSecretLen is the minimum length of the HMAC secret.
const minSignerSecretLen = 8

// NewSessionSigner creates a new SessionSigner with the given HMAC secret.
// Panics if the secret is shorter than minSignerSecretLen.
func NewSessionSigner(secret []byte) SessionSigner {
	if len(secret) < minSignerSecretLen {
		panic(fmt.Sprintf("session signer secret must be at least %d bytes", minSignerSecretLen))
	}
	return SessionSigner{secret: secret}
}

// SignUserID creates a signed cookie value embedding the given userID.
// The returned string is URL-safe and contains no characters that would
// interfere with HTTP cookie encoding.
func (s SessionSigner) SignUserID(userID string) (string, error) {
	if userID == "" {
		return "", errors.New("userID must not be empty")
	}
	uidB64 := base64.RawURLEncoding.EncodeToString([]byte(userID))

	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(userID))
	sig := mac.Sum(nil)
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return uidB64 + "." + sigB64, nil
}

// VerifyUserID extracts and verifies the userID from a signed cookie value.
// Returns the userID on success, or an error if the signature is invalid
// or the cookie value is malformed.
func (s SessionSigner) VerifyUserID(cookieValue string) (string, error) {
	parts := strings.SplitN(cookieValue, ".", 2)
	if len(parts) != 2 {
		return "", errors.New("malformed session cookie")
	}

	uidB64, sigB64 := parts[0], parts[1]

	userIDBytes, err := base64.RawURLEncoding.DecodeString(uidB64)
	if err != nil {
		return "", errors.New("malformed session cookie: invalid userID encoding")
	}
	userID := string(userIDBytes)

	expectedSig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return "", errors.New("malformed session cookie: invalid signature encoding")
	}

	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(userID))
	expected := mac.Sum(nil)

	if !hmac.Equal(expectedSig, expected) {
		return "", errors.New("invalid session cookie: signature mismatch")
	}

	return userID, nil
}

// GenerateRandomBytes returns cryptographically secure random bytes.
// Useful for generating secret keys and token material.
func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate random bytes: %w", err)
	}
	return b, nil
}
