package adapter

import (
	"context"
	"errors"

	"AgentScope/internal/sampling"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// SamplingQuerier adapts store.Queries to implement sampling.Querier.
type SamplingQuerier struct {
	q *store.Queries
}

var _ sampling.Querier = (*SamplingQuerier)(nil)

// NewSamplingQuerier creates a new SamplingQuerier.
func NewSamplingQuerier(q *store.Queries) *SamplingQuerier {
	return &SamplingQuerier{q: q}
}

func (s *SamplingQuerier) CreateSamplingRule(ctx context.Context, params sampling.CreateRuleParams) (sampling.SamplingRule, error) {
	params, err := sampling.ValidateCreateRuleParams(params)
	if err != nil {
		return sampling.SamplingRule{}, err
	}

	var agentID pgtype.UUID
	if params.AgentID != "" {
		agentID = parsePGUUID(params.AgentID)
	}

	storeRule, err := s.q.CreateTraceSamplingRule(ctx, store.CreateTraceSamplingRuleParams{
		ProjectID:      parsePGUUID(params.ProjectID),
		AgentID:        agentID,
		Name:           params.Name,
		Enabled:        true,
		SampleRate:     params.SampleRate,
		Filters:        []byte(params.Filters),
		Strategy:       params.Strategy,
		DailyBudget:    int32(params.DailyBudget),
		Stratification: []byte(params.Stratification),
	})
	if err != nil {
		return sampling.SamplingRule{}, err
	}
	return samplingRuleToDomain(storeRule), nil
}

func (s *SamplingQuerier) ReserveSamplingDecision(
	ctx context.Context,
	ruleID, runID, stratum string,
	dailyBudget, stratumBudget int,
) (bool, error) {
	reserved, err := s.q.ReserveTraceSamplingDecision(ctx, store.ReserveTraceSamplingDecisionParams{
		RuleID: parsePGUUID(ruleID), RunID: parsePGUUID(runID), Stratum: stratum,
		DailyBudget: int32(dailyBudget), StratumBudget: int32(stratumBudget),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return reserved, err
}

func (s *SamplingQuerier) ListSamplingRulesByProject(ctx context.Context, projectID string) ([]sampling.SamplingRule, error) {
	storeRules, err := s.q.ListTraceSamplingRulesByProject(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, err
	}
	result := make([]sampling.SamplingRule, 0, len(storeRules))
	for _, r := range storeRules {
		result = append(result, samplingRuleToDomain(r))
	}
	return result, nil
}

func (s *SamplingQuerier) GetSamplingRuleByID(ctx context.Context, ruleID string) (sampling.SamplingRule, error) {
	storeRule, err := s.q.GetTraceSamplingRuleByID(ctx, parsePGUUID(ruleID))
	if err != nil {
		return sampling.SamplingRule{}, err
	}
	return samplingRuleToDomain(storeRule), nil
}

func (s *SamplingQuerier) UpdateSamplingRuleByProject(ctx context.Context, projectID, ruleID string, params sampling.UpdateRuleParams) (sampling.SamplingRule, error) {
	params, err := sampling.ValidateUpdateRuleParams(params)
	if err != nil {
		return sampling.SamplingRule{}, err
	}

	var name pgtype.Text
	if params.Name != nil {
		name = pgtype.Text{String: *params.Name, Valid: true}
	}
	var enabled pgtype.Bool
	if params.Enabled != nil {
		enabled = pgtype.Bool{Bool: *params.Enabled, Valid: true}
	}
	var sampleRate pgtype.Float4
	if params.SampleRate != nil {
		sampleRate = pgtype.Float4{Float32: *params.SampleRate, Valid: true}
	}
	var filters []byte
	if params.Filters != nil {
		filters = []byte(*params.Filters)
	}

	storeRule, err := s.q.UpdateTraceSamplingRuleByProject(ctx, store.UpdateTraceSamplingRuleByProjectParams{
		Name:       name,
		Enabled:    enabled,
		SampleRate: sampleRate,
		Filters:    filters,
		ID:         parsePGUUID(ruleID),
		ProjectID:  parsePGUUID(projectID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sampling.SamplingRule{}, sampling.ErrRuleNotFound
		}
		return sampling.SamplingRule{}, err
	}
	return samplingRuleToDomain(storeRule), nil
}

func (s *SamplingQuerier) DeleteSamplingRule(ctx context.Context, ruleID string) error {
	_, err := s.q.DeleteTraceSamplingRule(ctx, parsePGUUID(ruleID))
	return err
}

func (s *SamplingQuerier) ListEnabledRulesForRun(ctx context.Context, projectID, agentID string) ([]sampling.SamplingRule, error) {
	var agentUUID pgtype.UUID
	if agentID != "" {
		agentUUID = parsePGUUID(agentID)
	}

	storeRules, err := s.q.ListEnabledSamplingRulesForRun(ctx, store.ListEnabledSamplingRulesForRunParams{
		ProjectID: parsePGUUID(projectID),
		Column2:   agentUUID,
	})
	if err != nil {
		return nil, err
	}
	result := make([]sampling.SamplingRule, 0, len(storeRules))
	for _, r := range storeRules {
		result = append(result, samplingRuleToDomain(r))
	}
	return result, nil
}

func samplingRuleToDomain(r store.TraceSamplingRule) sampling.SamplingRule {
	agentID := ""
	if r.AgentID.Valid {
		agentID = UUIDToString(r.AgentID.Bytes)
	}
	return sampling.SamplingRule{
		ID:             UUIDToString(r.ID.Bytes),
		ProjectID:      UUIDToString(r.ProjectID.Bytes),
		AgentID:        agentID,
		Name:           r.Name,
		Enabled:        r.Enabled,
		SampleRate:     r.SampleRate,
		Filters:        string(r.Filters),
		Strategy:       r.Strategy,
		DailyBudget:    int(r.DailyBudget),
		Stratification: string(r.Stratification),
	}
}
