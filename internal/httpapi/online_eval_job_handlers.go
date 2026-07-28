package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"AgentScope/internal/onlinerval"
	"github.com/go-chi/chi/v5"
)

func requireOnlineEvalJobs(w http.ResponseWriter, deps Dependencies) bool {
	if deps.OnlineEvalJobs == nil {
		writeError(w, http.StatusServiceUnavailable, "online_eval_unavailable", "Online evaluation jobs are not configured")
		return false
	}
	return true
}
func requireOnlineEvalProjectMember(w http.ResponseWriter, r *http.Request, deps Dependencies, projectID string) bool {
	if userIDFromCtx(r.Context()) == "" || deps.ProjectAuth == nil || deps.ProjectAuth.IsProjectMember(r.Context(), userIDFromCtx(r.Context()), projectID) != nil {
		writeError(w, http.StatusForbidden, "project_forbidden", "Project membership is required")
		return false
	}
	return true
}
func handleListOnlineEvalJobs(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireOnlineEvalJobs(w, deps) {
			return
		}
		projectID := chi.URLParam(r, "projectID")
		if !requireOnlineEvalProjectMember(w, r, deps, projectID) {
			return
		}
		limit := 100
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if value, err := strconv.Atoi(raw); err == nil {
				limit = value
			}
		}
		jobs, err := deps.OnlineEvalJobs.List(r.Context(), projectID, r.URL.Query().Get("run_id"), limit)
		if err != nil {
			writeInternalError(w, "list_online_eval_jobs_failed", "Failed to list online evaluation jobs", "list online evaluation jobs", err)
			return
		}
		writeJSON(w, http.StatusOK, jobs)
	}
}
func handleGetOnlineEvalJob(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireOnlineEvalJobs(w, deps) {
			return
		}
		projectID := chi.URLParam(r, "projectID")
		if !requireOnlineEvalProjectMember(w, r, deps, projectID) {
			return
		}
		job, err := deps.OnlineEvalJobs.Get(r.Context(), projectID, chi.URLParam(r, "jobID"))
		if errors.Is(err, onlinerval.ErrJobNotFound) {
			writeError(w, http.StatusNotFound, "online_eval_job_not_found", "Online evaluation job was not found")
			return
		}
		if err != nil {
			writeInternalError(w, "get_online_eval_job_failed", "Failed to get online evaluation job", "get online evaluation job", err)
			return
		}
		writeJSON(w, http.StatusOK, job)
	}
}
func handleRetryOnlineEvalJob(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireOnlineEvalJobs(w, deps) {
			return
		}
		projectID := chi.URLParam(r, "projectID")
		if !requireOnlineEvalProjectMember(w, r, deps, projectID) {
			return
		}
		job, err := deps.OnlineEvalJobs.Retry(r.Context(), projectID, chi.URLParam(r, "jobID"))
		if errors.Is(err, onlinerval.ErrJobNotFound) {
			writeError(w, http.StatusNotFound, "online_eval_job_not_found", "Online evaluation job was not found")
			return
		}
		if errors.Is(err, onlinerval.ErrJobNotRetryable) {
			writeError(w, http.StatusConflict, "online_eval_job_not_retryable", "Only failed jobs can be retried")
			return
		}
		if err != nil {
			writeInternalError(w, "retry_online_eval_job_failed", "Failed to retry online evaluation job", "retry online evaluation job", err)
			return
		}
		writeJSON(w, http.StatusOK, job)
	}
}
