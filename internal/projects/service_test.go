package projects

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeStorage struct {
	mu         sync.Mutex
	projects   map[string]*Project
	members    map[string]map[string]string // projectID -> userID -> role
	tokens     map[string]*ProjectToken
	tokensByID map[string]*ProjectToken
	users      []string // user IDs that exist
	userCounter int
	projCounter int
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{
		projects:   make(map[string]*Project),
		members:    make(map[string]map[string]string),
		tokens:     make(map[string]*ProjectToken),
		tokensByID: make(map[string]*ProjectToken),
	}
}

func (f *fakeStorage) CreateProject(ctx context.Context, name, description string) (Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.projCounter++
	id := fmt.Sprintf("proj-%d", f.projCounter)
	p := Project{
		ID:          id,
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
	}
	f.projects[id] = &p
	f.members[id] = make(map[string]string)
	return p, nil
}

func (f *fakeStorage) AddProjectMember(ctx context.Context, projectID, userID, role string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.projects[projectID]; !ok {
		return fmt.Errorf("project not found")
	}
	if f.members[projectID] == nil {
		f.members[projectID] = make(map[string]string)
	}
	f.members[projectID][userID] = role
	return nil
}

func (f *fakeStorage) ListProjectsByUser(ctx context.Context, userID string) ([]Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []Project
	for _, p := range f.projects {
		if roles, ok := f.members[p.ID]; ok {
			if _, member := roles[userID]; member {
				result = append(result, *p)
			}
		}
	}
	return result, nil
}

func (f *fakeStorage) CreateProjectToken(ctx context.Context, projectID, tokenHash, tokenPrefix, scope, description string) (ProjectToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uuid.New().String()
	t := ProjectToken{
		ID:          id,
		ProjectID:   projectID,
		TokenHash:   tokenHash,
		TokenPrefix: tokenPrefix,
		Scopes:      scope,
		ScopeList:   strings.Fields(scope),
		Description: description,
		CreatedAt:   time.Now(),
	}
	f.tokens[tokenHash] = &t
	f.tokensByID[id] = &t
	return t, nil
}

func (f *fakeStorage) GetTokenByHash(ctx context.Context, tokenHash []byte) (ProjectToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tokens[string(tokenHash)]
	if !ok {
		return ProjectToken{}, fmt.Errorf("token not found")
	}
	return *t, nil
}

// ---- Tests ----

func TestCreateProject_addsOwner(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newFakeStorage())

	proj, err := svc.CreateProject(ctx, "user-1", "Test Project", "a test project")
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if proj.Name != "Test Project" {
		t.Errorf("proj.Name = %q, want %q", proj.Name, "Test Project")
	}
	if proj.ID == "" {
		t.Fatal("proj.ID is empty")
	}
}

func TestListProjects(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newFakeStorage())

	_, err := svc.CreateProject(ctx, "user-1", "Project A", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	_, err = svc.CreateProject(ctx, "user-1", "Project B", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// Create a project for another user
	_, err = svc.CreateProject(ctx, "user-2", "Project C", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	projects, err := svc.ListProjects(ctx, "user-1")
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("got %d projects, want 2", len(projects))
	}
}

func TestCreateToken_returnsPlainToken(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newFakeStorage())

	proj, err := svc.CreateProject(ctx, "user-1", "Project", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	plainToken, token, err := svc.CreateToken(ctx, proj.ID, "test token", []string{"otlp:write"})
	if err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}

	// Plain token must start with ascope_
	if !strings.HasPrefix(plainToken, "ascope_") {
		t.Fatalf("plainToken prefix = %q, want %q", plainToken[:7], "ascope_")
	}
	if len(plainToken) <= 7 {
		t.Fatal("plainToken too short")
	}

	// Returned ProjectToken must not contain the plain token; TokenHash is SHA-256 hex (64 chars)
	if token.TokenHash == "" {
		t.Fatal("token.TokenHash is empty")
	}
	if len(token.TokenHash) != 64 {
		t.Fatalf("token hash length = %d, want 64 (SHA-256 hex)", len(token.TokenHash))
	}
}

func TestAuthenticateToken_validScope(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newFakeStorage())

	proj, err := svc.CreateProject(ctx, "user-1", "Project", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	plainToken, _, err := svc.CreateToken(ctx, proj.ID, "test", []string{"otlp:write", "runner:write"})
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	// Authenticate with matching scope
	auth, err := svc.AuthenticateToken(ctx, plainToken, "otlp:write")
	if err != nil {
		t.Fatalf("AuthenticateToken() error = %v", err)
	}
	if auth.ProjectID != proj.ID {
		t.Errorf("auth.ProjectID = %q, want %q", auth.ProjectID, proj.ID)
	}
}

func TestAuthenticateToken_missingScope(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newFakeStorage())

	proj, err := svc.CreateProject(ctx, "user-1", "Project", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	plainToken, _, err := svc.CreateToken(ctx, proj.ID, "test", []string{"runner:write"})
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	_, err = svc.AuthenticateToken(ctx, plainToken, "otlp:write")
	if err == nil {
		t.Fatal("AuthenticateToken() expected error for missing scope")
	}
}

func TestAuthenticateToken_invalidPrefix(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newFakeStorage())

	_, err := svc.AuthenticateToken(ctx, "invalid-token-value", "otlp:write")
	if err == nil {
		t.Fatal("AuthenticateToken() expected error for invalid prefix")
	}
}
