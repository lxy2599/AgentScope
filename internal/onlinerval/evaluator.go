package onlinerval

import (
	"context"
	"fmt"
	"strings"

	"AgentScope/internal/runs"
)

// RunDetailProvider provides detailed run data including trace information
// such as spans, tool calls, and retrieval calls from ClickHouse storage.
type RunDetailProvider interface {
	GetRunDetail(ctx context.Context, runID string) (*runs.RunDetail, error)
}

// TraceAwareEvaluator evaluates runs using real trace data from ClickHouse.
// It produces multi-dimensional scores (efficiency, final_answer, tool_correctness,
// trajectory, rag_groundedness, safety) based on available trace data.
// When trace data is missing for a specific dimension, it returns an explicit
// reason rather than assuming a score from run status alone.
type TraceAwareEvaluator struct {
	detailProvider RunDetailProvider
}

// NewTraceAwareEvaluator creates a new TraceAwareEvaluator.
func NewTraceAwareEvaluator(provider RunDetailProvider) *TraceAwareEvaluator {
	return &TraceAwareEvaluator{detailProvider: provider}
}

// EvaluateRun evaluates a run using trace data and returns multi-dimensional scores.
func (e *TraceAwareEvaluator) EvaluateRun(ctx context.Context, projectID, runID string) ([]EvalScore, error) {
	detail, err := e.detailProvider.GetRunDetail(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("get run detail: %w", err)
	}

	run := detail.Run

	scores := []EvalScore{
		e.evalEfficiency(run),
		e.evalFinalAnswer(run),
		e.evalToolCorrectness(run, detail.ToolCalls),
		e.evalTrajectory(run, detail.Spans),
		e.evalRAGGroundedness(run, detail.RetrievalCalls),
		e.evalSafety(run),
	}

	// Fill in project/run/trace IDs
	for i := range scores {
		scores[i].ProjectID = projectID
		scores[i].RunID = runID
		scores[i].TraceID = run.TraceID
	}

	return scores, nil
}

// evalEfficiency scores based on run completion status.
func (e *TraceAwareEvaluator) evalEfficiency(run runs.Run) EvalScore {
	isCompleted := run.Status == "completed"
	score := 0.5
	if isCompleted {
		score = 1.0
	} else if run.Status == "failed" {
		score = 0.0
	}

	return EvalScore{
		MetricName: "completion_status",
		MetricKind: "efficiency",
		Score:      score,
		Passed:     isCompleted,
		Reason:     runStatusReason(run.Status, run.Error),
	}
}

// evalFinalAnswer scores based on whether the run produced output.
func (e *TraceAwareEvaluator) evalFinalAnswer(run runs.Run) EvalScore {
	if run.Status == "completed" {
		if run.Output != "" {
			return EvalScore{
				MetricName: "final_answer_quality",
				MetricKind: "final_answer",
				Score:      1.0,
				Passed:     true,
				Reason:     "Final answer generated; run completed successfully",
			}
		}
		return EvalScore{
			MetricName: "final_answer_quality",
			MetricKind: "final_answer",
			Score:      0.5,
			Passed:     false,
			Reason:     "Run completed but output is empty; final answer quality cannot be assessed",
		}
	}

	errMsg := run.Error
	if errMsg == "" {
		errMsg = "run did not complete"
	}
	reason := fmt.Sprintf("Final answer not available: %s", errMsg)

	return EvalScore{
		MetricName: "final_answer_quality",
		MetricKind: "final_answer",
		Score:      0.0,
		Passed:     false,
		Reason:     reason,
	}
}

// evalToolCorrectness scores based on actual tool call records from trace data.
func (e *TraceAwareEvaluator) evalToolCorrectness(run runs.Run, toolCalls []runs.ToolCallRecord) EvalScore {
	if len(toolCalls) == 0 {
		return EvalScore{
			MetricName: "tool_correctness",
			MetricKind: "tool_correctness",
			Score:      0.5,
			Passed:     run.Status == "completed",
			Reason:     "tool call records unavailable for tool_correctness evaluation",
		}
	}

	successCount := 0
	for _, tc := range toolCalls {
		if tc.Success {
			successCount++
		}
	}

	ratio := float64(successCount) / float64(len(toolCalls))
	reason := fmt.Sprintf("%d/%d tool calls succeeded", successCount, len(toolCalls))

	if successCount == len(toolCalls) {
		reason = "All tool calls succeeded; no tool errors detected"
	} else {
		// Include first failed tool name in reason
		for _, tc := range toolCalls {
			if !tc.Success && tc.Error != "" {
				reason = fmt.Sprintf("%s; first failure: %s - %s", reason, tc.ToolName, tc.Error)
				break
			}
		}
	}

	return EvalScore{
		MetricName: "tool_correctness",
		MetricKind: "tool_correctness",
		Score:      ratio,
		Passed:     ratio >= 0.5,
		Reason:     reason,
	}
}

// evalTrajectory scores based on span existence from trace data.
func (e *TraceAwareEvaluator) evalTrajectory(run runs.Run, spans []runs.SpanRecord) EvalScore {
	if len(spans) == 0 {
		return EvalScore{
			MetricName: "trajectory_quality",
			MetricKind: "trajectory",
			Score:      0.5,
			Passed:     run.Status == "completed",
			Reason:     "trace data unavailable for trajectory evaluation",
		}
	}

	// Score based on span status: if any span has error, lower the score
	hasError := false
	for _, s := range spans {
		if s.StatusCode == "ERROR" {
			hasError = true
			break
		}
	}

	score := 0.7
	if hasError {
		score = 0.4
	}

	reason := "Trajectory evaluated from span-level trace data"
	if hasError {
		reason = "Trajectory includes error spans"
	}

	return EvalScore{
		MetricName: "trajectory_quality",
		MetricKind: "trajectory",
		Score:      score,
		Passed:     score >= 0.5,
		Reason:     reason,
	}
}

// evalRAGGroundedness scores based on retrieval call records from trace data.
func (e *TraceAwareEvaluator) evalRAGGroundedness(run runs.Run, retrievalCalls []runs.RetrievalCallRecord) EvalScore {
	if len(retrievalCalls) == 0 {
		return EvalScore{
			MetricName: "rag_groundedness",
			MetricKind: "rag_groundedness",
			Score:      0.5,
			Passed:     run.Status == "completed",
			Reason:     "retrieval records unavailable for rag_groundedness evaluation",
		}
	}

	// Check if retrieval calls returned documents
	hasResults := false
	for _, rc := range retrievalCalls {
		if rc.ReturnedCount != nil && *rc.ReturnedCount > 0 {
			hasResults = true
			break
		}
		if len(rc.DocumentIDs) > 0 {
			hasResults = true
			break
		}
	}

	score := 0.7
	reason := "Retrieval records found; assuming acceptable groundedness"
	if !hasResults {
		score = 0.4
		reason = "Retrieval calls present but returned no documents"
	}

	return EvalScore{
		MetricName: "rag_groundedness",
		MetricKind: "rag_groundedness",
		Score:      score,
		Passed:     score >= 0.5,
		Reason:     reason,
	}
}

// evalSafety scores based on run errors.
func (e *TraceAwareEvaluator) evalSafety(run runs.Run) EvalScore {
	if run.Error == "" {
		return EvalScore{
			MetricName: "safety",
			MetricKind: "safety",
			Score:      1.0,
			Passed:     true,
			Reason:     "No errors detected during run execution",
		}
	}

	return EvalScore{
		MetricName: "safety",
		MetricKind: "safety",
		Score:      0.5,
		Passed:     false,
		Reason:     fmt.Sprintf("Run encountered error: %s", run.Error),
	}
}

// runStatusReason returns a human-readable reason for a run's status.
func runStatusReason(status, errStr string) string {
	if errStr != "" {
		return "Run " + status + ": " + strings.TrimPrefix(errStr, "Run ")
	}
	return "Run " + status
}
