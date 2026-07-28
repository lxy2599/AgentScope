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
	auditEvents []TokenAuditEvent
	policies    map[string]TokenPolicy
	projCounter int
	memberFail  bool // when true, AddProjectMember always fails
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{
		projects:   make(map[string]*Project),
		members:    make(map[string]map[string]string),
		tokens:     make(map[string]*ProjectToken),
		tokensByID: make(map[string]*ProjectToken),
		policies:   make(map[string]TokenPolicy),
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

func (f *fakeStorage) CreateProjectTokenWithMetadata(ctx context.Context, projectID, tokenHash, tokenPrefix, scope, description, createdBy string, expiresAt *time.Time, environment, tokenType, rotatesTokenID string) (ProjectToken, error) {
	_, err := f.CreateProjectToken(ctx, projectID, tokenHash, tokenPrefix, scope, description)
	if err != nil {
		return ProjectToken{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := f.tokens[tokenHash]
	stored.CreatedBy, stored.ExpiresAt, stored.Environment, stored.TokenType, stored.RotatesTokenID = createdBy, expiresAt, environment, tokenType, rotatesTokenID
	return *stored, nil
}
func (f *fakeStorage) ListProjectTokens(ctx context.Context, projectID string) ([]ProjectToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []ProjectToken{}
	for _, t := range f.tokensByID {
		if t.ProjectID == projectID {
			out = append(out, *t)
		}
	}
	return out, nil
}
func (f *fakeStorage) RevokeProjectToken(ctx context.Context, projectID, tokenID, revokedBy, reason string) (ProjectToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tokensByID[tokenID]
	if !ok || t.ProjectID != projectID || t.RevokedAt != nil {
		return ProjectToken{}, ErrTokenNotFound
	}
	now := time.Now().UTC()
	t.RevokedAt = &now
	t.RevokedBy = revokedBy
	t.RevocationReason = reason
	return *t, nil
}
func (f *fakeStorage) TouchProjectToken(ctx context.Context, tokenID, endpoint, agentID, instanceID string, usedAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tokensByID[tokenID]
	if !ok {
		return ErrTokenNotFound
	}
	t.LastUsedAt = &usedAt
	t.LastUsedEndpoint = endpoint
	t.LastUsedAgentID = agentID
	t.LastUsedInstanceID = instanceID
	return nil
}
func (f *fakeStorage) CreateTokenAuditEvent(ctx context.Context, event TokenAuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	f.auditEvents = append(f.auditEvents, event)
	return nil
}
func (f *fakeStorage) ListTokenAuditEvents(ctx context.Context, projectID, tokenID string) ([]TokenAuditEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := make([]TokenAuditEvent, 0)
	for _, event := range f.auditEvents {
		if event.ProjectID == projectID && event.TokenID == tokenID {
			items = append(items, event)
		}
	}
	return items, nil
}
func (f *fakeStorage) GetTokenPolicy(ctx context.Context, projectID string) (TokenPolicy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	policy, ok := f.policies[projectID]
	if !ok {
		return TokenPolicy{}, ErrTokenNotFound
	}
	return policy, nil
}
func (f *fakeStorage) UpsertTokenPolicy(ctx context.Context, policy TokenPolicy) (TokenPolicy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	policy.UpdatedAt = time.Now().UTC()
	f.policies[policy.ProjectID] = policy
	return policy, nil
}

func TestTokenGovernanceMemberCannotCreateOrList(t *testing.T) {
	ctx := context.Background()
	store := newFakeStorage()
	svc := NewService(store)
	project, err := svc.CreateProject(ctx, "owner", "project", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddProjectMember(ctx, project.ID, "member", "member"); err != nil {
		t.Fatal(err)
	}

	_, _, err = svc.CreateToken(ctx, project.ID, "member", "blocked", []string{ScopeOTLPWrite})
	if !errors.Is(err, ErrProjectAdminRequired) {
		t.Fatalf("member create error = %v, want admin requirement", err)
	}
	_, err = svc.ListTokens(ctx, project.ID, "member")
	if !errors.Is(err, ErrProjectAdminRequired) {
		t.Fatalf("member list error = %v, want admin requirement", err)
	}
}

func TestTokenRevocationAndExpiryRejectAuthentication(t *testing.T) {
	ctx := context.Background()
	store := newFakeStorage()
	svc := NewService(store)
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	project, err := svc.CreateProject(ctx, "owner", "project", "")
	if err != nil {
		t.Fatal(err)
	}

	plain, token, err := svc.CreateToken(ctx, project.ID, "owner", "revoke me", []string{ScopeOTLPWrite})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RevokeToken(ctx, project.ID, token.ID, "owner", "rotated"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateToken(ctx, plain, ScopeOTLPWrite); !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("revoked auth error = %v, want ErrTokenRevoked", err)
	}
	if events, err := svc.ListTokenAudit(ctx, project.ID, token.ID, "owner"); err != nil || len(events) != 2 {
		t.Fatalf("audit events = %d, %v; want create + revoke", len(events), err)
	}

	expired := now.Add(-time.Hour)
	expiredPlain, _, err := svc.CreateTokenWithOptions(ctx, project.ID, "owner", TokenCreateOptions{Description: "expired", Scopes: []string{ScopeOTLPWrite}, ExpiresAt: &expired})
	if !errors.Is(err, ErrInvalidTokenOptions) || expiredPlain != "" {
		t.Fatalf("past expiry creation = %q, %v; want validation error", expiredPlain, err)
	}
	future := now.Add(time.Hour)
	validPlain, expiring, err := svc.CreateTokenWithOptions(ctx, project.ID, "owner", TokenCreateOptions{Description: "short", Scopes: []string{ScopeOTLPWrite}, ExpiresAt: &future})
	if err != nil {
		t.Fatal(err)
	}
	later := future.Add(time.Nanosecond)
	svc.now = func() time.Time { return later }
	if _, err := svc.AuthenticateToken(ctx, validPlain, ScopeOTLPWrite); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expired auth error = %v, want ErrTokenExpired", err)
	}
	if expiring.Status(later) != TokenStatusExpired {
		t.Fatal("token status should be expired")
	}
}

func TestTokenRotatePreservesOldTokenUntilExplicitRevocation(t *testing.T) {
	ctx := context.Background()
	store := newFakeStorage()
	svc := NewService(store)
	project, err := svc.CreateProject(ctx, "owner", "project", "")
	if err != nil {
		t.Fatal(err)
	}
	oldPlain, old, err := svc.CreateToken(ctx, project.ID, "owner", "old", []string{ScopeOTLPWrite})
	if err != nil {
		t.Fatal(err)
	}
	newPlain, replacement, err := svc.RotateToken(ctx, project.ID, old.ID, "owner", TokenCreateOptions{Description: "new", Scopes: []string{ScopeOTLPWrite}, Environment: TokenEnvironmentProduction})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.RotatesTokenID != old.ID {
		t.Fatalf("rotates token ID = %q, want %q", replacement.RotatesTokenID, old.ID)
	}
	if _, err := svc.AuthenticateToken(ctx, oldPlain, ScopeOTLPWrite); err != nil {
		t.Fatalf("old token should still work: %v", err)
	}
	if _, err := svc.AuthenticateToken(ctx, newPlain, ScopeOTLPWrite); err != nil {
		t.Fatalf("replacement should work: %v", err)
	}
	if _, _, err := svc.RotateToken(ctx, project.ID, "unknown", "owner", TokenCreateOptions{Description: "bad", Scopes: []string{ScopeOTLPWrite}}); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("unknown rotation error = %v", err)
	}
}

func TestTokenUsageIsCoalescedAndFlushesLatestMetadata(t *testing.T) {
	ctx := context.Background()
	store := newFakeStorage()
	svc := NewService(store)
	svc.usageFlushDelay = time.Hour // explicit flush keeps this test deterministic.
	project, err := svc.CreateProject(ctx, "owner", "project", "")
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := svc.CreateToken(ctx, project.ID, "owner", "usage", []string{ScopeOTLPWrite})
	if err != nil {
		t.Fatal(err)
	}
	svc.RecordTokenUse(ctx, token.ID, "scope:otlp:write", "", "")
	svc.RecordTokenUse(ctx, token.ID, "scope:runner:write", "", "")
	if err := svc.FlushTokenUses(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.findProjectToken(ctx, project.ID, token.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LastUsedAt == nil || stored.LastUsedEndpoint != "scope:runner:write" {
		t.Fatalf("usage metadata = %#v, want latest coalesced value", stored)
	}
}

func TestTokenPolicyAndRiskSummary(t *testing.T) {
	ctx := context.Background()
	store := newFakeStorage()
	svc := NewService(store)
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	project, err := svc.CreateProject(ctx, "owner", "project", "")
	if err != nil {
		t.Fatal(err)
	}
	maxTTL := 30
	policy, err := svc.UpdateTokenPolicy(ctx, project.ID, "owner", TokenPolicy{AllowNonExpiring: false, MaxProductionTTLDays: &maxTTL, ExpiryWarningDays: []int{7, 1}, UnusedWarningDays: 60})
	if err != nil {
		t.Fatal(err)
	}
	if policy.AllowNonExpiring {
		t.Fatal("policy should disable non-expiring tokens")
	}
	_, _, err = svc.CreateTokenWithOptions(ctx, project.ID, "owner", TokenCreateOptions{Description: "no-expiry", Scopes: []string{ScopeOTLPWrite}, Environment: TokenEnvironmentProduction})
	if !errors.Is(err, ErrTokenPolicyViolation) {
		t.Fatalf("non-expiring policy error = %v", err)
	}
	tooLong := now.Add(31 * 24 * time.Hour)
	_, _, err = svc.CreateTokenWithOptions(ctx, project.ID, "owner", TokenCreateOptions{Description: "long", Scopes: []string{ScopeOTLPWrite}, Environment: TokenEnvironmentProduction, ExpiresAt: &tooLong})
	if !errors.Is(err, ErrTokenPolicyViolation) {
		t.Fatalf("long TTL policy error = %v", err)
	}
	soon := now.Add(24 * time.Hour)
	_, broad, err := svc.CreateTokenWithOptions(ctx, project.ID, "owner", TokenCreateOptions{Description: "broad", Scopes: []string{ScopeOTLPWrite, ScopeRunnerWrite, ScopeAgentRegister, ScopeAgentRuntime}, Environment: TokenEnvironmentProduction, ExpiresAt: &soon})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := svc.GetTokenRiskSummary(ctx, project.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if summary.ExpiringSoon != 1 || summary.Unused != 1 || summary.BroadProduction != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	if broad.ID == "" {
		t.Fatal("expected created token")
	}
}
