package httpapi

import (
	"context"
	"net/http"
	"strings"

	"AgentScope/internal/ingest"
	"AgentScope/internal/runs"
)

// TokenAuth authenticates a bearer token for OTLP ingestion.
type TokenAuth interface {
	AuthenticateToken(ctx context.Context, plainToken, requiredScope string) (projectID, tokenID string, err error)
}

// RunsServiceInterface defines the subset of runs.Service that handlers need.
type RunsServiceInterface interface {
	CreateOrGetLiveRunFromTrace(ctx context.Context, projectID, traceID string, attrs map[string]string) (runs.Run, error)
	ListProjectRuns(ctx context.Context, projectID string) ([]runs.RunSummary, error)
	GetRunDetail(ctx context.Context, runID string) (runs.RunDetail, error)
}

// TracestoreInterface defines the subset of tracestore.Store that ingestion needs.
type TracestoreInterface interface {
	InsertSpans(ctx context.Context, spans []ingest.SpanRecord) error
	InsertSpanEvents(ctx context.Context, events []ingest.SpanEventRecord) error
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

		// Read and decode body
		body, err := readBody(r, 10*1024*1024) // 10MB limit
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "Failed to read request body")
			return
		}

		spanRecords, eventRecords, err := ingest.DecodeOTLPTraceRequest(body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "decode_failed", "Failed to decode OTLP trace data")
			return
		}

		// Determine trace ID and run attributes from decoded spans
		var traceID string
		var agentAttrs = make(map[string]string)
		for _, sp := range spanRecords {
			if traceID == "" && sp.TraceID != "" {
				traceID = sp.TraceID
			}
			if sp.Attributes != "" {
				if attr := extractAttr(sp.Attributes, "agentscope.run.id"); attr != "" {
					agentAttrs["agentscope.run.id"] = attr
				}
				if attr := extractAttr(sp.Attributes, "agentscope.run.name"); attr != "" {
					agentAttrs["agentscope.run.name"] = attr
				}
				if attr := extractAttr(sp.Attributes, "agentscope.agent.name"); attr != "" {
					agentAttrs["agentscope.agent.name"] = attr
				}
				if attr := extractAttr(sp.Attributes, "agentscope.agent.version"); attr != "" {
					agentAttrs["agentscope.agent.version"] = attr
				}
			}
		}

		// Create or get the run
		run, err := deps.RunsService.CreateOrGetLiveRunFromTrace(r.Context(), projectID, traceID, agentAttrs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "run_failed", "Failed to create run")
			return
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

		// Write spans and events to ClickHouse
		if err := deps.Tracestore.InsertSpans(r.Context(), spanRecords); err != nil {
			writeError(w, http.StatusInternalServerError, "storage_failed", "Failed to store spans")
			return
		}
		if err := deps.Tracestore.InsertSpanEvents(r.Context(), eventRecords); err != nil {
			writeError(w, http.StatusInternalServerError, "storage_failed", "Failed to store span events")
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"run_id": run.ID,
			"spans":  len(spanRecords),
			"events": len(eventRecords),
		})
	}
}

// extractAttr does a simple key:"value" extraction from a JSON-like attribute string.
func extractAttr(attrs, key string) string {
	prefix := `"` + key + `":`
	idx := strings.Index(attrs, prefix)
	if idx < 0 {
		return ""
	}
	start := idx + len(prefix)
	if start >= len(attrs) {
		return ""
	}
	if attrs[start] != '"' {
		return ""
	}
	start++ // skip opening quote
	end := strings.IndexByte(attrs[start:], '"')
	if end < 0 {
		return ""
	}
	return attrs[start : start+end]
}
