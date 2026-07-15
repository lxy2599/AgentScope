package adapter

import (
	"context"
	"errors"
	"strings"

	"AgentScope/internal/projects"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// ProjectQuerier implements projects.Querier using store.Queries.
type ProjectQuerier struct {
	q *store.Queries
}

// compile-time check
var _ interface {
	CreateProject(ctx context.Context, name, description string) (projects.Project, error)
	AddProjectMember(ctx context.Context, projectID, userID, role string) error
	ListProjectsByUser(ctx context.Context, userID string) ([]projects.Project, error)
	CreateProjectToken(ctx context.Context, projectID, tokenHash, tokenPrefix, scope, description string) (projects.ProjectToken, error)
	GetTokenByHash(ctx context.Context, tokenHash []byte) (projects.ProjectToken, error)
	GetProjectMember(ctx context.Context, projectID, userID string) (interface{}, error)
} = (*ProjectQuerier)(nil)

// NewProjectQuerier creates a new ProjectQuerier.
func NewProjectQuerier(q *store.Queries) *ProjectQuerier {
	return &ProjectQuerier{q: q}
}

func (p *ProjectQuerier) CreateProject(ctx context.Context, name, description string) (projects.Project, error) {
	proj, err := p.q.CreateProject(ctx, store.CreateProjectParams{
		Name:        name,
		Description: pgtype.Text{String: description, Valid: description != ""},
	})
	if err != nil {
		return projects.Project{}, err
	}
	return projects.Project{
		ID:          uuidToString(proj.ID.Bytes),
		Name:        proj.Name,
		Description: strOrZero(proj.Description),
		CreatedAt:   proj.CreatedAt.Time,
	}, nil
}

func (p *ProjectQuerier) AddProjectMember(ctx context.Context, projectID, userID, role string) error {
	_, err := p.q.AddProjectMember(ctx, store.AddProjectMemberParams{
		ProjectID: parsePGUUID(projectID),
		UserID:    parsePGUUID(userID),
		Role:      role,
	})
	return err
}

func (p *ProjectQuerier) ListProjectsByUser(ctx context.Context, userID string) ([]projects.Project, error) {
	rows, err := p.q.ListProjectsByUser(ctx, parsePGUUID(userID))
	if err != nil {
		return nil, err
	}
	result := make([]projects.Project, 0, len(rows))
	for _, r := range rows {
		result = append(result, projects.Project{
			ID:          uuidToString(r.ID.Bytes),
			Name:        r.Name,
			Description: strOrZero(r.Description),
			CreatedAt:   r.CreatedAt.Time,
		})
	}
	return result, nil
}

func (p *ProjectQuerier) CreateProjectToken(ctx context.Context, projectID, tokenHash, tokenPrefix, scope, description string) (projects.ProjectToken, error) {
	tok, err := p.q.CreateProjectToken(ctx, store.CreateProjectTokenParams{
		ProjectID:   parsePGUUID(projectID),
		TokenHash:   []byte(tokenHash),
		TokenPrefix: tokenPrefix,
		Scope:       scope,
		Description: pgtype.Text{String: description, Valid: description != ""},
	})
	if err != nil {
		return projects.ProjectToken{}, err
	}
	return projects.ProjectToken{
		ID:          uuidToString(tok.ID.Bytes),
		ProjectID:   uuidToString(tok.ProjectID.Bytes),
		TokenHash:   string(tok.TokenHash),
		TokenPrefix: tok.TokenPrefix,
		Scopes:      tok.Scope,
		ScopeList:   strings.Fields(tok.Scope),
		CreatedAt:   tok.CreatedAt.Time,
	}, nil
}

func (p *ProjectQuerier) GetTokenByHash(ctx context.Context, tokenHash []byte) (projects.ProjectToken, error) {
	tok, err := p.q.GetTokenByHash(ctx, tokenHash)
	if err != nil {
		return projects.ProjectToken{}, err
	}
	return projects.ProjectToken{
		ID:          uuidToString(tok.ID.Bytes),
		ProjectID:   uuidToString(tok.ProjectID.Bytes),
		TokenHash:   string(tok.TokenHash),
		TokenPrefix: tok.TokenPrefix,
		Scopes:      tok.Scope,
		ScopeList:   strings.Fields(tok.Scope),
		CreatedAt:   tok.CreatedAt.Time,
	}, nil
}

// GetProjectMember returns the member role or an error if not found.
func (p *ProjectQuerier) GetProjectMember(ctx context.Context, projectID, userID string) (interface{}, error) {
	member, err := p.q.GetProjectMember(ctx, store.GetProjectMemberParams{
		ProjectID: parsePGUUID(projectID),
		UserID:    parsePGUUID(userID),
	})
	if err != nil {
		return nil, errors.New("not a project member")
	}
	return member, nil
}
