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
	sessionUserVersion    = "v1"
	sessionPayloadVersion = "v2"
	maxSessionFutureSkew  = 5 * time.Minute

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

// SignUserID creates a legacy v1 signed cookie value embedding a user ID.
// New server-authoritative sessions use SignSessionPayload instead.
func (s SessionSigner) SignUserID(userID string) (string, error) {
	return s.signPayload(sessionUserVersion, userID)
}

// VerifyUserID verifies a legacy v1 signed user cookie. It is kept only for
// compatibility with test-only and pre-migration dependency wiring.
func (s SessionSigner) VerifyUserID(cookieValue string) (string, error) {
	return s.verifyPayload(cookieValue, sessionUserVersion)
}

// SignSessionPayload creates a v2 signed session payload. The caller supplies
// an opaque value (session ID plus random secret), not a user ID.
func (s SessionSigner) SignSessionPayload(payload string) (string, error) {
	return s.signPayload(sessionPayloadVersion, payload)
}

// VerifySessionPayload verifies and extracts a v2 session payload.
func (s SessionSigner) VerifySessionPayload(cookieValue string) (string, error) {
	return s.verifyPayload(cookieValue, sessionPayloadVersion)
}

func (s SessionSigner) signPayload(version, value string) (string, error) {
	if value == "" {
		return "", errors.New("session payload must not be empty")
	}
	if !s.valid() {
		return "", errSessionSignerInvalid
	}

	issuedAt := strconv.FormatInt(s.now().UTC().UnixNano(), 10)
	valueB64 := base64.RawURLEncoding.EncodeToString([]byte(value))
	payload := strings.Join([]string{version, issuedAt, valueB64}, ".")

	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(payload))
	sigB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sigB64, nil
}

func (s SessionSigner) verifyPayload(cookieValue, expectedVersion string) (string, error) {
	if !s.valid() {
		return "", errSessionSignerInvalid
	}

	parts := strings.SplitN(cookieValue, ".", 5)
	if len(parts) != 4 || parts[0] != expectedVersion {
		return "", ErrMalformedSession
	}
	issuedUnixNano, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", ErrMalformedSession
	}
	valueBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(valueBytes) == 0 {
		return "", ErrMalformedSession
	}
	providedSig, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return "", ErrMalformedSession
	}
	payload := strings.Join(parts[:3], ".")
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(payload))
	if !hmac.Equal(providedSig, mac.Sum(nil)) {
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
	return string(valueBytes), nil
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
