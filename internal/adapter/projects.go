package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"AgentScope/internal/projects"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5"
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
	CreateProjectTokenWithMetadata(ctx context.Context, projectID, tokenHash, tokenPrefix, scope, description, createdBy string, expiresAt *time.Time, environment, tokenType, rotatesTokenID string) (projects.ProjectToken, error)
	GetTokenByHash(ctx context.Context, tokenHash []byte) (projects.ProjectToken, error)
	ListProjectTokens(ctx context.Context, projectID string) ([]projects.ProjectToken, error)
	RevokeProjectToken(ctx context.Context, projectID, tokenID, revokedBy, reason string) (projects.ProjectToken, error)
	TouchProjectToken(ctx context.Context, tokenID, endpoint, agentID, instanceID string, usedAt time.Time) error
	CreateTokenAuditEvent(ctx context.Context, event projects.TokenAuditEvent) error
	ListTokenAuditEvents(ctx context.Context, projectID, tokenID string) ([]projects.TokenAuditEvent, error)
	GetTokenPolicy(ctx context.Context, projectID string) (projects.TokenPolicy, error)
	UpsertTokenPolicy(ctx context.Context, policy projects.TokenPolicy) (projects.TokenPolicy, error)
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
	row := p.pool.QueryRow(ctx, `SELECT id, project_id, token_hash, token_prefix, scope, description, created_at, expires_at, last_used_at, created_by, revoked_at, revoked_by, revocation_reason, rotates_token_id, environment, token_type, last_used_endpoint, last_used_agent_id, last_used_instance_id FROM project_tokens WHERE token_hash=$1`, tokenHash)
	token, err := scanProjectToken(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return projects.ProjectToken{}, projects.ErrTokenNotFound
	}
	return token, err
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

// The methods below intentionally live in the persistence adapter rather than HTTP handlers.
// Token metadata and audit events are PostgreSQL business facts.
func (p *ProjectQuerier) CreateProjectTokenWithMetadata(ctx context.Context, projectID, tokenHash, tokenPrefix, scope, description, createdBy string, expiresAt *time.Time, environment, tokenType, rotatesTokenID string) (projects.ProjectToken, error) {
	row := p.pool.QueryRow(ctx, `INSERT INTO project_tokens (project_id, token_hash, token_prefix, scope, description, created_by, expires_at, environment, token_type, rotates_token_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, project_id, token_hash, token_prefix, scope, description, created_at, expires_at, last_used_at, created_by, revoked_at, revoked_by, revocation_reason, rotates_token_id, environment, token_type, last_used_endpoint, last_used_agent_id, last_used_instance_id`,
		parsePGUUID(projectID), []byte(tokenHash), tokenPrefix, scope, pgtype.Text{String: description, Valid: description != ""}, parsePGUUID(createdBy), timestamptz(expiresAt), environment, tokenType, nullableUUID(rotatesTokenID))
	return scanProjectToken(row)
}

func (p *ProjectQuerier) ListProjectTokens(ctx context.Context, projectID string) ([]projects.ProjectToken, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, project_id, token_hash, token_prefix, scope, description, created_at, expires_at, last_used_at, created_by, revoked_at, revoked_by, revocation_reason, rotates_token_id, environment, token_type, last_used_endpoint, last_used_agent_id, last_used_instance_id FROM project_tokens WHERE project_id=$1 ORDER BY created_at DESC`, parsePGUUID(projectID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []projects.ProjectToken{}
	for rows.Next() {
		token, err := scanProjectToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, token)
	}
	return out, rows.Err()
}
func (p *ProjectQuerier) RevokeProjectToken(ctx context.Context, projectID, tokenID, revokedBy, reason string) (projects.ProjectToken, error) {
	row := p.pool.QueryRow(ctx, `UPDATE project_tokens SET revoked_at=now(), revoked_by=$3, revocation_reason=$4 WHERE id=$1 AND project_id=$2 AND revoked_at IS NULL RETURNING id, project_id, token_hash, token_prefix, scope, description, created_at, expires_at, last_used_at, created_by, revoked_at, revoked_by, revocation_reason, rotates_token_id, environment, token_type, last_used_endpoint, last_used_agent_id, last_used_instance_id`, nullableUUID(tokenID), parsePGUUID(projectID), parsePGUUID(revokedBy), pgtype.Text{String: reason, Valid: reason != ""})
	return scanProjectToken(row)
}
func (p *ProjectQuerier) TouchProjectToken(ctx context.Context, tokenID, endpoint, agentID, instanceID string, usedAt time.Time) error {
	_, err := p.pool.Exec(ctx, `UPDATE project_tokens SET last_used_at=$2, last_used_endpoint=$3, last_used_agent_id=$4, last_used_instance_id=$5 WHERE id=$1`, nullableUUID(tokenID), usedAt, pgtype.Text{String: endpoint, Valid: endpoint != ""}, nullableUUID(agentID), nullableUUID(instanceID))
	return err
}
func (p *ProjectQuerier) CreateTokenAuditEvent(ctx context.Context, event projects.TokenAuditEvent) error {
	metadata, _ := json.Marshal(event.Metadata)
	_, err := p.pool.Exec(ctx, `INSERT INTO project_token_audit_events (project_id, token_id, actor_user_id, event_type, metadata, created_at) VALUES ($1,$2,$3,$4,$5,$6)`, parsePGUUID(event.ProjectID), nullableUUID(event.TokenID), nullableUUID(event.ActorUserID), event.EventType, metadata, event.CreatedAt)
	return err
}
func (p *ProjectQuerier) ListTokenAuditEvents(ctx context.Context, projectID, tokenID string) ([]projects.TokenAuditEvent, error) {
	rows, err := p.pool.Query(ctx, `SELECT id,project_id,token_id,actor_user_id,event_type,metadata,created_at FROM project_token_audit_events WHERE project_id=$1 AND token_id=$2 ORDER BY created_at DESC`, parsePGUUID(projectID), nullableUUID(tokenID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []projects.TokenAuditEvent{}
	for rows.Next() {
		var id, pid, tid, actor pgtype.UUID
		var typ string
		var metadata []byte
		var created pgtype.Timestamptz
		if err := rows.Scan(&id, &pid, &tid, &actor, &typ, &metadata, &created); err != nil {
			return nil, err
		}
		var m map[string]any
		_ = json.Unmarshal(metadata, &m)
		out = append(out, projects.TokenAuditEvent{ID: UUIDToString(id.Bytes), ProjectID: UUIDToString(pid.Bytes), TokenID: UUIDToString(tid.Bytes), ActorUserID: uuidText(actor), EventType: typ, Metadata: m, CreatedAt: created.Time})
	}
	return out, rows.Err()
}
func (p *ProjectQuerier) GetTokenPolicy(ctx context.Context, projectID string) (projects.TokenPolicy, error) {
	row := p.pool.QueryRow(ctx, `SELECT project_id,allow_non_expiring,max_production_ttl_days,max_ci_ttl_days,expiry_warning_days,unused_warning_days,updated_by,updated_at FROM project_token_policies WHERE project_id=$1`, parsePGUUID(projectID))
	policy, err := scanTokenPolicy(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return projects.TokenPolicy{}, projects.ErrTokenNotFound
	}
	return policy, err
}
func (p *ProjectQuerier) UpsertTokenPolicy(ctx context.Context, policy projects.TokenPolicy) (projects.TokenPolicy, error) {
	row := p.pool.QueryRow(ctx, `INSERT INTO project_token_policies (project_id,allow_non_expiring,max_production_ttl_days,max_ci_ttl_days,expiry_warning_days,unused_warning_days,updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (project_id) DO UPDATE SET allow_non_expiring=excluded.allow_non_expiring,max_production_ttl_days=excluded.max_production_ttl_days,max_ci_ttl_days=excluded.max_ci_ttl_days,expiry_warning_days=excluded.expiry_warning_days,unused_warning_days=excluded.unused_warning_days,updated_by=excluded.updated_by,updated_at=now() RETURNING project_id,allow_non_expiring,max_production_ttl_days,max_ci_ttl_days,expiry_warning_days,unused_warning_days,updated_by,updated_at`, parsePGUUID(policy.ProjectID), policy.AllowNonExpiring, nullableInt(policy.MaxProductionTTLDays), nullableInt(policy.MaxCITTLDays), policy.ExpiryWarningDays, policy.UnusedWarningDays, nullableUUID(policy.UpdatedBy))
	return scanTokenPolicy(row)
}

type scanner interface{ Scan(...any) error }

func scanProjectToken(row scanner) (projects.ProjectToken, error) {
	var id, pid, createdBy, revokedBy, rotates, agent, instance pgtype.UUID
	var hash []byte
	var prefix, scope, env, typ string
	var desc, reason, endpoint pgtype.Text
	var created, expires, lastUsed, revoked pgtype.Timestamptz
	if err := row.Scan(&id, &pid, &hash, &prefix, &scope, &desc, &created, &expires, &lastUsed, &createdBy, &revoked, &revokedBy, &reason, &rotates, &env, &typ, &endpoint, &agent, &instance); err != nil {
		return projects.ProjectToken{}, err
	}
	return projects.ProjectToken{ID: UUIDToString(id.Bytes), ProjectID: UUIDToString(pid.Bytes), TokenHash: string(hash), TokenPrefix: prefix, Scopes: scope, ScopeList: strings.Fields(scope), Description: strOrZero(desc), CreatedAt: created.Time, ExpiresAt: timePtr(expires), LastUsedAt: timePtr(lastUsed), CreatedBy: uuidText(createdBy), RevokedAt: timePtr(revoked), RevokedBy: uuidText(revokedBy), RevocationReason: strOrZero(reason), RotatesTokenID: uuidText(rotates), Environment: env, TokenType: typ, LastUsedEndpoint: strOrZero(endpoint), LastUsedAgentID: uuidText(agent), LastUsedInstanceID: uuidText(instance)}, nil
}
func scanTokenPolicy(row scanner) (projects.TokenPolicy, error) {
	var pid, by pgtype.UUID
	var allow bool
	var prod, ci pgtype.Int4
	var warnings []int32
	var unused int32
	var updated pgtype.Timestamptz
	if err := row.Scan(&pid, &allow, &prod, &ci, &warnings, &unused, &by, &updated); err != nil {
		return projects.TokenPolicy{}, err
	}
	return projects.TokenPolicy{ProjectID: UUIDToString(pid.Bytes), AllowNonExpiring: allow, MaxProductionTTLDays: intPtr(prod), MaxCITTLDays: intPtr(ci), ExpiryWarningDays: int32s(warnings), UnusedWarningDays: int(unused), UpdatedBy: uuidText(by), UpdatedAt: updated.Time}, nil
}
func nullableUUID(s string) pgtype.UUID {
	if s == "" {
		return pgtype.UUID{}
	}
	return parsePGUUID(s)
}
func timestamptz(v *time.Time) pgtype.Timestamptz {
	if v == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *v, Valid: true}
}
func timePtr(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	x := v.Time
	return &x
}
func uuidText(v pgtype.UUID) string {
	if !v.Valid {
		return ""
	}
	return UUIDToString(v.Bytes)
}
func nullableInt(v *int) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(*v), Valid: true}
}
func intPtr(v pgtype.Int4) *int {
	if !v.Valid {
		return nil
	}
	x := int(v.Int32)
	return &x
}
func int32s(v []int32) []int {
	out := make([]int, len(v))
	for i, n := range v {
		out[i] = int(n)
	}
	return out
}
