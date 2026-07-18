package adapter

import (
	"context"
	"errors"

	"AgentScope/internal/auth"
	"AgentScope/internal/store"
)

// AuthService implements httpapi.AuthService using store.Queries.
type AuthService struct {
	q *store.Queries
}

// compile-time interface check
var _ interface {
	Register(ctx context.Context, email, password, displayName string) (string, error)
	Login(ctx context.Context, email, password string) (string, string, error)
} = (*AuthService)(nil)

// NewAuthService creates a new AuthService adapter.
func NewAuthService(q *store.Queries) *AuthService {
	return &AuthService{q: q}
}

// Register creates a user with bcrypt-hashed password.
func (s *AuthService) Register(ctx context.Context, email, password, displayName string) (string, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", err
	}

	user, err := s.q.CreateUser(ctx, store.CreateUserParams{
		Email:        email,
		PasswordHash: hash,
		DisplayName:  displayName,
	})
	if err != nil {
		return "", errors.New("email already registered")
	}

	return UUIDToString(user.ID.Bytes), nil
}

// Login verifies credentials and returns (userID, displayName, error).
func (s *AuthService) Login(ctx context.Context, email, password string) (string, string, error) {
	user, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		return "", "", errors.New("invalid email or password")
	}

	if !auth.CheckPassword(user.PasswordHash, password) {
		return "", "", errors.New("invalid email or password")
	}

	return UUIDToString(user.ID.Bytes), user.DisplayName, nil
}
