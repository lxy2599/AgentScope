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

// traceIngestGroup is the request-local unit that maps one OTLP trace to one
// AgentScope run. An OTLP export request is only a transport batch and may
// legitimately contain spans from several traces.
type traceIngestGroup struct {
	traceID string
	attrs   map[string]string
	runID   string

	spans        []ingest.SpanRecord
	events       []ingest.SpanEventRecord
	llm          []ingest.LLMCallRecord
	tools        []ingest.ToolCallRecord
	retrievals   []ingest.RetrievalCallRecord
	skills       []ingest.SkillCallRecord
	thoughts     []ingest.ThoughtRecord
	finalAnswers []ingest.FinalAnswerRecord
}

// groupTraceRecords preserves first-seen trace order while separating the
// records in a transport-level OTLP batch by their business trace identity.
func groupTraceRecords(spans []ingest.SpanRecord, events []ingest.SpanEventRecord) []traceIngestGroup {
	// Preserve the existing allocation profile for the common case where an
	// exporter sends one trace per request (including the load-test workload).
	if len(spans) > 0 {
		traceID := spans[0].TraceID
		singleTrace := true
		for i := 1; i < len(spans); i++ {
			if spans[i].TraceID != traceID {
				singleTrace = false
				break
			}
		}
		if singleTrace {
			for i := range events {
				if events[i].TraceID != traceID {
					singleTrace = false
					break
				}
			}
		}
		if singleTrace {
			attrs := make(map[string]string)
			for i := range spans {
				mergeRunAttributes(attrs, spans[i].Attributes)
			}
			return []traceIngestGroup{{
				traceID: traceID,
				attrs:   attrs,
				spans:   spans,
				events:  events,
			}}
		}
	}

	groups := make([]traceIngestGroup, 0)
	groupIndex := make(map[string]int, len(spans))
	ensureGroup := func(traceID string) int {
		if index, ok := groupIndex[traceID]; ok {
			return index
		}
		index := len(groups)
		groupIndex[traceID] = index
		groups = append(groups, traceIngestGroup{
			traceID: traceID,
			attrs:   make(map[string]string),
		})
		return index
	}

	for _, span := range spans {
		index := ensureGroup(span.TraceID)
		groups[index].spans = append(groups[index].spans, span)
		mergeRunAttributes(groups[index].attrs, span.Attributes)
	}
	for _, event := range events {
		index := ensureGroup(event.TraceID)
		groups[index].events = append(groups[index].events, event)
	}
	return groups
}

func mergeRunAttributes(target map[string]string, rawAttributes string) {
	if rawAttributes == "" {
		return
	}
	parsed := ingest.MustParseAttributes(rawAttributes)
	for _, key := range []string{
		"agentscope.run.id",
		"agentscope.run.name",
		"agentscope.agent.name",
		"agentscope.agent.version",
	} {
		if value := ingest.AttrString(parsed, key); value != "" {
			target[key] = value
		}
	}
}

func (g *traceIngestGroup) assignOwnership(projectID, runID string) {
	g.runID = runID
	for i := range g.spans {
		g.spans[i].ProjectID = projectID
		g.spans[i].RunID = runID
	}
	for i := range g.events {
		g.events[i].ProjectID = projectID
		g.events[i].RunID = runID
	}
	g.llm, g.tools, g.retrievals, g.skills, g.thoughts, g.finalAnswers = ingest.ExtractStructuredCalls(g.spans)
}

func (g *traceIngestGroup) semanticCount() int {
	return len(g.llm) + len(g.tools) + len(g.retrievals) + len(g.skills) + len(g.thoughts) + len(g.finalAnswers)
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

		// Record request-level health before removing SDK exporter metrics from
		// the business trace path.
		health.SpanCount = len(spanRecords)
		health.EventCount = len(eventRecords)
		requestHasTraceID := false
		for _, sp := range spanRecords {
			if sp.TraceID != "" {
				requestHasTraceID = true
				break
			}
		}
		if metrics := ingest.ExtractSDKExportMetrics(spanRecords); metrics != nil {
			health.SDKQueueDepth = metrics.QueueDepth
			health.SDKQueueCapacity = metrics.QueueCapacity
			health.SDKSpansDropped = metrics.SpansDropped
			health.SDKExportRetries = metrics.ExportRetries
			health.SDKExportLatencyMs = metrics.ExportLatencyMs
		}

		// SDK exporter health belongs to ingest health, not to a business run.
		// Filtering before trace/run resolution also keeps a metrics span at the
		// front of a mixed OTLP batch from hijacking the business trace ID.
		spanRecords, eventRecords = ingest.ExcludeSDKExportMetrics(spanRecords, eventRecords)
		if len(spanRecords) == 0 {
			health.MissingTraceID = !requestHasTraceID
			health.MissingRunID = true
			health.Outcome = ingest.HealthAccepted
			health.HTTPStatus = http.StatusOK
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"run_id":        "",
				"run_ids":       []string{},
				"spans":         0,
				"events":        0,
				"llm":           0,
				"tools":         0,
				"retrievals":    0,
				"skills":        0,
				"thoughts":      0,
				"final_answers": 0,
			})
			return
		}

		// An OTLP request is a transport batch, not a business-run boundary. Resolve
		// each trace independently, then merge the correctly-owned records back into
		// one storage batch so ClickHouse batching remains efficient.
		groups := groupTraceRecords(spanRecords, eventRecords)
		runIDs := make([]string, 0, len(groups))
		var healthAgentName string
		mixedAgentNames := false
		for i := range groups {
			group := &groups[i]
			health.MissingTraceID = health.MissingTraceID || group.traceID == ""
			health.MissingRunID = health.MissingRunID || group.attrs["agentscope.run.id"] == ""
			if agentName := group.attrs["agentscope.agent.name"]; agentName != "" {
				if healthAgentName == "" {
					healthAgentName = agentName
				} else if healthAgentName != agentName {
					mixedAgentNames = true
				}
			}

			run, err := deps.RunsService.CreateOrGetLiveRunFromTrace(r.Context(), projectID, group.traceID, group.attrs)
			if err != nil {
				health.ErrorCode = "run_failed"
				writeError(w, http.StatusInternalServerError, "run_failed", "Failed to create run")
				return
			}
			runIDs = append(runIDs, run.ID)

			// If the run was created with "unknown" agent_name and this trace has
			// agent info, update it without failing ingestion on enrichment errors.
			if agentName := group.attrs["agentscope.agent.name"]; agentName != "" {
				_, _ = deps.RunsService.UpdateRunFromTrace(r.Context(), run.ID, agentName, group.attrs["agentscope.agent.version"])
			}
			group.assignOwnership(projectID, run.ID)
			health.SemanticCount += group.semanticCount()
		}
		if !mixedAgentNames {
			health.AgentName = healthAgentName
		}

		// Start with the first group's slices so the common single-trace path does
		// not copy records after ownership assignment. Additional trace groups are
		// appended only for genuine multi-trace transport batches.
		spanRecords = groups[0].spans
		eventRecords = groups[0].events
		llmCalls := groups[0].llm
		toolCalls := groups[0].tools
		retrievalCalls := groups[0].retrievals
		skillCalls := groups[0].skills
		thoughts := groups[0].thoughts
		finalAnswers := groups[0].finalAnswers
		for i := 1; i < len(groups); i++ {
			group := &groups[i]
			spanRecords = append(spanRecords, group.spans...)
			eventRecords = append(eventRecords, group.events...)
			llmCalls = append(llmCalls, group.llm...)
			toolCalls = append(toolCalls, group.tools...)
			retrievalCalls = append(retrievalCalls, group.retrievals...)
			skillCalls = append(skillCalls, group.skills...)
			thoughts = append(thoughts, group.thoughts...)
			finalAnswers = append(finalAnswers, group.finalAnswers...)
		}

		// Write via the TraceBatchWriter abstraction (fall back to direct ClickHouse write if not set)
		if err := writeTraceBatch(r.Context(), deps, spanRecords, eventRecords, llmCalls, toolCalls, retrievalCalls, skillCalls, thoughts, finalAnswers); err != nil {
			health.ErrorCode = "storage_failed"
			writeError(w, http.StatusInternalServerError, "storage_failed", "Failed to store trace data")
			return
		}
		if deps.RealtimeOutbox != nil {
			for i := range groups {
				group := &groups[i]
				if group.semanticCount() == 0 {
					continue
				}
				payload, err := json.Marshal(map[string]int{
					"span_count":     len(group.spans),
					"semantic_count": group.semanticCount(),
					"llm_calls":      len(group.llm),
					"tool_calls":     len(group.tools),
					"retrievals":     len(group.retrievals),
					"skills":         len(group.skills),
					"thoughts":       len(group.thoughts),
					"final_answers":  len(group.finalAnswers),
				})
				if err == nil {
					_, err = deps.RealtimeOutbox.Enqueue(r.Context(), realtime.Event{
						SchemaVersion:   realtime.SchemaVersionV1,
						ProjectID:       projectID,
						RunID:           group.runID,
						Type:            realtime.EventRunSemanticAppended,
						OccurredAt:      time.Now().UTC(),
						Payload:         payload,
						PayloadRedacted: true,
					})
				}
				if err != nil {
					log.Printf("enqueue semantic realtime event for run %s: %v", group.runID, err)
				}
			}
		}
		health.Outcome = ingest.HealthAccepted
		health.HTTPStatus = http.StatusOK

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"run_id":        runIDs[0],
			"run_ids":       runIDs,
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
