package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"AgentScope/internal/realtime"
	"github.com/go-chi/chi/v5"
)

func handleRealtimeStream(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		projectID := chi.URLParam(r, "projectID")
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "missing_project", "Project ID is required")
			return
		}
		if deps.ProjectAuth == nil ||
			deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID) != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a member of this project")
			return
		}
		if deps.RealtimeSubscriber == nil {
			writeError(w, http.StatusServiceUnavailable, "realtime_unavailable", "Realtime stream is unavailable")
			return
		}
		if deps.QuotaService != nil {
			decision := deps.QuotaService.AcquireSSE(r.Context(), projectID)
			writeQuotaHeaders(w, decision)
			if !decision.Allowed {
				if decision.Degraded {
					writeError(w, http.StatusServiceUnavailable, "quota_unavailable", "Realtime quota service is unavailable")
				} else {
					writeError(w, http.StatusTooManyRequests, "quota_exceeded", "Project realtime connection quota exceeded")
				}
				return
			}
			defer deps.QuotaService.ReleaseSSE(context.WithoutCancel(r.Context()), projectID)
		}

		filter := realtime.SubscriptionFilter{
			RunID:   strings.TrimSpace(r.URL.Query().Get("run_id")),
			AgentID: strings.TrimSpace(r.URL.Query().Get("agent_id")),
			Types:   parseRealtimeTypes(r.URL.Query().Get("types")),
		}
		items, err := deps.RealtimeSubscriber.Subscribe(
			r.Context(),
			projectID,
			r.Header.Get("Last-Event-ID"),
			filter,
		)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "realtime_unavailable", "Realtime stream is unavailable")
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming_unsupported", "Streaming is unsupported")
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		heartbeat := deps.RealtimeHeartbeat
		if heartbeat <= 0 {
			heartbeat = 15 * time.Second
		}
		ticker := time.NewTicker(heartbeat)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				_, _ = fmt.Fprint(w, ": heartbeat\n\n")
				flusher.Flush()
			case item, open := <-items:
				if !open {
					return
				}
				switch {
				case item.Err != nil:
					writeSSEData(w, "", "error", map[string]string{"code": "realtime_stream_error"})
					flusher.Flush()
					return
				case item.Reset:
					writeSSEData(w, "", "reset", map[string]string{"reason": item.ResetReason})
				case item.Event != nil:
					writeSSEData(w, fmt.Sprint(item.Event.ID), item.Event.Type, item.Event)
				default:
					continue
				}
				flusher.Flush()
			}
		}
	}
}

func parseRealtimeTypes(raw string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if value != "" && len(result) < 50 {
			result[value] = struct{}{}
		}
	}
	return result
}

func writeSSEData(w http.ResponseWriter, id, eventType string, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		body = []byte(`{"code":"event_encode_failed"}`)
		eventType = "error"
	}
	if id != "" {
		_, _ = fmt.Fprintf(w, "id: %s\n", id)
	}
	_, _ = fmt.Fprintf(w, "event: %s\n", eventType)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
}
