package adapter

import (
	"context"
	"errors"
	"fmt"

	"AgentScope/internal/auth"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// dummyPasswordHash is a valid cost-12 bcrypt hash used to keep the
// nonexistent-user login path comparable to the wrong-password path.
var dummyPasswordHash = []byte("$2b$12$JjTuEXFOxKusBmucjQixouN.WYs6keJ6Me8YwwDVj47p5mt3uwG1q")

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
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", auth.ErrEmailAlreadyRegistered
		}
		return "", fmt.Errorf("create user: %w", err)
	}

	return UUIDToString(user.ID.Bytes), nil
}

// Login verifies credentials and returns (userID, displayName, error).
func (s *AuthService) Login(ctx context.Context, email, password string) (string, string, error) {
	user, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			auth.CheckPassword(dummyPasswordHash, password)
			return "", "", auth.ErrInvalidCredentials
		}
		return "", "", fmt.Errorf("get user by email: %w", err)
	}

	if !auth.CheckPassword(user.PasswordHash, password) {
		return "", "", auth.ErrInvalidCredentials
	}

	return UUIDToString(user.ID.Bytes), user.DisplayName, nil
}
