package auth

import (
	"strings"
	"testing"
)

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
	_, err := HashPassword("")
	if err == nil {
		t.Fatal("HashPassword('') expected error")
	}
	_, err = HashPassword("ab")
	if err == nil {
		t.Fatal("HashPassword('ab') expected error (too short)")
	}
	_, err = HashPassword(strings.Repeat("a", 128))
	if err == nil {
		t.Fatal("HashPassword(128 chars) expected error (too long)")
	}
}

func TestSessionSigner_signAndVerify(t *testing.T) {
	signer := NewSessionSigner([]byte("my-secret-key-16b"))

	cookie, err := signer.SignUserID("user-123")
	if err != nil {
		t.Fatalf("SignUserID() error = %v", err)
	}
	if len(cookie) == 0 {
		t.Fatal("SignUserID() returned empty cookie")
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
	signer := NewSessionSigner([]byte("my-secret-key-16b"))

	cookie, err := signer.SignUserID("user-456")
	if err != nil {
		t.Fatalf("SignUserID() error = %v", err)
	}

	tampered := cookie[:len(cookie)-1] + "x"
	_, err = signer.VerifyUserID(tampered)
	if err == nil {
		t.Fatal("VerifyUserID() expected error for tampered cookie")
	}
}

func TestSessionSigner_shortSecret(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewSessionSigner with short secret expected panic")
		}
	}()
	NewSessionSigner([]byte("short"))
}
