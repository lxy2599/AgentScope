package sampling

import (
	"context"
	"errors"
	"testing"
)

// ---- Fake Querier ----

type fakeQuerier struct {
	rules    []SamplingRule
	idSeq    int
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{
		rules: make([]SamplingRule, 0),
	}
}

func (f *fakeQuerier) CreateSamplingRule(ctx context.Context, params CreateRuleParams) (SamplingRule, error) {
	f.idSeq++
	rule := SamplingRule{
		ID:         "rule-" + itoa(f.idSeq),
		ProjectID:  params.ProjectID,
		AgentID:    params.AgentID,
		Name:       params.Name,
		Enabled:    true,
		SampleRate: params.SampleRate,
		Filters:    params.Filters,
	}
	f.rules = append(f.rules, rule)
	return rule, nil
}

func (f *fakeQuerier) ListEnabledRulesForRun(ctx context.Context, projectID, agentID string) ([]SamplingRule, error) {
	var result []SamplingRule
	for _, r := range f.rules {
		if r.ProjectID != projectID || !r.Enabled {
			continue
		}
		// AgentID match: rule with no agent_id matches all; specific agent_id matches
		if r.AgentID != "" && r.AgentID != agentID {
			continue
		}
		result = append(result, r)
	}
	return result, nil
}

func (f *fakeQuerier) ListSamplingRulesByProject(ctx context.Context, projectID string) ([]SamplingRule, error) {
	var result []SamplingRule
	for _, r := range f.rules {
		if r.ProjectID == projectID {
			result = append(result, r)
		}
	}
	return result, nil
}

func (f *fakeQuerier) GetSamplingRuleByID(ctx context.Context, ruleID string) (SamplingRule, error) {
	for _, r := range f.rules {
		if r.ID == ruleID {
			return r, nil
		}
	}
	return SamplingRule{}, errors.New("rule not found")
}

func (f *fakeQuerier) UpdateSamplingRuleEnabled(ctx context.Context, ruleID string, enabled bool) (SamplingRule, error) {
	for i, r := range f.rules {
		if r.ID == ruleID {
			f.rules[i].Enabled = enabled
			return f.rules[i], nil
		}
	}
	return SamplingRule{}, errors.New("rule not found")
}

func (f *fakeQuerier) DeleteSamplingRule(ctx context.Context, ruleID string) error {
	for i, r := range f.rules {
		if r.ID == ruleID {
			f.rules = append(f.rules[:i], f.rules[i+1:]...)
			return nil
		}
	}
	return errors.New("rule not found")
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

// ---- Tests ----

func TestCreateSamplingRule(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	rule, err := svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID:  "proj-1",
		Name:       "test-rule",
		SampleRate: 0.5,
		Filters:    `{"has_error": true}`,
	})
	if err != nil {
		t.Fatalf("CreateRule() error = %v", err)
	}
	if rule.Name != "test-rule" {
		t.Errorf("CreateRule().Name = %q, want %q", rule.Name, "test-rule")
	}
	if rule.SampleRate != 0.5 {
		t.Errorf("CreateRule().SampleRate = %f, want %f", rule.SampleRate, 0.5)
	}
	if !rule.Enabled {
		t.Error("CreateRule().Enabled = false, want true")
	}
	if rule.ProjectID != "proj-1" {
		t.Errorf("CreateRule().ProjectID = %q, want %q", rule.ProjectID, "proj-1")
	}
}

func TestCreateSamplingRule_Validation(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	tests := []struct {
		name    string
		params  CreateRuleParams
		wantErr string
	}{
		{
			name:    "missing projectID",
			params:  CreateRuleParams{Name: "test", SampleRate: 0.5},
			wantErr: "projectID is required",
		},
		{
			name:    "missing name",
			params:  CreateRuleParams{ProjectID: "proj-1", SampleRate: 0.5},
			wantErr: "name is required",
		},
		{
			name:    "sample rate too high",
			params:  CreateRuleParams{ProjectID: "proj-1", Name: "test", SampleRate: 1.5},
			wantErr: "sample_rate must be between 0.0 and 1.0",
		},
		{
			name:    "sample rate too low",
			params:  CreateRuleParams{ProjectID: "proj-1", Name: "test", SampleRate: -0.1},
			wantErr: "sample_rate must be between 0.0 and 1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreateRule(context.Background(), tt.params)
			if err == nil {
				t.Fatal("CreateRule() expected error, got nil")
			}
			if err.Error() != tt.wantErr {
				t.Errorf("CreateRule() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestListRulesByProject(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	// Create rules in two projects
	svc.CreateRule(context.Background(), CreateRuleParams{ProjectID: "proj-1", Name: "rule-1", SampleRate: 1.0})
	svc.CreateRule(context.Background(), CreateRuleParams{ProjectID: "proj-1", Name: "rule-2", SampleRate: 0.5})
	svc.CreateRule(context.Background(), CreateRuleParams{ProjectID: "proj-2", Name: "rule-3", SampleRate: 0.2})

	rules, err := svc.ListRules(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("ListRules() error = %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("ListRules() returned %d rules, want 2", len(rules))
	}
}

func TestGetSamplingRuleByID(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	created, _ := svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID: "proj-1", Name: "test", SampleRate: 0.5,
	})

	got, err := svc.GetRule(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetRule() error = %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("GetRule().ID = %q, want %q", got.ID, created.ID)
	}
}

func TestGetSamplingRuleByID_NotFound(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	_, err := svc.GetRule(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("GetRule() expected error for nonexistent rule")
	}
}

func TestUpdateSamplingRuleEnabled(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	created, _ := svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID: "proj-1", Name: "test", SampleRate: 0.5,
	})

	updated, err := svc.UpdateEnabled(context.Background(), created.ID, false)
	if err != nil {
		t.Fatalf("UpdateEnabled() error = %v", err)
	}
	if updated.Enabled {
		t.Error("UpdateEnabled() rule still enabled after disabling")
	}

	// Re-enable
	updated, err = svc.UpdateEnabled(context.Background(), created.ID, true)
	if err != nil {
		t.Fatalf("UpdateEnabled() error = %v", err)
	}
	if !updated.Enabled {
		t.Error("UpdateEnabled() rule still disabled after re-enabling")
	}
}

func TestDeleteSamplingRule(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	created, _ := svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID: "proj-1", Name: "test", SampleRate: 0.5,
	})

	err := svc.DeleteRule(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("DeleteRule() error = %v", err)
	}

	// Verify it's gone
	_, err = svc.GetRule(context.Background(), created.ID)
	if err == nil {
		t.Error("GetRule() succeeded after delete, expected not found")
	}
}

func TestShouldSample(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	// Create a rule with sample_rate=1.0 (always sample)
	svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID:  "proj-1",
		Name:       "always-sample",
		SampleRate: 1.0,
		Filters:    `{"has_error": true}`,
	})

	// Create a rule with sample_rate=0.0 (never sample)
	svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID:  "proj-1",
		Name:       "never-sample",
		SampleRate: 0.0,
		Filters:    `{}`,
	})

	tests := []struct {
		name       string
		projectID  string
		agentID    string
		filters    map[string]any
		wantSample bool
		wantError  bool
	}{
		{
			name:       "sample_rate=1 with matching has_error filter",
			projectID:  "proj-1",
			agentID:    "agent-1",
			filters:    map[string]any{"error": "timeout"},
			wantSample: true,
		},
		{
			name:       "sample_rate=1 with non-matching has_error filter",
			projectID:  "proj-1",
			agentID:    "agent-1",
			filters:    map[string]any{},
			wantSample: false,
		},
		{
			name:       "no rules for project",
			projectID:  "proj-other",
			agentID:    "agent-1",
			filters:    map[string]any{},
			wantSample: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sampled, rule, err := svc.ShouldSample(context.Background(), tt.projectID, tt.agentID, tt.filters)
			if tt.wantError {
				if err == nil {
					t.Fatal("ShouldSample() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ShouldSample() unexpected error: %v", err)
			}
			if sampled != tt.wantSample {
				t.Errorf("ShouldSample() sampled = %v, want %v", sampled, tt.wantSample)
			}
			if tt.wantSample && rule == nil {
				t.Error("ShouldSample() rule is nil when sampled = true")
			}
			if !tt.wantSample && rule != nil {
				t.Error("ShouldSample() rule should be nil when sampled = false")
			}
		})
	}
}
