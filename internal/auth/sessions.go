package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionRevoked  = errors.New("session revoked")
)

// Session is safe to return to its owning user; it deliberately excludes the session secret and hash.
type Session struct {
	ID, UserID, RevokedReason, UserAgentSummary, IPPrefix string
	CreatedAt, ExpiresAt                                  time.Time
	LastUsedAt, RevokedAt                                 *time.Time
}

type SessionRepository interface {
	CreateSession(ctx context.Context, session Session, secretHash []byte) error
	GetSession(ctx context.Context, sessionID string) (Session, []byte, error)
	TouchSession(ctx context.Context, sessionID string, usedAt time.Time) error
	RevokeSession(ctx context.Context, sessionID, userID, reason string, at time.Time) error
	RevokeAllUserSessions(ctx context.Context, userID, exceptSessionID, reason string, at time.Time) error
	ListUserSessions(ctx context.Context, userID string) ([]Session, error)
}

type SessionService struct {
	repo   SessionRepository
	signer SessionSigner
	now    func() time.Time
}

func NewSessionService(repo SessionRepository, signer SessionSigner) *SessionService {
	return &SessionService{repo: repo, signer: signer, now: func() time.Time { return time.Now().UTC() }}
}

func (s *SessionService) Create(ctx context.Context, userID, userAgent, ipPrefix string) (string, Session, error) {
	if userID == "" {
		return "", Session{}, errors.New("user ID is required")
	}
	secret, err := GenerateRandomBytes(32)
	if err != nil {
		return "", Session{}, fmt.Errorf("generate session secret: %w", err)
	}
	now := s.now()
	session := Session{ID: uuid.NewString(), UserID: userID, CreatedAt: now, ExpiresAt: now.Add(s.signer.TTL()), UserAgentSummary: summarizeUserAgent(userAgent), IPPrefix: ipPrefix}
	hash := sha256.Sum256(secret)
	if err := s.repo.CreateSession(ctx, session, hash[:]); err != nil {
		return "", Session{}, err
	}
	cookie, err := s.signer.SignSessionPayload(session.ID + "." + hex.EncodeToString(secret))
	if err != nil {
		return "", Session{}, err
	}
	return cookie, session, nil
}

func (s *SessionService) Authenticate(ctx context.Context, cookie string) (Session, error) {
	payload, err := s.signer.VerifySessionPayload(cookie)
	if err != nil {
		return Session{}, err
	}
	parts := strings.Split(payload, ".")
	if len(parts) != 2 || parts[0] == "" {
		return Session{}, ErrMalformedSession
	}
	secret, err := hex.DecodeString(parts[1])
	if err != nil || len(secret) != 32 {
		return Session{}, ErrMalformedSession
	}
	session, storedHash, err := s.repo.GetSession(ctx, parts[0])
	if err != nil {
		return Session{}, err
	}
	hash := sha256.Sum256(secret)
	if !equalHash(storedHash, hash[:]) {
		return Session{}, ErrInvalidSessionSignature
	}
	now := s.now()
	if session.RevokedAt != nil {
		return Session{}, ErrSessionRevoked
	}
	if !session.ExpiresAt.After(now) {
		return Session{}, ErrSessionExpired
	}
	_ = s.repo.TouchSession(context.WithoutCancel(ctx), session.ID, now)
	return session, nil
}

func (s *SessionService) RevokeCurrent(ctx context.Context, sessionID, userID, reason string) error {
	return s.repo.RevokeSession(ctx, sessionID, userID, reason, s.now())
}
func (s *SessionService) RevokeAll(ctx context.Context, userID, exceptSessionID, reason string) error {
	return s.repo.RevokeAllUserSessions(ctx, userID, exceptSessionID, reason, s.now())
}
func (s *SessionService) List(ctx context.Context, userID string) ([]Session, error) {
	return s.repo.ListUserSessions(ctx, userID)
}

func equalHash(a, b []byte) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare(a, b) == 1
}
func summarizeUserAgent(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 180 {
		return value[:180]
	}
	return value
}
