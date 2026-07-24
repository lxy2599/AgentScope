package httpapi

import (
	"errors"
	"net/http"

	"AgentScope/internal/snapshots"
	"github.com/go-chi/chi/v5"
)

func handleGetRunSnapshot(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
			return
		}
		runID := chi.URLParam(r, "runID")
		if runID == "" {
			writeError(w, http.StatusBadRequest, "missing_run_id", "Run ID is required")
			return
		}
		if deps.SnapshotService == nil || deps.RunsService == nil {
			writeError(w, http.StatusServiceUnavailable, "snapshot_unavailable", "Run snapshot is unavailable")
			return
		}

		run, err := deps.RunsService.GetRun(r.Context(), runID)
		if err != nil {
			writeError(w, http.StatusNotFound, "run_not_found", "Run not found")
			return
		}
		if deps.ProjectAuth == nil ||
			deps.ProjectAuth.IsProjectMember(r.Context(), userID, run.ProjectID) != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a member of this project")
			return
		}

		snapshot, err := deps.SnapshotService.Get(r.Context(), runID, run.ProjectID)
		if errors.Is(err, snapshots.ErrUnavailable) {
			writeError(w, http.StatusNotFound, "snapshot_unavailable", "This run predates immutable snapshots")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "snapshot_failed", "Failed to load run snapshot")
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	}
}
