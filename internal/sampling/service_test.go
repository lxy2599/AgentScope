package sampling

import (
	"context"
	"errors"
	"math"
	"testing"
)

// ---- Fake Querier ----

type fakeQuerier struct {
	rules       []SamplingRule
	idSeq       int
	updateCalls int
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

func (f *fakeQuerier) UpdateSamplingRuleByProject(ctx context.Context, projectID, ruleID string, params UpdateRuleParams) (SamplingRule, error) {
	f.updateCalls++
	for i, r := range f.rules {
		if r.ID == ruleID && r.ProjectID == projectID {
			if params.Name != nil {
				f.rules[i].Name = *params.Name
			}
			if params.Enabled != nil {
				f.rules[i].Enabled = *params.Enabled
			}
			if params.SampleRate != nil {
				f.rules[i].SampleRate = *params.SampleRate
			}
			if params.Filters != nil {
				f.rules[i].Filters = *params.Filters
			}
			return f.rules[i], nil
		}
	}
	return SamplingRule{}, ErrRuleNotFound
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

func TestCreateSamplingRule_DefaultsEmptyFiltersToObject(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	rule, err := svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID:  "proj-1",
		Name:       "default-filters",
		SampleRate: 0.5,
	})
	if err != nil {
		t.Fatalf("CreateRule() error = %v", err)
	}
	if rule.Filters != "{}" {
		t.Fatalf("CreateRule().Filters = %q, want {}", rule.Filters)
	}
	if len(q.rules) != 1 || q.rules[0].Filters != "{}" {
		t.Fatalf("persisted filters = %#v, want normalized object", q.rules)
	}
}

func TestCreateSamplingRule_Validation(t *testing.T) {
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
		{
			name:    "sample rate NaN",
			params:  CreateRuleParams{ProjectID: "proj-1", Name: "test", SampleRate: float32(math.NaN())},
			wantErr: "sample_rate must be between 0.0 and 1.0",
		},
		{
			name:    "sample rate positive infinity",
			params:  CreateRuleParams{ProjectID: "proj-1", Name: "test", SampleRate: float32(math.Inf(1))},
			wantErr: "sample_rate must be between 0.0 and 1.0",
		},
		{
			name:    "sample rate negative infinity",
			params:  CreateRuleParams{ProjectID: "proj-1", Name: "test", SampleRate: float32(math.Inf(-1))},
			wantErr: "sample_rate must be between 0.0 and 1.0",
		},
		{
			name:    "malformed filters",
			params:  CreateRuleParams{ProjectID: "proj-1", Name: "test", SampleRate: 0.5, Filters: `{"status":`},
			wantErr: "filters must be a valid JSON object",
		},
		{
			name:    "array filters",
			params:  CreateRuleParams{ProjectID: "proj-1", Name: "test", SampleRate: 0.5, Filters: `[]`},
			wantErr: "filters must be a valid JSON object",
		},
		{
			name:    "scalar filters",
			params:  CreateRuleParams{ProjectID: "proj-1", Name: "test", SampleRate: 0.5, Filters: `true`},
			wantErr: "filters must be a valid JSON object",
		},
		{
			name:    "null filters",
			params:  CreateRuleParams{ProjectID: "proj-1", Name: "test", SampleRate: 0.5, Filters: `null`},
			wantErr: "filters must be a valid JSON object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newFakeQuerier()
			svc := NewService(q)
			_, err := svc.CreateRule(context.Background(), tt.params)
			if err == nil {
				t.Fatal("CreateRule() expected error, got nil")
			}
			if err.Error() != tt.wantErr {
				t.Errorf("CreateRule() error = %q, want %q", err.Error(), tt.wantErr)
			}
			if q.idSeq != 0 || len(q.rules) != 0 {
				t.Fatalf("invalid rule reached persistence: idSeq=%d rules=%d", q.idSeq, len(q.rules))
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

func TestUpdateSamplingRule(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	created, _ := svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID: "proj-1", Name: "test", SampleRate: 0.5,
	})

	name := "updated"
	enabled := false
	sampleRate := float32(0.75)
	filters := `{"status":"failed"}`
	updated, err := svc.UpdateRule(context.Background(), "proj-1", created.ID, UpdateRuleParams{
		Name:       &name,
		Enabled:    &enabled,
		SampleRate: &sampleRate,
		Filters:    &filters,
	})
	if err != nil {
		t.Fatalf("UpdateRule() error = %v", err)
	}
	if updated.Name != name || updated.Enabled || updated.SampleRate != sampleRate || updated.Filters != filters {
		t.Fatalf("UpdateRule() = %#v, want all fields updated", updated)
	}

	// An omitted field is left unchanged.
	enabled = true
	updated, err = svc.UpdateRule(context.Background(), "proj-1", created.ID, UpdateRuleParams{Enabled: &enabled})
	if err != nil {
		t.Fatalf("UpdateRule() re-enable error = %v", err)
	}
	if !updated.Enabled || updated.Name != name || updated.SampleRate != sampleRate || updated.Filters != filters {
		t.Fatalf("partial UpdateRule() changed omitted fields: %#v", updated)
	}
	if q.updateCalls != 2 {
		t.Fatalf("persistence update calls = %d, want 2", q.updateCalls)
	}
}

func TestUpdateSamplingRule_ValidationBeforePersistence(t *testing.T) {
	disabled := false
	nan := float32(math.NaN())
	positiveInf := float32(math.Inf(1))
	negativeInf := float32(math.Inf(-1))
	outOfRange := float32(1.5)
	malformed := `{"status":`
	array := `[]`
	scalar := `true`
	nullValue := `null`

	tests := []struct {
		name    string
		params  UpdateRuleParams
		wantErr error
	}{
		{name: "empty patch", params: UpdateRuleParams{}, wantErr: ErrNoUpdateFields},
		{name: "sample rate NaN", params: UpdateRuleParams{Enabled: &disabled, SampleRate: &nan}, wantErr: ErrInvalidSampleRate},
		{name: "sample rate positive infinity", params: UpdateRuleParams{Enabled: &disabled, SampleRate: &positiveInf}, wantErr: ErrInvalidSampleRate},
		{name: "sample rate negative infinity", params: UpdateRuleParams{Enabled: &disabled, SampleRate: &negativeInf}, wantErr: ErrInvalidSampleRate},
		{name: "sample rate out of range", params: UpdateRuleParams{Enabled: &disabled, SampleRate: &outOfRange}, wantErr: ErrInvalidSampleRate},
		{name: "malformed filters", params: UpdateRuleParams{Enabled: &disabled, Filters: &malformed}, wantErr: ErrInvalidFilters},
		{name: "array filters", params: UpdateRuleParams{Enabled: &disabled, Filters: &array}, wantErr: ErrInvalidFilters},
		{name: "scalar filters", params: UpdateRuleParams{Enabled: &disabled, Filters: &scalar}, wantErr: ErrInvalidFilters},
		{name: "null filters", params: UpdateRuleParams{Enabled: &disabled, Filters: &nullValue}, wantErr: ErrInvalidFilters},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newFakeQuerier()
			svc := NewService(q)
			created, err := svc.CreateRule(context.Background(), CreateRuleParams{
				ProjectID:  "proj-1",
				Name:       "unchanged",
				SampleRate: 0.5,
			})
			if err != nil {
				t.Fatalf("CreateRule() error = %v", err)
			}

			_, err = svc.UpdateRule(context.Background(), "proj-1", created.ID, tt.params)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("UpdateRule() error = %v, want %v", err, tt.wantErr)
			}
			if q.updateCalls != 0 {
				t.Fatalf("invalid update reached persistence %d times", q.updateCalls)
			}
			unchanged, err := q.GetSamplingRuleByID(context.Background(), created.ID)
			if err != nil {
				t.Fatalf("GetSamplingRuleByID() error = %v", err)
			}
			if !unchanged.Enabled || unchanged.SampleRate != 0.5 || unchanged.Filters != "{}" {
				t.Fatalf("invalid update mutated rule: %#v", unchanged)
			}
		})
	}
}

func TestUpdateSamplingRule_RejectsForeignProject(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)
	created, err := svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID:  "proj-owner",
		Name:       "owner-rule",
		SampleRate: 0.5,
	})
	if err != nil {
		t.Fatalf("CreateRule() error = %v", err)
	}

	enabled := false
	_, err = svc.UpdateRule(context.Background(), "proj-foreign", created.ID, UpdateRuleParams{Enabled: &enabled})
	if !errors.Is(err, ErrRuleNotFound) {
		t.Fatalf("foreign UpdateRule() error = %v, want not found", err)
	}
	got, err := q.GetSamplingRuleByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetSamplingRuleByID() error = %v", err)
	}
	if !got.Enabled {
		t.Fatal("foreign project mutated sampling rule")
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

func TestShouldSample_Deterministic(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	// Create a rule with sample_rate=0.5
	svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID:  "proj-1",
		Name:       "partial-sample",
		SampleRate: 0.5,
	})

	// Same input should always produce the same result
	filters := map[string]any{"run_id": "run-1", "trace_id": "trace-1"}
	result1, _, _ := svc.ShouldSample(context.Background(), "proj-1", "agent-1", filters)
	result2, _, _ := svc.ShouldSample(context.Background(), "proj-1", "agent-1", filters)
	result3, _, _ := svc.ShouldSample(context.Background(), "proj-1", "agent-1", filters)

	if result1 != result2 || result2 != result3 {
		t.Error("ShouldSample is not deterministic: same inputs produced different results")
	}
}

func TestShouldSample_DifferentRunsHaveDifferentResults(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	// Create a rule with sample_rate that allows some variation
	svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID:  "proj-1",
		Name:       "partial-sample",
		SampleRate: 0.5,
	})

	// Different run IDs with the same project/agent should have different sampling outcomes
	// (they use different inputs = different hash values)
	results := make(map[bool]int)
	for i := 0; i < 100; i++ {
		sampled, _, _ := svc.ShouldSample(context.Background(), "proj-1", "agent-1",
			map[string]any{"run_id": "run-" + itoa(i), "trace_id": "trace-" + itoa(i)})
		results[sampled]++
	}

	// With 100 runs and sample_rate=0.5, we should see some of each
	if results[true] == 0 || results[false] == 0 {
		t.Errorf("expected a mix of sampled/not-sampled with 100 different runs, got sampled=%d unsampled=%d",
			results[true], results[false])
	}
}

func TestShouldSample_ConcurrentSafe(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	// Create rules covering different sample rates
	svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID:  "proj-1",
		Name:       "always",
		SampleRate: 1.0,
	})
	svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID:  "proj-1",
		Name:       "half",
		SampleRate: 0.5,
	})
	svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID:  "proj-1",
		Name:       "never",
		SampleRate: 0.0,
	})

	const goroutines = 20
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(runID int) {
			_, _, err := svc.ShouldSample(context.Background(), "proj-1", "agent-1",
				map[string]any{"run_id": itoa(runID), "trace_id": itoa(runID)})
			errs <- err
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent ShouldSample failed: %v", err)
		}
	}
}

func TestShouldSample_SampleRateOneAlwaysSamples(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID:  "proj-1",
		Name:       "always",
		SampleRate: 1.0,
	})

	// Run multiple times with different IDs to confirm always sampled
	for i := 0; i < 20; i++ {
		sampled, _, err := svc.ShouldSample(context.Background(), "proj-1", "agent-1",
			map[string]any{"run_id": itoa(i)})
		if err != nil {
			t.Fatalf("ShouldSample() error = %v", err)
		}
		if !sampled {
			t.Error("ShouldSample() with sample_rate=1.0 should always sample")
		}
	}
}

func TestShouldSample_SampleRateZeroNeverSamples(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID:  "proj-1",
		Name:       "never",
		SampleRate: 0.0,
	})

	for i := 0; i < 20; i++ {
		sampled, _, err := svc.ShouldSample(context.Background(), "proj-1", "agent-1",
			map[string]any{"run_id": itoa(i)})
		if err != nil {
			t.Fatalf("ShouldSample() error = %v", err)
		}
		if sampled {
			t.Error("ShouldSample() with sample_rate=0.0 should never sample")
		}
	}
}

func TestShouldSample_EmptyRunFilters(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	svc.CreateRule(context.Background(), CreateRuleParams{
		ProjectID:  "proj-1",
		Name:       "always",
		SampleRate: 1.0,
		Filters:    `{}`,
	})

	// Empty runFilters should still work (hash will use empty strings for run_id/trace_id)
	sampled, _, err := svc.ShouldSample(context.Background(), "proj-1", "agent-1", map[string]any{})
	if err != nil {
		t.Fatalf("ShouldSample() error = %v", err)
	}
	if !sampled {
		t.Error("ShouldSample() with sample_rate=1.0 should sample even with empty runFilters")
	}
}

func TestSampleHash_SameInputSameResult(t *testing.T) {
	filters := map[string]any{"run_id": "run-42", "trace_id": "trace-99"}
	h1 := sampleHash("proj-1", "agent-1", "rule-1", filters)
	h2 := sampleHash("proj-1", "agent-1", "rule-1", filters)
	if h1 != h2 {
		t.Errorf("sampleHash is not deterministic: got %f then %f", h1, h2)
	}
}

func TestSampleHash_DifferentInputsDifferentResults(t *testing.T) {
	h1 := sampleHash("proj-1", "agent-1", "rule-1", map[string]any{"run_id": "run-1"})
	h2 := sampleHash("proj-1", "agent-1", "rule-1", map[string]any{"run_id": "run-2"})
	if h1 == h2 {
		t.Error("sampleHash should produce different results for different run IDs")
	}
}

func TestSampleHash_ProducesStableFloat(t *testing.T) {
	// Verify that the hash produces values in [0, 1)
	h := sampleHash("proj-1", "agent-1", "rule-1", map[string]any{"run_id": "run-1", "trace_id": "trace-1"})
	if h < 0 || h >= 1.0 {
		t.Errorf("sampleHash produced %f, expected in [0, 1)", h)
	}
}

func TestSampleHash_NoRunID_UsesDefaults(t *testing.T) {
	// Without run_id/trace_id, the hash should still be deterministic
	h1 := sampleHash("proj-1", "agent-1", "rule-1", map[string]any{})
	h2 := sampleHash("proj-1", "agent-1", "rule-1", map[string]any{})
	if h1 != h2 {
		t.Error("sampleHash without run_id should still be deterministic")
	}
}
