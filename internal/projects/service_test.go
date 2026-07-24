package projects

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeStorage struct {
	mu          sync.Mutex
	projects    map[string]*Project
	members     map[string]map[string]string // projectID -> userID -> role
	tokens      map[string]*ProjectToken
	tokensByID  map[string]*ProjectToken
	projCounter int
	memberFail  bool // when true, AddProjectMember always fails
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

func (f *fakeStorage) CreateProjectWithOwner(ctx context.Context, userID, name, description string) (Project, error) {
	proj, err := f.CreateProject(ctx, name, description)
	if err != nil {
		return Project{}, err
	}
	if err := f.AddProjectMember(ctx, proj.ID, userID, "owner"); err != nil {
		// Simulate atomic rollback by deleting the project
		f.mu.Lock()
		delete(f.projects, proj.ID)
		delete(f.members, proj.ID)
		f.mu.Unlock()
		return Project{}, err
	}
	return proj, nil
}

func (f *fakeStorage) AddProjectMember(ctx context.Context, projectID, userID, role string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.memberFail {
		return fmt.Errorf("simulated member failure")
	}
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

func (f *fakeStorage) GetProjectMember(ctx context.Context, projectID, userID string) (ProjectMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if roles, ok := f.members[projectID]; ok {
		if role, member := roles[userID]; member {
			return ProjectMember{ProjectID: projectID, UserID: userID, Role: role}, nil
		}
	}
	return ProjectMember{}, fmt.Errorf("not a project member")
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

func TestCreateProject_rollsBackOnMemberFailure(t *testing.T) {
	ctx := context.Background()
	store := newFakeStorage()
	// Prevent member addition by making AddProjectMember always fail
	store.memberFail = true
	svc := NewService(store)

	_, err := svc.CreateProject(ctx, "user-1", "Rollback Test", "")
	if err == nil {
		t.Fatal("expected error when AddProjectMember fails")
	}

	// The project should not be visible after rollback
	projects, err := svc.ListProjects(ctx, "user-1")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("got %d projects after rollback, want 0", len(projects))
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

func TestIsProjectAdminAllowsOwnerAndRejectsMember(t *testing.T) {
	ctx := context.Background()
	storage := newFakeStorage()
	service := NewService(storage)
	project, err := service.CreateProject(ctx, "owner-1", "权限测试", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := storage.AddProjectMember(ctx, project.ID, "member-1", "member"); err != nil {
		t.Fatalf("AddProjectMember: %v", err)
	}

	if err := service.IsProjectAdmin(ctx, "owner-1", project.ID); err != nil {
		t.Fatalf("owner rejected: %v", err)
	}
	if err := service.IsProjectAdmin(ctx, "member-1", project.ID); err == nil {
		t.Fatal("member accepted as project admin")
	}
}

func TestCreateToken_requiresMembership(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newFakeStorage())

	proj, err := svc.CreateProject(ctx, "user-1", "Project", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	_, _, err = svc.CreateToken(ctx, proj.ID, "user-2", "test token", []string{"otlp:write"})
	if err == nil {
		t.Fatal("expected error for non-member creating token")
	}
}

func TestCreateToken_returnsPlainToken(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newFakeStorage())

	proj, err := svc.CreateProject(ctx, "user-1", "Project", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	plainToken, token, err := svc.CreateToken(ctx, proj.ID, "user-1", "test token", []string{"otlp:write"})
	if err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}

	if !strings.HasPrefix(plainToken, "ascope_") {
		t.Fatalf("plainToken prefix = %q, want ascope_", plainToken[:7])
	}
	if len(plainToken) <= 7 {
		t.Fatal("plainToken too short")
	}
	if token.TokenHash == "" {
		t.Fatal("token.TokenHash is empty")
	}
	if len(token.TokenHash) != 64 {
		t.Fatalf("token hash length = %d, want 64", len(token.TokenHash))
	}
}

func TestCreateToken_normalizesAndValidatesScopes(t *testing.T) {
	ctx := context.Background()
	storage := newFakeStorage()
	svc := NewService(storage)

	proj, err := svc.CreateProject(ctx, "user-1", "Project", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	_, token, err := svc.CreateToken(
		ctx,
		proj.ID,
		"user-1",
		"test token",
		[]string{ScopeOTLPWrite, ScopeOTLPWrite, ScopeAgentRuntime},
	)
	if err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}
	if got, want := strings.Join(token.ScopeList, ","), "otlp:write,agent:runtime"; got != want {
		t.Fatalf("scopes = %q, want %q", got, want)
	}

	tests := []struct {
		name   string
		scopes []string
	}{
		{name: "empty", scopes: nil},
		{name: "blank", scopes: []string{""}},
		{name: "unknown", scopes: []string{"admin:write"}},
		{name: "whitespace", scopes: []string{" otlp:write"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(storage.tokens)
			_, _, err := svc.CreateToken(ctx, proj.ID, "user-1", "invalid", tt.scopes)
			if !errors.Is(err, ErrInvalidTokenScopes) {
				t.Fatalf("CreateToken() error = %v, want ErrInvalidTokenScopes", err)
			}
			if got := len(storage.tokens); got != before {
				t.Fatalf("stored token count = %d, want %d", got, before)
			}
		})
	}
}

func TestAuthenticateToken_validScope(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newFakeStorage())

	proj, err := svc.CreateProject(ctx, "user-1", "Project", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	plainToken, _, err := svc.CreateToken(ctx, proj.ID, "user-1", "test", []string{"otlp:write", "runner:write"})
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

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

	plainToken, _, err := svc.CreateToken(ctx, proj.ID, "user-1", "test", []string{"runner:write"})
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
