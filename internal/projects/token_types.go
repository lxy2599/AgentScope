package projects

import "time"

const (
	TokenEnvironmentDevelopment = "development"
	TokenEnvironmentStaging     = "staging"
	TokenEnvironmentProduction  = "production"
	TokenEnvironmentCI          = "ci"
	TokenEnvironmentLocal       = "local"

	TokenTypeTraceSDK     = "trace_sdk"
	TokenTypeRunner       = "runner"
	TokenTypeAgentRuntime = "agent_runtime"
	TokenTypeCI           = "ci"
	TokenTypeCustom       = "custom"
)

var validTokenEnvironments = map[string]struct{}{
	TokenEnvironmentDevelopment: {}, TokenEnvironmentStaging: {}, TokenEnvironmentProduction: {}, TokenEnvironmentCI: {}, TokenEnvironmentLocal: {},
}
var validTokenTypes = map[string]struct{}{
	TokenTypeTraceSDK: {}, TokenTypeRunner: {}, TokenTypeAgentRuntime: {}, TokenTypeCI: {}, TokenTypeCustom: {},
}

type TokenCreateOptions struct {
	Description    string
	Scopes         []string
	ExpiresAt      *time.Time
	Environment    string
	TokenType      string
	RotatesTokenID string
}

type TokenStatus string

const (
	TokenStatusActive  TokenStatus = "active"
	TokenStatusRevoked TokenStatus = "revoked"
	TokenStatusExpired TokenStatus = "expired"
)

type TokenAuditEvent struct {
	ID          string
	ProjectID   string
	TokenID     string
	ActorUserID string
	EventType   string
	Metadata    map[string]any
	CreatedAt   time.Time
}

type TokenPolicy struct {
	ProjectID            string
	AllowNonExpiring     bool
	MaxProductionTTLDays *int
	MaxCITTLDays         *int
	ExpiryWarningDays    []int
	UnusedWarningDays    int
	UpdatedBy            string
	UpdatedAt            time.Time
}

type TokenRiskSummary struct {
	ExpiringSoon            int `json:"expiring_soon"`
	Unused                  int `json:"unused"`
	BroadProduction         int `json:"broad_production"`
	UnclassifiedEnvironment int `json:"unclassified_environment"`
}
