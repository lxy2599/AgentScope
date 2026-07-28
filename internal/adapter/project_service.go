package adapter

import (
	"context"
	"fmt"
	"time"

	"AgentScope/internal/httpapi"
	"AgentScope/internal/projects"
)

// HTTPProjectsService implements httpapi.ProjectsService using *projects.Service.
type HTTPProjectsService struct {
	svc *projects.Service
}

// Compile-time check.
var _ httpapi.ProjectsService = (*HTTPProjectsService)(nil)

// NewHTTPProjectsService creates a new HTTPProjectsService adapter.
func NewHTTPProjectsService(svc *projects.Service) *HTTPProjectsService {
	return &HTTPProjectsService{svc: svc}
}

func (h *HTTPProjectsService) CreateProject(ctx context.Context, userID, name, description string) (httpapi.Project, error) {
	p, err := h.svc.CreateProject(ctx, userID, name, description)
	if err != nil {
		return httpapi.Project{}, err
	}
	return httpapi.Project{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		CreatedAt:   p.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}, nil
}

func (h *HTTPProjectsService) ListProjects(ctx context.Context, userID string) ([]httpapi.Project, error) {
	list, err := h.svc.ListProjects(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := make([]httpapi.Project, 0, len(list))
	for _, p := range list {
		result = append(result, httpapi.Project{
			ID:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			CreatedAt:   p.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return result, nil
}

func (h *HTTPProjectsService) CreateToken(ctx context.Context, projectID, userID string, options httpapi.TokenCreateOptions) (string, httpapi.ProjectToken, error) {
	var expiresAt *time.Time
	if options.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, options.ExpiresAt)
		if err != nil {
			return "", httpapi.ProjectToken{}, fmt.Errorf("%w: expires_at must be RFC3339", projects.ErrInvalidTokenOptions)
		}
		expiresAt = &parsed
	}
	plain, tok, err := h.svc.CreateTokenWithOptions(ctx, projectID, userID, projects.TokenCreateOptions{Description: options.Description, Scopes: options.Scopes, ExpiresAt: expiresAt, Environment: options.Environment, TokenType: options.TokenType})
	return plain, toHTTPToken(tok), err
}
func (h *HTTPProjectsService) ListTokens(ctx context.Context, projectID, userID string) ([]httpapi.ProjectToken, error) {
	items, err := h.svc.ListTokens(ctx, projectID, userID)
	if err != nil {
		return nil, err
	}
	out := make([]httpapi.ProjectToken, 0, len(items))
	for _, item := range items {
		out = append(out, toHTTPToken(item))
	}
	return out, nil
}
func (h *HTTPProjectsService) RevokeToken(ctx context.Context, projectID, tokenID, userID, reason string) (httpapi.ProjectToken, error) {
	tok, err := h.svc.RevokeToken(ctx, projectID, tokenID, userID, reason)
	return toHTTPToken(tok), err
}
func (h *HTTPProjectsService) RotateToken(ctx context.Context, projectID, tokenID, userID string, options httpapi.TokenCreateOptions) (string, httpapi.ProjectToken, error) {
	var expiresAt *time.Time
	if options.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, options.ExpiresAt)
		if err != nil {
			return "", httpapi.ProjectToken{}, fmt.Errorf("%w: expires_at must be RFC3339", projects.ErrInvalidTokenOptions)
		}
		expiresAt = &parsed
	}
	plain, tok, err := h.svc.RotateToken(ctx, projectID, tokenID, userID, projects.TokenCreateOptions{Description: options.Description, Scopes: options.Scopes, ExpiresAt: expiresAt, Environment: options.Environment, TokenType: options.TokenType})
	return plain, toHTTPToken(tok), err
}
func (h *HTTPProjectsService) ListTokenAudit(ctx context.Context, projectID, tokenID, userID string) ([]httpapi.TokenAuditEvent, error) {
	items, err := h.svc.ListTokenAudit(ctx, projectID, tokenID, userID)
	if err != nil {
		return nil, err
	}
	out := make([]httpapi.TokenAuditEvent, 0, len(items))
	for _, e := range items {
		out = append(out, httpapi.TokenAuditEvent{ID: e.ID, ProjectID: e.ProjectID, TokenID: e.TokenID, ActorUserID: e.ActorUserID, EventType: e.EventType, Metadata: e.Metadata, CreatedAt: e.CreatedAt.Format(time.RFC3339)})
	}
	return out, nil
}
func (h *HTTPProjectsService) GetTokenPolicy(ctx context.Context, projectID, userID string) (httpapi.TokenPolicy, error) {
	p, err := h.svc.GetTokenPolicy(ctx, projectID, userID)
	return toHTTPPolicy(p), err
}
func (h *HTTPProjectsService) UpdateTokenPolicy(ctx context.Context, projectID, userID string, p httpapi.TokenPolicy) (httpapi.TokenPolicy, error) {
	updated, err := h.svc.UpdateTokenPolicy(ctx, projectID, userID, projects.TokenPolicy{ProjectID: projectID, AllowNonExpiring: p.AllowNonExpiring, MaxProductionTTLDays: p.MaxProductionTTLDays, MaxCITTLDays: p.MaxCITTLDays, ExpiryWarningDays: p.ExpiryWarningDays, UnusedWarningDays: p.UnusedWarningDays})
	return toHTTPPolicy(updated), err
}
func (h *HTTPProjectsService) GetTokenRiskSummary(ctx context.Context, projectID, userID string) (httpapi.TokenRiskSummary, error) {
	summary, err := h.svc.GetTokenRiskSummary(ctx, projectID, userID)
	return httpapi.TokenRiskSummary{ExpiringSoon: summary.ExpiringSoon, Unused: summary.Unused, BroadProduction: summary.BroadProduction, UnclassifiedEnvironment: summary.UnclassifiedEnvironment}, err
}
func toHTTPToken(t projects.ProjectToken) httpapi.ProjectToken {
	return httpapi.ProjectToken{ID: t.ID, ProjectID: t.ProjectID, TokenPrefix: t.TokenPrefix, Scopes: t.ScopeList, Description: t.Description, CreatedAt: tokenTime(t.CreatedAt), ExpiresAt: optionalTime(t.ExpiresAt), LastUsedAt: optionalTime(t.LastUsedAt), CreatedBy: t.CreatedBy, RevokedAt: optionalTime(t.RevokedAt), RevokedBy: t.RevokedBy, RevocationReason: t.RevocationReason, RotatesTokenID: t.RotatesTokenID, Environment: t.Environment, TokenType: t.TokenType, LastUsedEndpoint: t.LastUsedEndpoint, LastUsedAgentID: t.LastUsedAgentID, LastUsedInstanceID: t.LastUsedInstanceID, Status: string(t.Status(time.Now().UTC()))}
}
func toHTTPPolicy(p projects.TokenPolicy) httpapi.TokenPolicy {
	return httpapi.TokenPolicy{ProjectID: p.ProjectID, AllowNonExpiring: p.AllowNonExpiring, MaxProductionTTLDays: p.MaxProductionTTLDays, MaxCITTLDays: p.MaxCITTLDays, ExpiryWarningDays: p.ExpiryWarningDays, UnusedWarningDays: p.UnusedWarningDays, UpdatedBy: p.UpdatedBy, UpdatedAt: tokenTime(p.UpdatedAt)}
}
func tokenTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
func optionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return tokenTime(*t)
}
