package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"AgentScope/internal/auth"
	"github.com/go-chi/chi/v5"
)

// AuthService defines the auth operations needed by handlers.
type AuthService interface {
	Register(ctx context.Context, email, password, displayName string) (string, error)
	Login(ctx context.Context, email, password string) (string, string, error)
}

// Project is the project type used by ProjectsService.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

// ProjectToken is returned by CreateToken.
type ProjectToken struct {
	ID          string   `json:"id"`
	ProjectID   string   `json:"project_id"`
	TokenPrefix string   `json:"token_prefix"`
	Scopes      []string `json:"scopes"`
	Description string   `json:"description,omitempty"`
}

// ProjectsService defines the project operations needed by handlers.
type ProjectsService interface {
	CreateProject(ctx context.Context, userID, name, description string) (Project, error)
	ListProjects(ctx context.Context, userID string) ([]Project, error)
	CreateToken(ctx context.Context, projectID, userID, description string, scopes []string) (plainToken string, token ProjectToken, err error)
}

// Dataset is the dataset type used by DatasetsService.
type Dataset struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// DatasetCase is the dataset case type used by DatasetsService.
type DatasetCase struct {
	ID        string          `json:"id"`
	DatasetID string          `json:"dataset_id"`
	Input     json.RawMessage `json:"input"`
	Expected  json.RawMessage `json:"expected,omitempty"`
	Metadata  json.RawMessage `json:"metadata"`
	SortOrder int             `json:"sort_order"`
	CreatedAt string          `json:"created_at"`
}

// DatasetsService defines the dataset operations needed by handlers.
type DatasetsService interface {
	CreateDataset(ctx context.Context, projectID, name, description string) (Dataset, error)
	AddCase(ctx context.Context, datasetID string, input, expected, metadata json.RawMessage) (DatasetCase, error)
	ListCases(ctx context.Context, datasetID string) ([]DatasetCase, error)
	GetDataset(ctx context.Context, datasetID string) (Dataset, error)
}

// Experiment is the experiment type used by ExperimentsService.
type Experiment struct {
	ID            string `json:"id"`
	ProjectID     string `json:"project_id"`
	DatasetID     string `json:"dataset_id"`
	EvaluatorID   string `json:"evaluator_id,omitempty"`
	TargetName    string `json:"target_name"`
	TargetVersion string `json:"target_version,omitempty"`
	Status        string `json:"status"`
	TotalCases    int    `json:"total_cases"`
	PassedCases   int    `json:"passed_cases"`
	FailedCases   int    `json:"failed_cases"`
	CreatedAt     string `json:"created_at"`
}

// ExperimentDetail provides full experiment information.
type ExperimentDetail struct {
	Experiment
	StatusCounts map[string]int `json:"status_counts"`
}

// ExperimentsService defines the experiment operations needed by handlers.
type ExperimentsService interface {
	CreateExperiment(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, caseIDs []string) (Experiment, error)
	GetExperiment(ctx context.Context, experimentID string) (ExperimentDetail, error)
}

// ClaimedJob is returned to the runner after claiming.
type ClaimedJob struct {
	JobID        string `json:"job_id"`
	Status       string `json:"status"`
	LeaseDuration string `json:"lease_duration"`
}

// SubmitResultRequest is submitted by the runner.
type SubmitResultRequest struct {
	RunID        string  `json:"run_id"`
	Score        float64 `json:"score"`
	Passed       bool    `json:"passed"`
	Reason       string  `json:"reason"`
	ActualOutput string  `json:"actual_output"`
	LatencyMs    int64   `json:"latency_ms"`
	Cost         float64 `json:"cost"`
}

// RunnerService defines the runner operations needed by handlers.
type RunnerService interface {
	Claim(ctx context.Context, projectID, runnerName string) (ClaimedJob, error)
	Heartbeat(ctx context.Context, jobID, projectID, runnerName string) error
	SubmitResult(ctx context.Context, jobID, projectID, runnerName string, req SubmitResultRequest) error
	FailJob(ctx context.Context, jobID, projectID, runnerName, errorMsg string) error
}

// ProjectAuth checks project membership for a given user.
type ProjectAuth interface {
	IsProjectMember(ctx context.Context, userID, projectID string) error
}

// Dependencies holds the dependencies injected into the router.
type Dependencies struct {
	AuthService        AuthService
	ProjectsService    ProjectsService
	TokenAuth          TokenAuth
	RunsService        RunsServiceInterface
	Tracestore         TracestoreInterface
	SessionSigner      auth.SessionSigner
	DatasetsService    DatasetsService
	ExperimentsService ExperimentsService
	RunnerService      RunnerService
	ProjectAuth        ProjectAuth
}

// ---- Auth handlers ----

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

func handleRegister(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registerRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}
		if req.Email == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "missing_fields", "Email and password are required")
			return
		}

		userID, err := deps.AuthService.Register(r.Context(), req.Email, req.Password, req.DisplayName)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				writeError(w, http.StatusConflict, "email_exists", "Email already registered")
				return
			}
			writeError(w, http.StatusBadRequest, "registration_failed", err.Error())
			return
		}

		signed, err := deps.SessionSigner.SignUserID(userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "session_error", "Failed to create session")
			return
		}

		setSessionCookie(w, signed)
		writeJSON(w, http.StatusCreated, map[string]string{
			"id":           userID,
			"email":        req.Email,
			"display_name": req.DisplayName,
		})
	}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func handleLogin(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}

		userID, displayName, err := deps.AuthService.Login(r.Context(), req.Email, req.Password)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password")
			return
		}

		signed, err := deps.SessionSigner.SignUserID(userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "session_error", "Failed to create session")
			return
		}

		setSessionCookie(w, signed)
		writeJSON(w, http.StatusOK, map[string]string{
			"id":           userID,
			"email":        req.Email,
			"display_name": displayName,
		})
	}
}

// ---- Project handlers ----

type createProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func handleCreateProject(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		var req createProjectRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "missing_name", "Project name is required")
			return
		}

		project, err := deps.ProjectsService.CreateProject(r.Context(), userID, req.Name, req.Description)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "create_failed", "Failed to create project")
			return
		}

		writeJSON(w, http.StatusCreated, project)
	}
}

func handleListProjects(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projects, err := deps.ProjectsService.ListProjects(r.Context(), userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list projects")
			return
		}

		writeJSON(w, http.StatusOK, projects)
	}
}

type createTokenRequest struct {
	Description string   `json:"description"`
	Scopes      []string `json:"scopes"`
}

func handleCreateToken(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projectID := chi.URLParam(r, "projectID")

		var req createTokenRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}

		plainToken, token, err := deps.ProjectsService.CreateToken(r.Context(), projectID, userID, req.Description, req.Scopes)
		if err != nil {
			writeError(w, http.StatusBadRequest, "token_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"plain_token": plainToken,
			"token":       token,
		})
	}
}
