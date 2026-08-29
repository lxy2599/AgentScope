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

// TraceAwareEvaluator derives execution-health heuristics from ClickHouse trace
// evidence. It does not verify semantic answer correctness, policy safety, or
// factual grounding; those require a rule evaluator, LLM Judge, or human Gold
// annotation. Every emitted reason makes this evidence boundary explicit.
const traceHeuristicPrefix = "Trace heuristic (execution evidence only): "

type TraceAwareEvaluator struct {
	detailProvider RunDetailProvider
}

// NewTraceAwareEvaluator creates a new TraceAwareEvaluator.
func NewTraceAwareEvaluator(provider RunDetailProvider) *TraceAwareEvaluator {
	return &TraceAwareEvaluator{detailProvider: provider}
}

// EvaluateRun evaluates a run from trace evidence and returns execution-health
// heuristic signals. Metric kinds retain the platform-wide taxonomy, while each
// reason explicitly states that semantic quality was not evaluated.
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
		scores[i].EvaluationStatus = "execution_health_only"
		scores[i].EvidenceStatus = "available"
		if strings.Contains(strings.ToLower(scores[i].Reason), "unavailable") ||
			strings.Contains(strings.ToLower(scores[i].Reason), "cannot be assessed") ||
			strings.Contains(strings.ToLower(scores[i].Reason), "not evaluated") {
			scores[i].EvidenceStatus = "insufficient_evidence"
		}
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
		Reason:     traceHeuristicPrefix + runStatusReason(run.Status, run.Error) + "; does not assess semantic task correctness",
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
				Reason:     traceHeuristicPrefix + "output is present after completion; semantic answer correctness was not evaluated",
			}
		}
		return EvalScore{
			MetricName: "final_answer_quality",
			MetricKind: "final_answer",
			Score:      0.5,
			Passed:     false,
			Reason:     traceHeuristicPrefix + "output is empty; semantic answer correctness cannot be assessed",
		}
	}

	errMsg := run.Error
	if errMsg == "" {
		errMsg = "run did not complete"
	}
	reason := traceHeuristicPrefix + fmt.Sprintf("output is unavailable: %s; semantic answer correctness was not evaluated", errMsg)

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
			Reason:     traceHeuristicPrefix + "tool call records are unavailable; tool execution and task correctness cannot be assessed",
		}
	}

	successCount := 0
	for _, tc := range toolCalls {
		if tc.Success {
			successCount++
		}
	}

	ratio := float64(successCount) / float64(len(toolCalls))
	reason := traceHeuristicPrefix + fmt.Sprintf("%d/%d tool calls succeeded; this measures execution success, not task correctness", successCount, len(toolCalls))

	if successCount == len(toolCalls) {
		reason = traceHeuristicPrefix + "all tool calls succeeded; this does not verify tool choice or task correctness"
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
			Reason:     traceHeuristicPrefix + "span data is unavailable; trajectory quality cannot be assessed",
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

	reason := traceHeuristicPrefix + "no ERROR span observed; this does not verify plan quality or task completion"
	if hasError {
		reason = traceHeuristicPrefix + "ERROR span observed; this is an execution anomaly, not a semantic trajectory judgment"
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
			Reason:     traceHeuristicPrefix + "retrieval records are unavailable; grounding cannot be assessed",
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
	reason := traceHeuristicPrefix + "retrieval evidence exists; grounding and citation faithfulness were not verified"
	if !hasResults {
		score = 0.4
		reason = traceHeuristicPrefix + "retrieval calls returned no documents; grounding was not verified"
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
			Reason:     traceHeuristicPrefix + "no runtime error was recorded; policy safety was not evaluated",
		}
	}

	return EvalScore{
		MetricName: "safety",
		MetricKind: "safety",
		Score:      0.5,
		Passed:     false,
		Reason:     traceHeuristicPrefix + fmt.Sprintf("runtime error was recorded: %s; policy safety was not evaluated", run.Error),
	}
}

// runStatusReason returns a human-readable reason for a run's status.
func runStatusReason(status, errStr string) string {
	if errStr != "" {
		return "Run " + status + ": " + strings.TrimPrefix(errStr, "Run ")
	}
	return "Run " + status
}
