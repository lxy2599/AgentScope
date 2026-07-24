package adapter

import (
	"context"
	"errors"
	"strings"

	"AgentScope/internal/projects"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProjectQuerier implements projects.Querier using store.Queries with
// PostgreSQL transaction support.
type ProjectQuerier struct {
	q    *store.Queries
	pool *pgxpool.Pool
}

// compile-time checks
var _ interface {
	CreateProjectWithOwner(ctx context.Context, userID, name, description string) (projects.Project, error)
	AddProjectMember(ctx context.Context, projectID, userID, role string) error
	ListProjectsByUser(ctx context.Context, userID string) ([]projects.Project, error)
	CreateProjectToken(ctx context.Context, projectID, tokenHash, tokenPrefix, scope, description string) (projects.ProjectToken, error)
	GetTokenByHash(ctx context.Context, tokenHash []byte) (projects.ProjectToken, error)
	GetProjectMember(ctx context.Context, projectID, userID string) (projects.ProjectMember, error)
} = (*ProjectQuerier)(nil)

// NewProjectQuerier creates a new ProjectQuerier backed by a connection pool.
func NewProjectQuerier(q *store.Queries, pool *pgxpool.Pool) *ProjectQuerier {
	return &ProjectQuerier{q: q, pool: pool}
}

// CreateProjectWithOwner creates a project and adds the user as owner in a
// single PostgreSQL transaction. If either operation fails, both are rolled back.
func (p *ProjectQuerier) CreateProjectWithOwner(ctx context.Context, userID, name, description string) (projects.Project, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return projects.Project{}, err
	}
	defer tx.Rollback(ctx) // no-op after commit

	tq := p.q.WithTx(tx)

	storeProj, err := tq.CreateProject(ctx, store.CreateProjectParams{
		Name:        name,
		Description: pgtype.Text{String: description, Valid: description != ""},
	})
	if err != nil {
		return projects.Project{}, err
	}

	_, err = tq.AddProjectMember(ctx, store.AddProjectMemberParams{
		ProjectID: storeProj.ID,
		UserID:    parsePGUUID(userID),
		Role:      "owner",
	})
	if err != nil {
		return projects.Project{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return projects.Project{}, err
	}

	return projects.Project{
		ID:          UUIDToString(storeProj.ID.Bytes),
		Name:        storeProj.Name,
		Description: strOrZero(storeProj.Description),
		CreatedAt:   storeProj.CreatedAt.Time,
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
			ID:          UUIDToString(r.ID.Bytes),
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
		ID:          UUIDToString(tok.ID.Bytes),
		ProjectID:   UUIDToString(tok.ProjectID.Bytes),
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
		ID:          UUIDToString(tok.ID.Bytes),
		ProjectID:   UUIDToString(tok.ProjectID.Bytes),
		TokenHash:   string(tok.TokenHash),
		TokenPrefix: tok.TokenPrefix,
		Scopes:      tok.Scope,
		ScopeList:   strings.Fields(tok.Scope),
		CreatedAt:   tok.CreatedAt.Time,
	}, nil
}

// GetProjectMember returns the member role or an error if not found.
func (p *ProjectQuerier) GetProjectMember(ctx context.Context, projectID, userID string) (projects.ProjectMember, error) {
	member, err := p.q.GetProjectMember(ctx, store.GetProjectMemberParams{
		ProjectID: parsePGUUID(projectID),
		UserID:    parsePGUUID(userID),
	})
	if err != nil {
		return projects.ProjectMember{}, errors.New("not a project member")
	}
	return projects.ProjectMember{
		ProjectID: UUIDToString(member.ProjectID.Bytes),
		UserID:    UUIDToString(member.UserID.Bytes),
		Role:      member.Role,
	}, nil
}
