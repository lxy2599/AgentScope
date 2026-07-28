package projects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"AgentScope/internal/auth"
)

// Project is the service-level project representation.
type Project struct {
	ID, Name, Description string
	CreatedAt             time.Time
}

// ProjectToken deliberately excludes plaintext; TokenHash is never exposed by HTTP adapters.
type ProjectToken struct {
	ID, ProjectID, TokenHash, TokenPrefix, Scopes, Description string
	ScopeList                                                  []string
	CreatedAt                                                  time.Time
	ExpiresAt, LastUsedAt, RevokedAt                           *time.Time
	CreatedBy, RevokedBy, RevocationReason, RotatesTokenID     string
	Environment, TokenType                                     string
	LastUsedEndpoint, LastUsedAgentID, LastUsedInstanceID      string
}

func (t ProjectToken) Status(now time.Time) TokenStatus {
	if t.RevokedAt != nil {
		return TokenStatusRevoked
	}
	if t.ExpiresAt != nil && !t.ExpiresAt.After(now) {
		return TokenStatusExpired
	}
	return TokenStatusActive
}

type AuthenticatedToken struct {
	ProjectID, TokenID string
	Scopes             []string
}
type ProjectMember struct{ ProjectID, UserID, Role string }

// Querier is the project domain persistence boundary. PostgreSQL stores all token facts.
type Querier interface {
	CreateProjectWithOwner(ctx context.Context, userID, name, description string) (Project, error)
	AddProjectMember(ctx context.Context, projectID, userID, role string) error
	ListProjectsByUser(ctx context.Context, userID string) ([]Project, error)
	CreateProjectTokenWithMetadata(ctx context.Context, projectID, tokenHash, tokenPrefix, scope, description, createdBy string, expiresAt *time.Time, environment, tokenType, rotatesTokenID string) (ProjectToken, error)
	GetTokenByHash(ctx context.Context, tokenHash []byte) (ProjectToken, error)
	ListProjectTokens(ctx context.Context, projectID string) ([]ProjectToken, error)
	RevokeProjectToken(ctx context.Context, projectID, tokenID, revokedBy, reason string) (ProjectToken, error)
	TouchProjectToken(ctx context.Context, tokenID, endpoint, agentID, instanceID string, usedAt time.Time) error
	CreateTokenAuditEvent(ctx context.Context, event TokenAuditEvent) error
	ListTokenAuditEvents(ctx context.Context, projectID, tokenID string) ([]TokenAuditEvent, error)
	GetTokenPolicy(ctx context.Context, projectID string) (TokenPolicy, error)
	UpsertTokenPolicy(ctx context.Context, policy TokenPolicy) (TokenPolicy, error)
	GetProjectMember(ctx context.Context, projectID, userID string) (ProjectMember, error)
}

type Service struct {
	q               Querier
	now             func() time.Time
	usageMu         sync.Mutex
	pendingUses     map[string]pendingTokenUse
	usageTimer      *time.Timer
	usageFlushDelay time.Duration
}

func NewService(q Querier) *Service {
	return &Service{
		q: q, now: func() time.Time { return time.Now().UTC() },
		pendingUses: make(map[string]pendingTokenUse), usageFlushDelay: tokenUsageFlushDelay,
	}
}

func (s *Service) CreateProject(ctx context.Context, userID, name, description string) (Project, error) {
	if name == "" {
		return Project{}, errors.New("project name is required")
	}
	if userID == "" {
		return Project{}, errors.New("user ID is required")
	}
	return s.q.CreateProjectWithOwner(ctx, userID, name, description)
}
func (s *Service) ListProjects(ctx context.Context, userID string) ([]Project, error) {
	if userID == "" {
		return nil, errors.New("user ID is required")
	}
	return s.q.ListProjectsByUser(ctx, userID)
}

const tokenPrefix = "ascope_"

const (
	ScopeOTLPWrite     = "otlp:write"
	ScopeRunnerWrite   = "runner:write"
	ScopeAgentRegister = "agent:register"
	ScopeAgentRuntime  = "agent:runtime"
)

const tokenUsageFlushDelay = 250 * time.Millisecond

var (
	ErrInvalidTokenScopes   = errors.New("token scopes are invalid")
	ErrInvalidTokenOptions  = errors.New("project token options are invalid")
	ErrProjectAdminRequired = errors.New("project administrator role is required")
	ErrTokenNotFound        = errors.New("project token not found")
	ErrTokenRevoked         = errors.New("project token revoked")
	ErrTokenExpired         = errors.New("project token expired")
	ErrTokenPolicyViolation = errors.New("project token policy violation")
)

var supportedTokenScopes = map[string]struct{}{
	ScopeOTLPWrite: {}, ScopeRunnerWrite: {}, ScopeAgentRegister: {}, ScopeAgentRuntime: {},
}

type pendingTokenUse struct {
	tokenID, endpoint, agentID, instanceID string
	usedAt                                 time.Time
}

func defaultTokenPolicy(projectID string) TokenPolicy {
	return TokenPolicy{
		ProjectID: projectID, AllowNonExpiring: true,
		ExpiryWarningDays: []int{14, 7, 1}, UnusedWarningDays: 60,
	}
}

func (s *Service) IsProjectMember(ctx context.Context, userID, projectID string) error {
	if userID == "" || projectID == "" {
		return fmt.Errorf("%w: user ID and project ID are required", ErrInvalidTokenOptions)
	}
	_, err := s.q.GetProjectMember(ctx, projectID, userID)
	return err
}

func (s *Service) IsProjectAdmin(ctx context.Context, userID, projectID string) error {
	member, err := s.q.GetProjectMember(ctx, projectID, userID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProjectAdminRequired, err)
	}
	if member.Role != "owner" && member.Role != "admin" {
		return ErrProjectAdminRequired
	}
	return nil
}

// CreateToken remains as the backward-compatible entry point. It creates a
// development custom token without expiry, subject to the project's policy.
func (s *Service) CreateToken(ctx context.Context, projectID, userID, description string, scopes []string) (string, ProjectToken, error) {
	return s.CreateTokenWithOptions(ctx, projectID, userID, TokenCreateOptions{
		Description: description, Scopes: scopes,
		Environment: TokenEnvironmentDevelopment, TokenType: TokenTypeCustom,
	})
}

func (s *Service) CreateTokenWithOptions(ctx context.Context, projectID, userID string, options TokenCreateOptions) (string, ProjectToken, error) {
	if projectID == "" || userID == "" {
		return "", ProjectToken{}, fmt.Errorf("%w: project ID and user ID are required", ErrInvalidTokenOptions)
	}
	if err := s.IsProjectAdmin(ctx, userID, projectID); err != nil {
		return "", ProjectToken{}, err
	}
	scopes, err := normalizeTokenScopes(options.Scopes)
	if err != nil {
		return "", ProjectToken{}, err
	}
	if options.Environment == "" {
		options.Environment = TokenEnvironmentDevelopment
	}
	if _, ok := validTokenEnvironments[options.Environment]; !ok {
		return "", ProjectToken{}, fmt.Errorf("%w: invalid environment", ErrInvalidTokenOptions)
	}
	if options.TokenType == "" {
		options.TokenType = TokenTypeCustom
	}
	if _, ok := validTokenTypes[options.TokenType]; !ok {
		return "", ProjectToken{}, fmt.Errorf("%w: invalid token type", ErrInvalidTokenOptions)
	}
	policy, err := s.tokenPolicy(ctx, projectID)
	if err != nil {
		return "", ProjectToken{}, err
	}
	if err := validateTokenPolicy(s.now(), policy, options); err != nil {
		return "", ProjectToken{}, err
	}

	randomBytes, err := auth.GenerateRandomBytes(32)
	if err != nil {
		return "", ProjectToken{}, fmt.Errorf("generate token: %w", err)
	}
	randomPart := hex.EncodeToString(randomBytes)
	plain := tokenPrefix + randomPart
	hash := sha256.Sum256([]byte(plain))
	prefix := tokenPrefix + randomPart[:12]
	created, err := s.q.CreateProjectTokenWithMetadata(
		ctx, projectID, hex.EncodeToString(hash[:]), prefix, strings.Join(scopes, " "),
		options.Description, userID, options.ExpiresAt, options.Environment, options.TokenType, options.RotatesTokenID,
	)
	if err != nil {
		return "", ProjectToken{}, fmt.Errorf("store token: %w", err)
	}
	// The audit payload deliberately contains only safe, operational metadata.
	_ = s.writeAudit(ctx, TokenAuditEvent{
		ProjectID: projectID, TokenID: created.ID, ActorUserID: userID, EventType: "created",
		Metadata:  map[string]any{"environment": options.Environment, "token_type": options.TokenType, "scopes": scopes},
		CreatedAt: s.now(),
	})
	return plain, created, nil
}

func (s *Service) tokenPolicy(ctx context.Context, projectID string) (TokenPolicy, error) {
	policy, err := s.q.GetTokenPolicy(ctx, projectID)
	if errors.Is(err, ErrTokenNotFound) {
		return defaultTokenPolicy(projectID), nil
	}
	return policy, err
}

func validateTokenPolicy(now time.Time, policy TokenPolicy, options TokenCreateOptions) error {
	if !policy.AllowNonExpiring && options.ExpiresAt == nil {
		return fmt.Errorf("%w: expiry is required", ErrTokenPolicyViolation)
	}
	if options.ExpiresAt == nil {
		return nil
	}
	if !options.ExpiresAt.After(now) {
		return fmt.Errorf("%w: expiry must be in the future", ErrInvalidTokenOptions)
	}
	ttl := options.ExpiresAt.Sub(now)
	if options.Environment == TokenEnvironmentProduction && policy.MaxProductionTTLDays != nil && ttl > time.Duration(*policy.MaxProductionTTLDays)*24*time.Hour {
		return fmt.Errorf("%w: production TTL exceeds policy", ErrTokenPolicyViolation)
	}
	if (options.Environment == TokenEnvironmentCI || options.TokenType == TokenTypeCI) && policy.MaxCITTLDays != nil && ttl > time.Duration(*policy.MaxCITTLDays)*24*time.Hour {
		return fmt.Errorf("%w: CI TTL exceeds policy", ErrTokenPolicyViolation)
	}
	return nil
}

func normalizeTokenScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, ErrInvalidTokenScopes
	}
	out := make([]string, 0, len(scopes))
	seen := map[string]struct{}{}
	for _, scope := range scopes {
		if _, ok := supportedTokenScopes[scope]; !ok {
			return nil, ErrInvalidTokenScopes
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out, nil
}

func (s *Service) ListTokens(ctx context.Context, projectID, userID string) ([]ProjectToken, error) {
	if err := s.IsProjectAdmin(ctx, userID, projectID); err != nil {
		return nil, err
	}
	return s.q.ListProjectTokens(ctx, projectID)
}

func (s *Service) findProjectToken(ctx context.Context, projectID, tokenID string) (ProjectToken, error) {
	tokens, err := s.q.ListProjectTokens(ctx, projectID)
	if err != nil {
		return ProjectToken{}, err
	}
	for _, token := range tokens {
		if token.ID == tokenID {
			return token, nil
		}
	}
	return ProjectToken{}, ErrTokenNotFound
}

func (s *Service) RevokeToken(ctx context.Context, projectID, tokenID, userID, reason string) (ProjectToken, error) {
	if err := s.IsProjectAdmin(ctx, userID, projectID); err != nil {
		return ProjectToken{}, err
	}
	current, err := s.findProjectToken(ctx, projectID, tokenID)
	if err != nil {
		return ProjectToken{}, err
	}
	if current.Status(s.now()) == TokenStatusRevoked {
		return ProjectToken{}, ErrTokenRevoked
	}
	token, err := s.q.RevokeProjectToken(ctx, projectID, tokenID, userID, reason)
	if err != nil {
		return ProjectToken{}, err
	}
	_ = s.writeAudit(ctx, TokenAuditEvent{
		ProjectID: projectID, TokenID: tokenID, ActorUserID: userID, EventType: "revoked",
		Metadata: map[string]any{"reason": reason}, CreatedAt: s.now(),
	})
	// A successor needs a first-use record before this retirement is considered
	// a completed rotation. Revocation itself remains an explicit admin action.
	tokens, listErr := s.q.ListProjectTokens(ctx, projectID)
	if listErr == nil {
		for _, successor := range tokens {
			if successor.RotatesTokenID == tokenID && successor.LastUsedAt != nil {
				_ = s.writeAudit(ctx, TokenAuditEvent{
					ProjectID: projectID, TokenID: successor.ID, ActorUserID: userID, EventType: "rotation_completed",
					Metadata: map[string]any{"retired_token_id": tokenID}, CreatedAt: s.now(),
				})
				break
			}
		}
	}
	return token, nil
}

func (s *Service) RotateToken(ctx context.Context, projectID, tokenID, userID string, options TokenCreateOptions) (string, ProjectToken, error) {
	if err := s.IsProjectAdmin(ctx, userID, projectID); err != nil {
		return "", ProjectToken{}, err
	}
	old, err := s.findProjectToken(ctx, projectID, tokenID)
	if err != nil {
		return "", ProjectToken{}, err
	}
	switch old.Status(s.now()) {
	case TokenStatusRevoked:
		return "", ProjectToken{}, ErrTokenRevoked
	case TokenStatusExpired:
		return "", ProjectToken{}, ErrTokenExpired
	}
	options.RotatesTokenID = tokenID
	plain, token, err := s.CreateTokenWithOptions(ctx, projectID, userID, options)
	if err != nil {
		return "", ProjectToken{}, err
	}
	_ = s.writeAudit(ctx, TokenAuditEvent{
		ProjectID: projectID, TokenID: token.ID, ActorUserID: userID, EventType: "rotation_started",
		Metadata: map[string]any{"rotates_token_id": tokenID}, CreatedAt: s.now(),
	})
	return plain, token, nil
}

func (s *Service) ListTokenAudit(ctx context.Context, projectID, tokenID, userID string) ([]TokenAuditEvent, error) {
	if err := s.IsProjectAdmin(ctx, userID, projectID); err != nil {
		return nil, err
	}
	if _, err := s.findProjectToken(ctx, projectID, tokenID); err != nil {
		return nil, err
	}
	return s.q.ListTokenAuditEvents(ctx, projectID, tokenID)
}

func (s *Service) GetTokenPolicy(ctx context.Context, projectID, userID string) (TokenPolicy, error) {
	if err := s.IsProjectAdmin(ctx, userID, projectID); err != nil {
		return TokenPolicy{}, err
	}
	return s.tokenPolicy(ctx, projectID)
}

func (s *Service) UpdateTokenPolicy(ctx context.Context, projectID, userID string, policy TokenPolicy) (TokenPolicy, error) {
	if err := s.IsProjectAdmin(ctx, userID, projectID); err != nil {
		return TokenPolicy{}, err
	}
	if err := validatePolicyFields(policy); err != nil {
		return TokenPolicy{}, err
	}
	policy.ProjectID = projectID
	policy.UpdatedBy = userID
	return s.q.UpsertTokenPolicy(ctx, policy)
}

func validatePolicyFields(policy TokenPolicy) error {
	if policy.MaxProductionTTLDays != nil && *policy.MaxProductionTTLDays <= 0 {
		return fmt.Errorf("%w: production TTL must be positive", ErrInvalidTokenOptions)
	}
	if policy.MaxCITTLDays != nil && *policy.MaxCITTLDays <= 0 {
		return fmt.Errorf("%w: CI TTL must be positive", ErrInvalidTokenOptions)
	}
	if policy.UnusedWarningDays <= 0 {
		return fmt.Errorf("%w: unused warning days must be positive", ErrInvalidTokenOptions)
	}
	if len(policy.ExpiryWarningDays) == 0 {
		return fmt.Errorf("%w: at least one expiry warning day is required", ErrInvalidTokenOptions)
	}
	for _, days := range policy.ExpiryWarningDays {
		if days <= 0 {
			return fmt.Errorf("%w: expiry warning days must be positive", ErrInvalidTokenOptions)
		}
	}
	return nil
}

func (s *Service) GetTokenRiskSummary(ctx context.Context, projectID, userID string) (TokenRiskSummary, error) {
	if err := s.IsProjectAdmin(ctx, userID, projectID); err != nil {
		return TokenRiskSummary{}, err
	}
	policy, err := s.tokenPolicy(ctx, projectID)
	if err != nil {
		return TokenRiskSummary{}, err
	}
	tokens, err := s.q.ListProjectTokens(ctx, projectID)
	if err != nil {
		return TokenRiskSummary{}, err
	}
	now := s.now()
	expiryWarning := 0
	for _, day := range policy.ExpiryWarningDays {
		if day > expiryWarning {
			expiryWarning = day
		}
	}
	result := TokenRiskSummary{}
	for _, token := range tokens {
		if token.Status(now) != TokenStatusActive {
			continue
		}
		if token.ExpiresAt != nil && !token.ExpiresAt.After(now.Add(time.Duration(expiryWarning)*24*time.Hour)) {
			result.ExpiringSoon++
		}
		if token.LastUsedAt == nil || token.LastUsedAt.Before(now.Add(-time.Duration(policy.UnusedWarningDays)*24*time.Hour)) {
			result.Unused++
		}
		if token.Environment == TokenEnvironmentProduction && hasAllScopes(token.ScopeList) {
			result.BroadProduction++
		}
		if token.Environment == "" {
			result.UnclassifiedEnvironment++
		}
	}
	return result, nil
}

func hasAllScopes(scopes []string) bool {
	if len(scopes) != len(supportedTokenScopes) {
		return false
	}
	for _, scope := range scopes {
		if _, ok := supportedTokenScopes[scope]; !ok {
			return false
		}
	}
	return true
}

func (s *Service) AuthenticateToken(ctx context.Context, plainToken, requiredScope string) (AuthenticatedToken, error) {
	if !strings.HasPrefix(plainToken, tokenPrefix) {
		return AuthenticatedToken{}, ErrTokenNotFound
	}
	hash := sha256.Sum256([]byte(plainToken))
	token, err := s.q.GetTokenByHash(ctx, []byte(hex.EncodeToString(hash[:])))
	if err != nil {
		return AuthenticatedToken{}, ErrTokenNotFound
	}
	switch token.Status(s.now()) {
	case TokenStatusRevoked:
		return AuthenticatedToken{}, ErrTokenRevoked
	case TokenStatusExpired:
		return AuthenticatedToken{}, ErrTokenExpired
	}
	for _, scope := range strings.Fields(token.Scopes) {
		if scope == requiredScope {
			return AuthenticatedToken{ProjectID: token.ProjectID, TokenID: token.ID, Scopes: strings.Fields(token.Scopes)}, nil
		}
	}
	return AuthenticatedToken{}, fmt.Errorf("token does not have required scope: %s", requiredScope)
}

// RecordTokenUse collects metadata asynchronously. The request path only takes
// a short mutex and never synchronously writes PostgreSQL. Entries are merged
// by token ID and flushed after a small bounded window; this data is operational
// metadata rather than authorization state.
func (s *Service) RecordTokenUse(_ context.Context, tokenID, endpoint, agentID, instanceID string) {
	if tokenID == "" {
		return
	}
	s.usageMu.Lock()
	if s.pendingUses == nil {
		s.pendingUses = make(map[string]pendingTokenUse)
	}
	s.pendingUses[tokenID] = pendingTokenUse{tokenID: tokenID, endpoint: endpoint, agentID: agentID, instanceID: instanceID, usedAt: s.now()}
	if s.usageTimer == nil {
		s.usageTimer = time.AfterFunc(s.usageFlushDelay, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.FlushTokenUses(ctx)
		})
	}
	s.usageMu.Unlock()
}

// FlushTokenUses is called by the bounded background window and by orderly
// application shutdown. It returns write failures to the caller but never
// participates in token authorization.
func (s *Service) FlushTokenUses(ctx context.Context) error {
	s.usageMu.Lock()
	pending := s.pendingUses
	s.pendingUses = make(map[string]pendingTokenUse)
	if s.usageTimer != nil {
		s.usageTimer.Stop()
		s.usageTimer = nil
	}
	s.usageMu.Unlock()

	var errs []error
	for _, use := range pending {
		if err := s.q.TouchProjectToken(ctx, use.tokenID, use.endpoint, use.agentID, use.instanceID, use.usedAt); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) writeAudit(ctx context.Context, event TokenAuditEvent) error {
	return s.q.CreateTokenAuditEvent(ctx, event)
}
