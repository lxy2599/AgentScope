package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// DefaultBcryptCost is the bcrypt cost factor used for password hashing.
// Exported as a constant so callers can inspect it without hardcoding.
const DefaultBcryptCost = 12

// Minimum password length in Unicode characters and maximum encoded length in
// bytes. The byte ceiling stays below bcrypt's 72-byte input limit.
const (
	MinPasswordLen = 8
	MaxPasswordLen = 64
)

// Registration errors that are safe for callers to classify and present as
// fixed client-validation messages.
var (
	ErrEmailAlreadyRegistered = errors.New("email already registered")
	ErrInvalidCredentials     = errors.New("invalid email or password")
	ErrPasswordEmpty          = errors.New("password must not be empty")
	ErrPasswordTooShort       = fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	ErrPasswordTooLong        = fmt.Errorf("password must be at most %d bytes", MaxPasswordLen)
)

// HashPassword hashes a plaintext password using bcrypt with DefaultBcryptCost.
// Returns an error if the password is empty, too short, or too long.
func HashPassword(password string) ([]byte, error) {
	if password == "" {
		return nil, ErrPasswordEmpty
	}
	if utf8.RuneCountInString(password) < MinPasswordLen {
		return nil, ErrPasswordTooShort
	}
	if len(password) > MaxPasswordLen {
		return nil, ErrPasswordTooLong
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

const (
	sessionVersion       = "v1"
	maxSessionFutureSkew = 5 * time.Minute

	// MinSessionSecretLen is the minimum HMAC secret length in bytes.
	MinSessionSecretLen = 32
	// DefaultSessionTTL is the default server-side session lifetime.
	DefaultSessionTTL = 7 * 24 * time.Hour
)

var (
	ErrMalformedSession        = errors.New("malformed session cookie")
	ErrInvalidSessionSignature = errors.New("invalid session signature")
	ErrSessionExpired          = errors.New("session expired")
	ErrSessionIssuedInFuture   = errors.New("session issued too far in the future")
	errSessionSignerInvalid    = errors.New("session signer is not configured")
)

// SessionSigner creates and verifies versioned, time-bounded signed session
// cookie values. The signature covers the version, issue time, and user ID.
type SessionSigner struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewSessionSigner creates a new SessionSigner with the given HMAC secret.
// It uses DefaultSessionTTL and the system clock.
func NewSessionSigner(secret []byte) SessionSigner {
	return NewSessionSignerWithTTL(secret, DefaultSessionTTL)
}

// NewSessionSignerWithTTL creates a signer with a custom positive TTL.
func NewSessionSignerWithTTL(secret []byte, ttl time.Duration) SessionSigner {
	return NewSessionSignerWithClock(secret, ttl, time.Now)
}

// NewSessionSignerWithClock creates a signer with an injectable clock for
// deterministic expiry tests.
func NewSessionSignerWithClock(secret []byte, ttl time.Duration, now func() time.Time) SessionSigner {
	if len(secret) < MinSessionSecretLen {
		panic(fmt.Sprintf("session signer secret must be at least %d bytes", MinSessionSecretLen))
	}
	if ttl <= 0 {
		panic("session signer TTL must be positive")
	}
	if now == nil {
		panic("session signer clock must not be nil")
	}
	return SessionSigner{
		secret: append([]byte(nil), secret...),
		ttl:    ttl,
		now:    now,
	}
}

// TTL returns the server-enforced session lifetime.
func (s SessionSigner) TTL() time.Duration {
	return s.ttl
}

// SignUserID creates a signed cookie value embedding the given userID.
// The returned string is URL-safe and contains no characters that would
// interfere with HTTP cookie encoding.
func (s SessionSigner) SignUserID(userID string) (string, error) {
	if userID == "" {
		return "", errors.New("userID must not be empty")
	}
	if !s.valid() {
		return "", errSessionSignerInvalid
	}

	issuedAt := strconv.FormatInt(s.now().UTC().UnixNano(), 10)
	uidB64 := base64.RawURLEncoding.EncodeToString([]byte(userID))
	payload := strings.Join([]string{sessionVersion, issuedAt, uidB64}, ".")

	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(payload))
	sig := mac.Sum(nil)
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return payload + "." + sigB64, nil
}

// VerifyUserID extracts and verifies the userID from a signed cookie value.
// Legacy, expired, and materially future-dated values are rejected.
func (s SessionSigner) VerifyUserID(cookieValue string) (string, error) {
	if !s.valid() {
		return "", errSessionSignerInvalid
	}

	parts := strings.SplitN(cookieValue, ".", 5)
	if len(parts) != 4 || parts[0] != sessionVersion {
		return "", ErrMalformedSession
	}

	issuedUnixNano, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", ErrMalformedSession
	}

	userIDBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(userIDBytes) == 0 {
		return "", ErrMalformedSession
	}
	userID := string(userIDBytes)

	providedSig, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return "", ErrMalformedSession
	}

	payload := strings.Join(parts[:3], ".")
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(payload))
	expected := mac.Sum(nil)

	if !hmac.Equal(providedSig, expected) {
		return "", ErrInvalidSessionSignature
	}

	now := s.now().UTC()
	issuedAt := time.Unix(0, issuedUnixNano).UTC()
	if issuedAt.After(now) && issuedAt.Sub(now) > maxSessionFutureSkew {
		return "", ErrSessionIssuedInFuture
	}
	if !issuedAt.After(now) && now.Sub(issuedAt) >= s.ttl {
		return "", ErrSessionExpired
	}

	return userID, nil
}

func (s SessionSigner) valid() bool {
	return len(s.secret) >= MinSessionSecretLen && s.ttl > 0 && s.now != nil
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
