package projects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"AgentScope/internal/auth"
)

// ---- Domain types ----

// Project is the service-level project representation.
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// ProjectToken is the service-level project token representation.
type ProjectToken struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	TokenHash   string    `json:"-"`
	TokenPrefix string    `json:"token_prefix"`
	Scopes      string    `json:"-"`
	ScopeList   []string  `json:"scopes"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// AuthenticatedToken is returned after successful token authentication.
type AuthenticatedToken struct {
	ProjectID string
	TokenID   string
	Scopes    []string
}

// ---- Querier interface ----

// Querier defines the database operations the projects service needs.
type Querier interface {
	CreateProjectWithOwner(ctx context.Context, userID, name, description string) (Project, error)
	AddProjectMember(ctx context.Context, projectID, userID, role string) error
	ListProjectsByUser(ctx context.Context, userID string) ([]Project, error)
	CreateProjectToken(ctx context.Context, projectID, tokenHash, tokenPrefix, scope, description string) (ProjectToken, error)
	GetTokenByHash(ctx context.Context, tokenHash []byte) (ProjectToken, error)
	GetProjectMember(ctx context.Context, projectID, userID string) (interface{}, error)
}

// ---- Service ----

// Service provides project and project-token operations.
type Service struct {
	q Querier
}

// NewService creates a new projects Service.
func NewService(q Querier) *Service {
	return &Service{q: q}
}

// CreateProject creates a new project and adds the creating user as an owner.
// Both operations are performed atomically via CreateProjectWithOwner.
func (s *Service) CreateProject(ctx context.Context, userID, name, description string) (Project, error) {
	if name == "" {
		return Project{}, errors.New("project name is required")
	}
	if userID == "" {
		return Project{}, errors.New("user ID is required")
	}

	return s.q.CreateProjectWithOwner(ctx, userID, name, description)
}

// ListProjects returns all projects the user is a member of.
func (s *Service) ListProjects(ctx context.Context, userID string) ([]Project, error) {
	if userID == "" {
		return nil, errors.New("user ID is required")
	}
	return s.q.ListProjectsByUser(ctx, userID)
}

const tokenPrefix = "ascope_"

// CreateToken generates a new project token. The userID is used to check
// project membership before creating the token.
func (s *Service) CreateToken(ctx context.Context, projectID, userID, description string, scopes []string) (plainToken string, token ProjectToken, err error) {
	if projectID == "" {
		return "", ProjectToken{}, errors.New("project ID is required")
	}
	if userID == "" {
		return "", ProjectToken{}, errors.New("user ID is required")
	}
	if len(scopes) == 0 {
		return "", ProjectToken{}, errors.New("at least one scope is required")
	}

	// Check user is a member of the project
	if _, err := s.q.GetProjectMember(ctx, projectID, userID); err != nil {
		return "", ProjectToken{}, errors.New("not a project member")
	}

	randomBytes, err := auth.GenerateRandomBytes(32)
	if err != nil {
		return "", ProjectToken{}, fmt.Errorf("generate token: %w", err)
	}

	randomPart := hex.EncodeToString(randomBytes)
	plainToken = tokenPrefix + randomPart

	prefix := tokenPrefix + randomPart[:12]

	hash := sha256.Sum256([]byte(plainToken))
	tokenHash := hex.EncodeToString(hash[:])

	scopeStr := strings.Join(scopes, " ")

	created, err := s.q.CreateProjectToken(ctx, projectID, tokenHash, prefix, scopeStr, description)
	if err != nil {
		return "", ProjectToken{}, fmt.Errorf("store token: %w", err)
	}

	return plainToken, created, nil
}

// AuthenticateToken validates a plain project token against a required scope.
func (s *Service) AuthenticateToken(ctx context.Context, plainToken, requiredScope string) (AuthenticatedToken, error) {
	if !strings.HasPrefix(plainToken, tokenPrefix) {
		return AuthenticatedToken{}, errors.New("invalid token format")
	}

	hash := sha256.Sum256([]byte(plainToken))
	tokenHash := hex.EncodeToString(hash[:])

	tok, err := s.q.GetTokenByHash(ctx, []byte(tokenHash))
	if err != nil {
		return AuthenticatedToken{}, errors.New("token not found or invalid")
	}

	tokenScopes := strings.Fields(tok.Scopes)
	scopeFound := false
	for _, s := range tokenScopes {
		if s == requiredScope {
			scopeFound = true
			break
		}
	}
	if !scopeFound {
		return AuthenticatedToken{}, fmt.Errorf("token does not have required scope: %s", requiredScope)
	}

	return AuthenticatedToken{
		ProjectID: tok.ProjectID,
		TokenID:   tok.ID,
		Scopes:    tokenScopes,
	}, nil
}
