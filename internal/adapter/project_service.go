package adapter

import (
	"context"

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

func (h *HTTPProjectsService) CreateToken(ctx context.Context, projectID, userID, description string, scopes []string) (string, httpapi.ProjectToken, error) {
	plainToken, tok, err := h.svc.CreateToken(ctx, projectID, userID, description, scopes)
	if err != nil {
		return "", httpapi.ProjectToken{}, err
	}
	return plainToken, httpapi.ProjectToken{
		ID:          tok.ID,
		ProjectID:   tok.ProjectID,
		TokenPrefix: tok.TokenPrefix,
		Scopes:      tok.ScopeList,
		Description: tok.Description,
	}, nil
}
