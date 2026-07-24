package auth

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

const validSessionSecret = "0123456789abcdef0123456789abcdef"

func TestHashPassword_roundTrip(t *testing.T) {
	password := "correct-horse-battery-staple"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if len(hash) == 0 {
		t.Fatal("HashPassword() returned empty hash")
	}

	if !CheckPassword(hash, password) {
		t.Fatal("CheckPassword() = false, want true for correct password")
	}
	if CheckPassword(hash, "wrong-password") {
		t.Fatal("CheckPassword() = true, want false for incorrect password")
	}
}

func TestHashPassword_emptyOrShort(t *testing.T) {
	if MinPasswordLen != 8 {
		t.Fatalf("MinPasswordLen = %d, want 8", MinPasswordLen)
	}

	tests := []struct {
		name     string
		password string
		wantErr  error
	}{
		{name: "empty", password: "", wantErr: ErrPasswordEmpty},
		{name: "seven characters", password: "1234567", wantErr: ErrPasswordTooShort},
		{name: "seven Unicode characters", password: strings.Repeat("密", 7), wantErr: ErrPasswordTooShort},
		{name: "too long", password: strings.Repeat("a", MaxPasswordLen+1), wantErr: ErrPasswordTooLong},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := HashPassword(tt.password)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("HashPassword() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestHashPassword_acceptsEightUnicodeCharacters(t *testing.T) {
	password := strings.Repeat("密", MinPasswordLen)
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if !CheckPassword(hash, password) {
		t.Fatal("CheckPassword() = false, want true")
	}
}

func TestSessionSigner_signAndVerify(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	signer := NewSessionSignerWithClock([]byte(validSessionSecret), time.Hour, func() time.Time {
		return now
	})

	cookie, err := signer.SignUserID("user-123")
	if err != nil {
		t.Fatalf("SignUserID() error = %v", err)
	}
	if len(cookie) == 0 {
		t.Fatal("SignUserID() returned empty cookie")
	}
	wantPrefix := "v1." + strconv.FormatInt(now.UnixNano(), 10) + "."
	if !strings.HasPrefix(cookie, wantPrefix) {
		t.Fatalf("SignUserID() = %q, want prefix %q", cookie, wantPrefix)
	}

	userID, err := signer.VerifyUserID(cookie)
	if err != nil {
		t.Fatalf("VerifyUserID() error = %v", err)
	}
	if userID != "user-123" {
		t.Fatalf("VerifyUserID() = %q, want %q", userID, "user-123")
	}
}

func TestSessionSigner_tamperedCookie(t *testing.T) {
	signer := NewSessionSigner([]byte(validSessionSecret))

	cookie, err := signer.SignUserID("user-456")
	if err != nil {
		t.Fatalf("SignUserID() error = %v", err)
	}

	replacement := byte('x')
	if cookie[len(cookie)-1] == replacement {
		replacement = 'y'
	}
	tampered := cookie[:len(cookie)-1] + string(replacement)
	_, err = signer.VerifyUserID(tampered)
	if !errors.Is(err, ErrInvalidSessionSignature) {
		t.Fatalf("VerifyUserID() error = %v, want ErrInvalidSessionSignature", err)
	}
}

func TestSessionSignerRejectsLegacyFormat(t *testing.T) {
	signer := NewSessionSigner([]byte(validSessionSecret))
	_, err := signer.VerifyUserID("dXNlci0xMjM.invalid-signature")
	if !errors.Is(err, ErrMalformedSession) {
		t.Fatalf("VerifyUserID() error = %v, want ErrMalformedSession", err)
	}
}

func TestSessionSignerRejectsExpiredAndFutureSessions(t *testing.T) {
	base := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	now := base
	signer := NewSessionSignerWithClock([]byte(validSessionSecret), time.Hour, func() time.Time {
		return now
	})

	expiringCookie, err := signer.SignUserID("user-expired")
	if err != nil {
		t.Fatalf("SignUserID() error = %v", err)
	}
	now = base.Add(time.Hour)
	if _, err := signer.VerifyUserID(expiringCookie); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("VerifyUserID() error = %v, want ErrSessionExpired", err)
	}

	now = base.Add(maxSessionFutureSkew + time.Second)
	futureCookie, err := signer.SignUserID("user-future")
	if err != nil {
		t.Fatalf("SignUserID() future error = %v", err)
	}
	now = base
	if _, err := signer.VerifyUserID(futureCookie); !errors.Is(err, ErrSessionIssuedInFuture) {
		t.Fatalf("VerifyUserID() error = %v, want ErrSessionIssuedInFuture", err)
	}
}

func TestSessionSignerAllowsSmallClockSkew(t *testing.T) {
	base := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	now := base.Add(maxSessionFutureSkew)
	signer := NewSessionSignerWithClock([]byte(validSessionSecret), time.Hour, func() time.Time {
		return now
	})
	cookie, err := signer.SignUserID("user-skew")
	if err != nil {
		t.Fatalf("SignUserID() error = %v", err)
	}

	now = base
	if _, err := signer.VerifyUserID(cookie); err != nil {
		t.Fatalf("VerifyUserID() error = %v, want small future skew accepted", err)
	}
}

func TestSessionSignerPreservesSubsecondTTL(t *testing.T) {
	base := time.Date(2026, time.July, 24, 12, 0, 0, 900_000_000, time.UTC)
	now := base
	signer := NewSessionSignerWithClock([]byte(validSessionSecret), 500*time.Millisecond, func() time.Time {
		return now
	})
	cookie, err := signer.SignUserID("user-subsecond")
	if err != nil {
		t.Fatalf("SignUserID() error = %v", err)
	}
	if _, err := signer.VerifyUserID(cookie); err != nil {
		t.Fatalf("VerifyUserID() immediately after signing error = %v", err)
	}

	now = base.Add(500 * time.Millisecond)
	if _, err := signer.VerifyUserID(cookie); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("VerifyUserID() error = %v, want ErrSessionExpired at exact TTL", err)
	}
}

func TestSessionSignerTimestampIsSigned(t *testing.T) {
	signer := NewSessionSigner([]byte(validSessionSecret))
	cookie, err := signer.SignUserID("user-123")
	if err != nil {
		t.Fatalf("SignUserID() error = %v", err)
	}
	parts := strings.Split(cookie, ".")
	if len(parts) != 4 {
		t.Fatalf("cookie has %d parts, want 4", len(parts))
	}
	parts[1] = "1"

	if _, err := signer.VerifyUserID(strings.Join(parts, ".")); !errors.Is(err, ErrInvalidSessionSignature) {
		t.Fatalf("VerifyUserID() error = %v, want ErrInvalidSessionSignature", err)
	}
}

func TestSessionSignerDefaults(t *testing.T) {
	signer := NewSessionSigner([]byte(validSessionSecret))
	if signer.TTL() != DefaultSessionTTL {
		t.Fatalf("TTL() = %v, want %v", signer.TTL(), DefaultSessionTTL)
	}
}

func TestSessionSignerInvalidConfigurationPanics(t *testing.T) {
	tests := []struct {
		name string
		run  func()
	}{
		{
			name: "short secret",
			run: func() {
				NewSessionSigner([]byte(strings.Repeat("x", MinSessionSecretLen-1)))
			},
		},
		{
			name: "non-positive TTL",
			run: func() {
				NewSessionSignerWithTTL([]byte(validSessionSecret), 0)
			},
		},
		{
			name: "nil clock",
			run: func() {
				NewSessionSignerWithClock([]byte(validSessionSecret), time.Hour, nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("constructor expected panic")
				}
			}()
			tt.run()
		})
	}
}

func TestSessionSignerCopiesSecret(t *testing.T) {
	secret := []byte(validSessionSecret)
	signer := NewSessionSigner(secret)
	secret[0] ^= 0xff

	cookie, err := signer.SignUserID("user-123")
	if err != nil {
		t.Fatalf("SignUserID() error = %v", err)
	}
	if _, err := signer.VerifyUserID(cookie); err != nil {
		t.Fatalf("VerifyUserID() error after caller mutated secret = %v", err)
	}
}

func TestSessionSignerRejectsEmptyUserID(t *testing.T) {
	signer := NewSessionSigner([]byte(validSessionSecret))
	if _, err := signer.SignUserID(""); err == nil {
		t.Fatal("SignUserID() expected error for empty user ID")
	}
}

func TestSessionSignerRejectsMalformedVersion(t *testing.T) {
	signer := NewSessionSigner([]byte(validSessionSecret))
	cookie, err := signer.SignUserID("user-123")
	if err != nil {
		t.Fatalf("SignUserID() error = %v", err)
	}
	parts := strings.Split(cookie, ".")
	parts[0] = "v2"
	if _, err := signer.VerifyUserID(strings.Join(parts, ".")); !errors.Is(err, ErrMalformedSession) {
		t.Fatalf("VerifyUserID() error = %v, want ErrMalformedSession", err)
	}
}

func TestSessionSignerRejectsInvalidIssuedAt(t *testing.T) {
	signer := NewSessionSigner([]byte(validSessionSecret))
	tests := []string{
		"v1.not-a-time.dXNlci0xMjM.invalid",
		"v1.1..invalid",
	}
	for _, cookie := range tests {
		if _, err := signer.VerifyUserID(cookie); !errors.Is(err, ErrMalformedSession) {
			t.Fatalf("VerifyUserID(%q) error = %v, want ErrMalformedSession", cookie, err)
		}
	}
}
