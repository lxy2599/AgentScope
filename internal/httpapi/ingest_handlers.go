package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"AgentScope/internal/ingest"
	"AgentScope/internal/quotas"
	"AgentScope/internal/realtime"
	"AgentScope/internal/runs"
)

// TokenAuth authenticates a bearer token for OTLP ingestion.
type TokenAuth interface {
	AuthenticateToken(ctx context.Context, plainToken, requiredScope string) (projectID, tokenID string, err error)
}

type RealtimeOutboxWriter interface {
	Enqueue(ctx context.Context, event realtime.Event) (realtime.OutboxRecord, error)
}

// RunsServiceInterface defines the subset of runs.Service that handlers need.
type RunsServiceInterface interface {
	CreateOrGetLiveRunFromTrace(ctx context.Context, projectID, traceID string, attrs map[string]string) (runs.Run, error)
	ListProjectRuns(ctx context.Context, projectID string) ([]runs.RunSummary, error)
	GetRunDetail(ctx context.Context, runID string) (runs.RunDetail, error)
	GetRun(ctx context.Context, runID string) (runs.Run, error)
	GetRunByTraceID(ctx context.Context, traceID, projectID string) (runs.Run, error)
	UpdateRunFromTrace(ctx context.Context, runID, agentName, agentVersion string) (runs.Run, error)
	FailRunByRunner(ctx context.Context, runID, projectID, error string) error
}

// TracestoreInterface defines the subset of tracestore.Store that ingestion needs.
type TracestoreInterface interface {
	InsertSpans(ctx context.Context, spans []ingest.SpanRecord) error
	InsertSpanEvents(ctx context.Context, events []ingest.SpanEventRecord) error
	InsertLLMCalls(ctx context.Context, calls []ingest.LLMCallRecord) error
	InsertToolCalls(ctx context.Context, calls []ingest.ToolCallRecord) error
	InsertRetrievalCalls(ctx context.Context, calls []ingest.RetrievalCallRecord) error
	InsertSkillCalls(ctx context.Context, calls []ingest.SkillCallRecord) error
	InsertThoughts(ctx context.Context, thoughts []ingest.ThoughtRecord) error
	InsertFinalAnswers(ctx context.Context, answers []ingest.FinalAnswerRecord) error
}

// writeTraceBatch writes all trace records through the configured TraceBatchWriter,
// falling back to direct TracestoreInterface calls if no writer is set.
func writeTraceBatch(
	ctx context.Context,
	deps Dependencies,
	spans []ingest.SpanRecord,
	events []ingest.SpanEventRecord,
	llmCalls []ingest.LLMCallRecord,
	toolCalls []ingest.ToolCallRecord,
	retrievalCalls []ingest.RetrievalCallRecord,
	skillCalls []ingest.SkillCallRecord,
	thoughtCalls []ingest.ThoughtRecord,
	finalAnswerCalls []ingest.FinalAnswerRecord,
) error {
	if deps.TraceWriter != nil {
		batch := ingest.NewTraceBatch(spans, events, llmCalls, toolCalls, retrievalCalls, skillCalls, thoughtCalls, finalAnswerCalls)
		return deps.TraceWriter.WriteBatch(ctx, batch)
	}
	// Fall back to direct ClickHouse writes via the old TracestoreInterface
	if err := deps.Tracestore.InsertSpans(ctx, spans); err != nil {
		return err
	}
	if err := deps.Tracestore.InsertSpanEvents(ctx, events); err != nil {
		return err
	}
	if err := deps.Tracestore.InsertLLMCalls(ctx, llmCalls); err != nil {
		return err
	}
	if err := deps.Tracestore.InsertToolCalls(ctx, toolCalls); err != nil {
		return err
	}
	if err := deps.Tracestore.InsertRetrievalCalls(ctx, retrievalCalls); err != nil {
		return err
	}
	if err := deps.Tracestore.InsertSkillCalls(ctx, skillCalls); err != nil {
		return err
	}
	if err := deps.Tracestore.InsertThoughts(ctx, thoughtCalls); err != nil {
		return err
	}
	if err := deps.Tracestore.InsertFinalAnswers(ctx, finalAnswerCalls); err != nil {
		return err
	}
	return nil
}

// handleIngest handles POST /v1/traces for OTLP trace ingestion.
func handleIngest(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract bearer token
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || len(authHeader) < 7 || authHeader[:7] != "Bearer " {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Missing or invalid authorization header")
			return
		}
		plainToken := authHeader[7:]

		projectID, _, err := deps.TokenAuth.AuthenticateToken(r.Context(), plainToken, "otlp:write")
		if err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Token lacks required scope")
			return
		}

		startedAt := time.Now()
		health := ingest.HealthEvent{
			ProjectID:  projectID,
			Outcome:    ingest.HealthStorageFailed,
			HTTPStatus: http.StatusInternalServerError,
			OccurredAt: startedAt,
		}
		defer func() {
			if deps.IngestHealth == nil {
				return
			}
			health.Duration = time.Since(startedAt)
			if err := deps.IngestHealth.RecordHealthEvent(r.Context(), health); err != nil {
				log.Printf("record ingest health event for project %s: %v", projectID, err)
			}
		}()

		// Check project-level rate limit
		if deps.QuotaService != nil {
			decision := deps.QuotaService.CheckIngest(
				r.Context(), projectID, quotas.DimensionIngestRequests, 1,
			)
			writeQuotaHeaders(w, decision)
			if decision.Degraded {
				health.ErrorCode = "quota_backend_unavailable"
			}
			if !decision.Allowed {
				health.Outcome = ingest.HealthRateLimited
				health.HTTPStatus = http.StatusTooManyRequests
				health.ErrorCode = "quota_ingest_requests"
				writeError(w, http.StatusTooManyRequests, "quota_exceeded", "Project ingest request quota exceeded")
				return
			}
			if r.ContentLength > 0 {
				decision = deps.QuotaService.CheckIngest(
					r.Context(), projectID, quotas.DimensionIngestBytes, r.ContentLength,
				)
				writeQuotaHeaders(w, decision)
				if decision.Degraded {
					health.ErrorCode = "quota_backend_unavailable"
				}
				if !decision.Allowed {
					health.Outcome = ingest.HealthRateLimited
					health.HTTPStatus = http.StatusTooManyRequests
					health.ErrorCode = "quota_ingest_bytes"
					writeError(w, http.StatusTooManyRequests, "quota_exceeded", "Project ingest byte quota exceeded")
					return
				}
			}
		} else if deps.RateLimiter != nil {
			if !deps.RateLimiter.Allow(projectID, 1) {
				health.Outcome = ingest.HealthRateLimited
				health.HTTPStatus = http.StatusTooManyRequests
				health.ErrorCode = "rate_limited"
				writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many ingest requests. Try again later.")
				return
			}
		}

		// Read and decode body (supports gzip decompression, enforces size limit)
		maxBodySize := deps.MaxBodySize
		if maxBodySize <= 0 {
			maxBodySize = ingest.MaxBodySize
		}
		body, err := ingest.ReadAndDecodeBody(r, maxBodySize)
		if err != nil {
			if err == ingest.ErrBodyTooLarge {
				health.Outcome = ingest.HealthRejectedBody
				health.HTTPStatus = http.StatusRequestEntityTooLarge
				health.ErrorCode = "body_too_large"
				writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body exceeds maximum size")
				return
			}
			health.Outcome = ingest.HealthRejectedBody
			health.HTTPStatus = http.StatusBadRequest
			health.ErrorCode = "invalid_body"
			writeError(w, http.StatusBadRequest, "invalid_body", "Failed to read request body")
			return
		}
		if deps.QuotaService != nil {
			remainingBytes := int64(len(body))
			if r.ContentLength > 0 {
				remainingBytes -= r.ContentLength
			}
			if remainingBytes > 0 {
				decision := deps.QuotaService.CheckIngest(
					r.Context(), projectID, quotas.DimensionIngestBytes, remainingBytes,
				)
				writeQuotaHeaders(w, decision)
				if decision.Degraded {
					health.ErrorCode = "quota_backend_unavailable"
				}
				if !decision.Allowed {
					health.Outcome = ingest.HealthRateLimited
					health.HTTPStatus = http.StatusTooManyRequests
					health.ErrorCode = "quota_ingest_bytes"
					writeError(w, http.StatusTooManyRequests, "quota_exceeded", "Project ingest byte quota exceeded")
					return
				}
			}
		}

		maxSpans := deps.MaxSpans
		if maxSpans <= 0 {
			maxSpans = ingest.MaxSpans
		}
		spanRecords, eventRecords, err := ingest.DecodeOTLPTraceRequest(body, maxSpans)
		if err != nil {
			if errors.Is(err, ingest.ErrTooManySpans) {
				health.Outcome = ingest.HealthRejectedDecode
				health.HTTPStatus = http.StatusRequestEntityTooLarge
				health.ErrorCode = "too_many_spans"
				writeError(w, http.StatusRequestEntityTooLarge, "too_many_spans", "Trace request exceeds maximum span count")
				return
			}
			health.Outcome = ingest.HealthRejectedDecode
			health.HTTPStatus = http.StatusBadRequest
			health.ErrorCode = "decode_failed"
			writeError(w, http.StatusBadRequest, "decode_failed", "Failed to decode OTLP trace data")
			return
		}
		if deps.QuotaService != nil {
			decision := deps.QuotaService.CheckIngest(
				r.Context(), projectID, quotas.DimensionIngestSpans, int64(len(spanRecords)),
			)
			writeQuotaHeaders(w, decision)
			if decision.Degraded {
				health.ErrorCode = "quota_backend_unavailable"
			}
			if !decision.Allowed {
				health.Outcome = ingest.HealthRateLimited
				health.HTTPStatus = http.StatusTooManyRequests
				health.ErrorCode = "quota_ingest_spans"
				writeError(w, http.StatusTooManyRequests, "quota_exceeded", "Project ingest span quota exceeded")
				return
			}
		}

		// Determine trace ID and run attributes from decoded spans
		var traceID string
		var agentAttrs = make(map[string]string)
		for _, sp := range spanRecords {
			if traceID == "" && sp.TraceID != "" {
				traceID = sp.TraceID
			}
			if sp.Attributes != "" {
				parsed := ingest.MustParseAttributes(sp.Attributes)
				if attr := ingest.AttrString(parsed, "agentscope.run.id"); attr != "" {
					agentAttrs["agentscope.run.id"] = attr
				}
				if attr := ingest.AttrString(parsed, "agentscope.run.name"); attr != "" {
					agentAttrs["agentscope.run.name"] = attr
				}
				if attr := ingest.AttrString(parsed, "agentscope.agent.name"); attr != "" {
					agentAttrs["agentscope.agent.name"] = attr
				}
				if attr := ingest.AttrString(parsed, "agentscope.agent.version"); attr != "" {
					agentAttrs["agentscope.agent.version"] = attr
				}
			}
		}
		health.SpanCount = len(spanRecords)
		health.EventCount = len(eventRecords)
		health.MissingTraceID = traceID == ""
		health.MissingRunID = agentAttrs["agentscope.run.id"] == ""
		health.AgentName = agentAttrs["agentscope.agent.name"]
		if metrics := ingest.ExtractSDKExportMetrics(spanRecords); metrics != nil {
			health.SDKQueueDepth = metrics.QueueDepth
			health.SDKQueueCapacity = metrics.QueueCapacity
			health.SDKSpansDropped = metrics.SpansDropped
			health.SDKExportRetries = metrics.ExportRetries
			health.SDKExportLatencyMs = metrics.ExportLatencyMs
		}

		// Create or get the run
		run, err := deps.RunsService.CreateOrGetLiveRunFromTrace(r.Context(), projectID, traceID, agentAttrs)
		if err != nil {
			health.ErrorCode = "run_failed"
			writeError(w, http.StatusInternalServerError, "run_failed", "Failed to create run")
			return
		}

		// If the run was created with "unknown" agent_name and this batch has agent info, update it
		if agentAttrs["agentscope.agent.name"] != "" {
			if _, err := deps.RunsService.UpdateRunFromTrace(r.Context(), run.ID, agentAttrs["agentscope.agent.name"], agentAttrs["agentscope.agent.version"]); err != nil {
				// Non-fatal: log but don't fail the request
			}
		}

		// Assign ProjectID and RunID to all span and event records
		for i := range spanRecords {
			spanRecords[i].ProjectID = projectID
			spanRecords[i].RunID = run.ID
		}
		for i := range eventRecords {
			eventRecords[i].ProjectID = projectID
			eventRecords[i].RunID = run.ID
		}

		// Extract and write structured call records
		llmCalls, toolCalls, retrievalCalls, skillCalls, thoughts, finalAnswers := ingest.ExtractStructuredCalls(spanRecords)
		health.SemanticCount = len(llmCalls) + len(toolCalls) + len(retrievalCalls) + len(skillCalls) + len(thoughts) + len(finalAnswers)

		// Write via the TraceBatchWriter abstraction (fall back to direct ClickHouse write if not set)
		if err := writeTraceBatch(r.Context(), deps, spanRecords, eventRecords, llmCalls, toolCalls, retrievalCalls, skillCalls, thoughts, finalAnswers); err != nil {
			health.ErrorCode = "storage_failed"
			writeError(w, http.StatusInternalServerError, "storage_failed", "Failed to store trace data")
			return
		}
		if health.SemanticCount > 0 && deps.RealtimeOutbox != nil {
			payload, err := json.Marshal(map[string]int{
				"span_count":     len(spanRecords),
				"semantic_count": health.SemanticCount,
				"llm_calls":      len(llmCalls),
				"tool_calls":     len(toolCalls),
				"retrievals":     len(retrievalCalls),
				"skills":         len(skillCalls),
				"thoughts":       len(thoughts),
				"final_answers":  len(finalAnswers),
			})
			if err == nil {
				_, err = deps.RealtimeOutbox.Enqueue(r.Context(), realtime.Event{
					SchemaVersion:   realtime.SchemaVersionV1,
					ProjectID:       projectID,
					RunID:           run.ID,
					Type:            realtime.EventRunSemanticAppended,
					OccurredAt:      time.Now().UTC(),
					Payload:         payload,
					PayloadRedacted: true,
				})
			}
			if err != nil {
				log.Printf("enqueue semantic realtime event for run %s: %v", run.ID, err)
			}
		}
		health.Outcome = ingest.HealthAccepted
		health.HTTPStatus = http.StatusOK

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"run_id":        run.ID,
			"spans":         len(spanRecords),
			"events":        len(eventRecords),
			"llm":           len(llmCalls),
			"tools":         len(toolCalls),
			"retrievals":    len(retrievalCalls),
			"skills":        len(skillCalls),
			"thoughts":      len(thoughts),
			"final_answers": len(finalAnswers),
		})
	}
}
