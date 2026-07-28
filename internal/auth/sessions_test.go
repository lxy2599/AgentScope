package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"sort"
	"testing"
	"time"
)

type memorySessionRepository struct {
	sessions map[string]Session
	hashes   map[string][]byte
}

func newMemorySessionRepository() *memorySessionRepository {
	return &memorySessionRepository{sessions: map[string]Session{}, hashes: map[string][]byte{}}
}

func (r *memorySessionRepository) CreateSession(_ context.Context, session Session, hash []byte) error {
	r.sessions[session.ID] = session
	r.hashes[session.ID] = append([]byte(nil), hash...)
	return nil
}
func (r *memorySessionRepository) GetSession(_ context.Context, id string) (Session, []byte, error) {
	session, ok := r.sessions[id]
	if !ok {
		return Session{}, nil, ErrSessionNotFound
	}
	return session, append([]byte(nil), r.hashes[id]...), nil
}
func (r *memorySessionRepository) TouchSession(_ context.Context, id string, at time.Time) error {
	session, ok := r.sessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	session.LastUsedAt = &at
	r.sessions[id] = session
	return nil
}
func (r *memorySessionRepository) RevokeSession(_ context.Context, id, userID, reason string, at time.Time) error {
	session, ok := r.sessions[id]
	if !ok || session.UserID != userID || session.RevokedAt != nil {
		return ErrSessionNotFound
	}
	session.RevokedAt = &at
	session.RevokedReason = reason
	r.sessions[id] = session
	return nil
}
func (r *memorySessionRepository) RevokeAllUserSessions(_ context.Context, userID, except, reason string, at time.Time) error {
	for id, session := range r.sessions {
		if session.UserID != userID || id == except || session.RevokedAt != nil {
			continue
		}
		session.RevokedAt = &at
		session.RevokedReason = reason
		r.sessions[id] = session
	}
	return nil
}
func (r *memorySessionRepository) ListUserSessions(_ context.Context, userID string) ([]Session, error) {
	out := make([]Session, 0)
	for _, session := range r.sessions {
		if session.UserID == userID {
			out = append(out, session)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func newSessionServiceForTest(now time.Time) (*SessionService, *memorySessionRepository) {
	repo := newMemorySessionRepository()
	signer := NewSessionSignerWithClock([]byte("0123456789abcdef0123456789abcdef"), time.Hour, func() time.Time { return now })
	service := NewSessionService(repo, signer)
	service.now = func() time.Time { return now }
	return service, repo
}

func TestSessionServiceCreateAuthenticateAndRevoke(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	service, repo := newSessionServiceForTest(now)
	cookie, created, err := service.Create(context.Background(), "user-a", "  Mozilla/5.0  ", "203.0.113.0/24")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(repo.hashes[created.ID]) != sha256.Size {
		t.Fatalf("stored hash length = %d, want %d", len(repo.hashes[created.ID]), sha256.Size)
	}
	if string(repo.hashes[created.ID]) == cookie || string(repo.hashes[created.ID]) == "" {
		t.Fatal("repository stored recoverable cookie or empty hash")
	}
	if created.UserAgentSummary != "Mozilla/5.0" {
		t.Fatalf("user agent = %q", created.UserAgentSummary)
	}
	if cookie[:3] != "v2." {
		t.Fatalf("cookie = %q, want v2", cookie)
	}

	authenticated, err := service.Authenticate(context.Background(), cookie)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if authenticated.ID != created.ID || authenticated.UserID != "user-a" {
		t.Fatalf("unexpected authenticated session: %#v", authenticated)
	}
	if repo.sessions[created.ID].LastUsedAt == nil {
		t.Fatal("authenticate did not update last_used_at")
	}

	if err := service.RevokeCurrent(context.Background(), created.ID, "user-a", "logout"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := service.Authenticate(context.Background(), cookie); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("authenticate revoked session error = %v, want ErrSessionRevoked", err)
	}
}

func TestSessionServiceRejectsForgedExpiredAndBulkRevokedSessions(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	service, _ := newSessionServiceForTest(now)
	first, firstSession, err := service.Create(context.Background(), "user-a", "", "")
	if err != nil {
		t.Fatal(err)
	}
	second, secondSession, err := service.Create(context.Background(), "user-a", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), first+"tampered"); err == nil {
		t.Fatal("forged cookie was accepted")
	}
	if err := service.RevokeAll(context.Background(), "user-a", firstSession.ID, "logout_all"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), second); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("bulk-revoked session error = %v", err)
	}
	if _, err := service.Authenticate(context.Background(), first); err != nil {
		t.Fatalf("excepted session should remain valid: %v", err)
	}

	service.now = func() time.Time { return now.Add(time.Hour) }
	if _, err := service.Authenticate(context.Background(), first); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expired session error = %v", err)
	}
	_ = secondSession
}

func TestSessionServiceTruncatesUserAgent(t *testing.T) {
	service, _ := newSessionServiceForTest(time.Now().UTC())
	_, session, err := service.Create(context.Background(), "user-a", string(make([]byte, 200)), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(session.UserAgentSummary) != 180 {
		t.Fatalf("user agent length = %d, want 180", len(session.UserAgentSummary))
	}
}
