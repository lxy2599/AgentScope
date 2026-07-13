# AgentScope Observability Eval MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first end-to-end AgentScope MVP: project tokens, OTLP trace ingestion, run inspection APIs, datasets, experiments, remote Python runner execution, rule and LLM judge evaluators, and a minimal React UI that links failed eval cases to traces.

**Architecture:** Use a Go monolith with PostgreSQL for auth, projects, runs, spans, datasets, jobs, and results. Use a separate Python SDK/runner package for target registration, OTel export, job claiming, evaluator execution, and result submission. Serve a Vite React UI from the Go service in production while keeping frontend development independent.

**Tech Stack:** Go 1.26, `chi`, `pgx`, `sqlc`, `goose`, PostgreSQL, React, TypeScript, Vite, TanStack Query, React Router, Tailwind CSS, Python 3.11+, OpenTelemetry Python SDK, Pydantic, Typer, httpx, pytest, Playwright.

## Global Constraints

- Keep the backend deployable as one Go service.
- Use PostgreSQL as the only persistent database.
- Use project tokens for OTLP ingestion and runner access.
- Use cookie sessions and bcrypt for user login.
- Do not execute arbitrary user agent code inside the Go service.
- The Python runner must execute user targets in the user's own environment.
- Persist both live runs and eval case runs through the same `runs` model.
- Support both deterministic rule evaluators and LLM judge evaluators.
- Store provider secrets through environment variable references, not plaintext database values.
- Make each task independently testable and commit after each task.

---

## File Structure

Create this structure:

```text
cmd/agentscope-server/main.go
internal/app/app.go
internal/config/config.go
internal/httpapi/router.go
internal/httpapi/middleware.go
internal/httpapi/respond.go
internal/auth/service.go
internal/projects/service.go
internal/runs/service.go
internal/ingest/otlp.go
internal/datasets/service.go
internal/experiments/service.go
internal/runnerapi/service.go
internal/evaluators/service.go
internal/store/db.go
internal/store/queries.sql.go
internal/testutil/postgres.go
migrations/00001_init.sql
queries/*.sql
web/package.json
web/vite.config.ts
web/src/*
python/agentscope_sdk/*
python/tests/*
docker-compose.yml
Makefile
```

Responsibilities:

- `cmd/agentscope-server`: process entrypoint only.
- `internal/config`: environment parsing and defaults.
- `internal/app`: dependency wiring and lifecycle.
- `internal/httpapi`: routing, middleware, request decoding, response helpers.
- `internal/store`: database connection and generated query access.
- Feature packages own business rules and call the store layer.
- `migrations`: schema definition.
- `queries`: SQL consumed by `sqlc`.
- `web`: React UI.
- `python/agentscope_sdk`: Python SDK and runner.

---

### Task 1: Repository Baseline and Tooling

**Files:**
- Modify: `go.mod`
- Create: `.gitignore`
- Create: `Makefile`
- Create: `docker-compose.yml`
- Create: `sqlc.yaml`
- Create: `cmd/agentscope-server/main.go`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config`, `config.Load(env map[string]string) (Config, error)`.
- Produces: `cmd/agentscope-server` binary that prints a startup line and exits cleanly until Task 3 wires the server.

- [ ] **Step 1: Write the failing config test**

Create `internal/config/config_test.go`:

```go
package config

import "testing"

func TestLoadUsesRequiredDatabaseURL(t *testing.T) {
	_, err := Load(map[string]string{
		"AGENTSCOPE_SESSION_SECRET": "test-secret",
	})
	if err == nil {
		t.Fatal("expected missing database url error")
	}
}

func TestLoadParsesDefaults(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":    "postgres://agentscope:agentscope@localhost:5432/agentscope?sslmode=disable",
		"AGENTSCOPE_SESSION_SECRET": "test-secret",
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config`

Expected: FAIL with `package AgentScope/internal/config is not in std` or `undefined: Load`.

- [ ] **Step 3: Add baseline files**

Create `.gitignore`:

```gitignore
.idea/
.env
bin/
dist/
web/node_modules/
web/dist/
python/.venv/
python/.pytest_cache/
__pycache__/
*.pyc
coverage.out
```

Replace `go.mod` with:

```go
module AgentScope

go 1.26

require (
	github.com/go-chi/chi/v5 v5.2.2
	github.com/jackc/pgx/v5 v5.7.6
	golang.org/x/crypto v0.39.0
)
```

Create `internal/config/config.go`:

```go
package config

import (
	"errors"
	"fmt"
)

type Config struct {
	HTTPAddr      string
	DatabaseURL   string
	SessionSecret string
}

func Load(env map[string]string) (Config, error) {
	cfg := Config{
		HTTPAddr:      value(env, "AGENTSCOPE_HTTP_ADDR", ":8080"),
		DatabaseURL:   env["AGENTSCOPE_DATABASE_URL"],
		SessionSecret: env["AGENTSCOPE_SESSION_SECRET"],
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("AGENTSCOPE_DATABASE_URL is required")
	}
	if cfg.SessionSecret == "" {
		return Config{}, errors.New("AGENTSCOPE_SESSION_SECRET is required")
	}
	if len(cfg.SessionSecret) < 8 {
		return Config{}, fmt.Errorf("AGENTSCOPE_SESSION_SECRET must be at least 8 characters")
	}
	return cfg, nil
}

func value(env map[string]string, key string, fallback string) string {
	if env[key] != "" {
		return env[key]
	}
	return fallback
}
```

Create `cmd/agentscope-server/main.go`:

```go
package main

import (
	"fmt"
	"os"

	"AgentScope/internal/config"
)

func main() {
	cfg, err := config.Load(environ())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("agentscope server configured for %s\n", cfg.HTTPAddr)
}

func environ() map[string]string {
	env := map[string]string{}
	for _, key := range []string{"AGENTSCOPE_HTTP_ADDR", "AGENTSCOPE_DATABASE_URL", "AGENTSCOPE_SESSION_SECRET"} {
		env[key] = os.Getenv(key)
	}
	return env
}
```

Create `Makefile`:

```makefile
.PHONY: test server db-up db-down

test:
	go test ./...

server:
	go run ./cmd/agentscope-server

db-up:
	docker compose up -d postgres

db-down:
	docker compose down
```

Create `docker-compose.yml`:

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_DB: agentscope
      POSTGRES_USER: agentscope
      POSTGRES_PASSWORD: agentscope
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U agentscope -d agentscope"]
      interval: 5s
      timeout: 3s
      retries: 20
```

Create `sqlc.yaml`:

```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "queries"
    schema: "migrations"
    gen:
      go:
        package: "store"
        out: "internal/store"
        sql_package: "pgx/v5"
```

- [ ] **Step 4: Run tests**

Run: `go mod tidy && go test ./...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add .gitignore Makefile docker-compose.yml sqlc.yaml go.mod go.sum cmd internal/config
git commit -m "chore: add repository baseline"
```

---

### Task 2: Database Schema and Store Access

**Files:**
- Create: `migrations/00001_init.sql`
- Create: `queries/auth.sql`
- Create: `queries/projects.sql`
- Create: `queries/runs.sql`
- Create: `queries/evals.sql`
- Create: `internal/store/db.go`
- Create: `internal/testutil/postgres.go`
- Create: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `config.Config.DatabaseURL`.
- Produces: `store.Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error)`.
- Produces: sqlc generated `store.Queries`.

- [ ] **Step 1: Write the failing store integration test**

Create `internal/store/store_test.go`:

```go
package store_test

import (
	"context"
	"os"
	"testing"

	"AgentScope/internal/store"
)

func TestCreateUserProjectRun(t *testing.T) {
	dsn := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("AGENTSCOPE_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	q := store.New(db)
	user, err := q.CreateUser(ctx, store.CreateUserParams{
		Email:        "dev@example.com",
		PasswordHash: []byte("hash"),
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	project, err := q.CreateProject(ctx, store.CreateProjectParams{
		Name:    "Demo",
		OwnerID: user.ID,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	run, err := q.CreateRun(ctx, store.CreateRunParams{
		ProjectID: project.ID,
		Source:    "live",
		Status:    "running",
		Name:      "demo run",
		TraceID:   "trace-1",
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.Source != "live" || run.TraceID != "trace-1" {
		t.Fatalf("unexpected run: %+v", run)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/store`

Expected: FAIL with missing `store.Open` or missing generated query types.

- [ ] **Step 3: Add schema and queries**

Create `migrations/00001_init.sql` with tables for `users`, `projects`, `project_members`, `project_tokens`, `runs`, `spans`, `span_events`, `datasets`, `dataset_cases`, `evaluators`, `experiments`, `eval_jobs`, and `eval_case_results`. Use UUID primary keys via `gen_random_uuid()`, `jsonb` for attributes, and indexes on `project_id`, `trace_id`, `run_id`, `experiment_id`, and job lease fields.

Create `queries/auth.sql`:

```sql
-- name: CreateUser :one
INSERT INTO users (email, password_hash)
VALUES ($1, $2)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;
```

Create `queries/projects.sql`:

```sql
-- name: CreateProject :one
WITH project AS (
  INSERT INTO projects (name, owner_id)
  VALUES ($1, $2)
  RETURNING *
)
INSERT INTO project_members (project_id, user_id, role)
SELECT id, owner_id, 'owner' FROM project
RETURNING (SELECT id FROM project) AS id,
          (SELECT name FROM project) AS name,
          (SELECT owner_id FROM project) AS owner_id,
          (SELECT created_at FROM project) AS created_at;

-- name: CreateProjectToken :one
INSERT INTO project_tokens (project_id, name, token_hash, scopes)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetProjectTokenByHash :one
SELECT * FROM project_tokens WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: ListProjectsForUser :many
SELECT p.*
FROM projects p
JOIN project_members pm ON pm.project_id = p.id
WHERE pm.user_id = $1
ORDER BY p.created_at DESC;
```

Create `queries/runs.sql` with `CreateRun`, `GetRun`, `GetRunByTraceID`, `ListRuns`, `UpdateRunSummary`, `CreateSpan`, `CreateSpanEvent`, `ListSpansByRun`, and `ListSpanEventsByRun`.

Create `queries/evals.sql` with `CreateDataset`, `CreateDatasetCase`, `CreateEvaluator`, `CreateExperiment`, `CreateEvalJob`, `ClaimEvalJob`, `HeartbeatEvalJob`, `CompleteEvalJob`, `FailEvalJob`, `CreateEvalCaseResult`, `ListExperimentResults`, and `GetExperiment`.

- [ ] **Step 4: Add store connection**

Create `internal/store/db.go`:

```go
package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}
```

- [ ] **Step 5: Generate queries and run tests**

Run:

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
sqlc generate
go test ./...
```

Expected: PASS, with the store integration test skipped unless `AGENTSCOPE_TEST_DATABASE_URL` is set.

- [ ] **Step 6: Commit**

```bash
git add migrations queries internal/store internal/testutil go.mod go.sum
git commit -m "feat: add database schema and store"
```

---

### Task 3: HTTP Server, Health, and JSON API Conventions

**Files:**
- Create: `internal/httpapi/respond.go`
- Create: `internal/httpapi/router.go`
- Create: `internal/httpapi/router_test.go`
- Create: `internal/app/app.go`
- Modify: `cmd/agentscope-server/main.go`

**Interfaces:**
- Produces: `httpapi.NewRouter(deps httpapi.Dependencies) http.Handler`.
- Produces: `GET /healthz -> {"ok":true}`.

- [ ] **Step 1: Write failing router test**

Create `internal/httpapi/router_test.go`:

```go
package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"AgentScope/internal/httpapi"
)

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	httpapi.NewRouter(httpapi.Dependencies{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.String() != "{\"ok\":true}\n" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run failing test**

Run: `go test ./internal/httpapi`

Expected: FAIL with missing `httpapi.NewRouter`.

- [ ] **Step 3: Implement router and response helpers**

Create `internal/httpapi/respond.go`:

```go
package httpapi

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
```

Create `internal/httpapi/router.go`:

```go
package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Dependencies struct{}

func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	return r
}
```

Create `internal/app/app.go`:

```go
package app

import (
	"context"
	"net/http"

	"AgentScope/internal/config"
	"AgentScope/internal/httpapi"
)

func NewServer(ctx context.Context, cfg config.Config) (*http.Server, error) {
	return &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: httpapi.NewRouter(httpapi.Dependencies{}),
	}, nil
}
```

Modify `cmd/agentscope-server/main.go` so `main` calls `app.NewServer` and `ListenAndServe`.

- [ ] **Step 4: Run tests**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd internal/app internal/httpapi
git commit -m "feat: add http server baseline"
```

---

### Task 4: Auth, Projects, and Project Tokens

**Files:**
- Create: `internal/auth/service.go`
- Create: `internal/projects/service.go`
- Create: `internal/httpapi/auth_handlers.go`
- Create: `internal/httpapi/project_handlers.go`
- Create: `internal/httpapi/middleware.go`
- Create: `internal/auth/service_test.go`
- Modify: `internal/httpapi/router.go`

**Interfaces:**
- Produces: `auth.Service.Register(ctx, email, password string) (store.User, error)`.
- Produces: `auth.Service.Login(ctx, email, password string) (store.User, error)`.
- Produces: `projects.Service.CreateProject(ctx, userID uuid.UUID, name string)`.
- Produces: `projects.Service.CreateToken(ctx, projectID uuid.UUID, name string, scopes []string) (plainToken string, err error)`.
- Produces: `projects.Service.AuthenticateToken(ctx, plainToken string, requiredScope string)`.
- Produces API endpoints `/api/auth/register`, `/api/auth/login`, `/api/projects`, `/api/projects/{id}/tokens`.

- [ ] **Step 1: Write failing auth service test**

Create `internal/auth/service_test.go` with a fake repository interface or database-backed test. Cover password hashing:

```go
func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := auth.HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !auth.CheckPassword(hash, "correct horse battery staple") {
		t.Fatal("expected password to verify")
	}
	if auth.CheckPassword(hash, "wrong") {
		t.Fatal("wrong password verified")
	}
}
```

- [ ] **Step 2: Run failing test**

Run: `go test ./internal/auth`

Expected: FAIL with missing package or missing functions.

- [ ] **Step 3: Implement password and token primitives**

Create `internal/auth/service.go` with:

```go
package auth

import "golang.org/x/crypto/bcrypt"

func HashPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
}

func CheckPassword(hash []byte, password string) bool {
	return bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
}
```

Create `internal/projects/service.go` with token generation using 32 random bytes, base64url encoding, SHA-256 hashing, and scopes `otlp:write` and `runner:write`.

- [ ] **Step 4: Add HTTP handlers**

Add handlers that accept and return JSON:

```json
{"email":"dev@example.com","password":"password123"}
```

Project token creation must return the plaintext token only once:

```json
{"token":"ascope_...","scopes":["otlp:write","runner:write"]}
```

- [ ] **Step 5: Run tests**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/auth internal/projects internal/httpapi
git commit -m "feat: add auth projects and tokens"
```

---

### Task 5: Run and OTLP Trace Ingestion

**Files:**
- Create: `internal/runs/service.go`
- Create: `internal/ingest/otlp.go`
- Create: `internal/ingest/otlp_test.go`
- Create: `internal/httpapi/ingest_handlers.go`
- Create: `internal/httpapi/run_handlers.go`
- Modify: `internal/httpapi/router.go`

**Interfaces:**
- Produces: `runs.Service.CreateLiveRunFromTrace(ctx, projectID, traceID, name string)`.
- Produces: `ingest.DecodeOTLPTraceRequest(body []byte) ([]ingest.SpanRecord, error)`.
- Produces: `POST /v1/traces` accepting OTLP protobuf HTTP requests.
- Produces: `GET /api/projects/{projectID}/runs` and `GET /api/runs/{runID}`.

- [ ] **Step 1: Write failing ingestion mapping test**

Create `internal/ingest/otlp_test.go`:

```go
func TestSpanRecordCarriesAgentAttributes(t *testing.T) {
	record := ingest.SpanRecord{
		TraceID:      "abc",
		SpanID:       "def",
		Name:         "llm call",
		RunName:      "case 1",
		AgentName:    "support-agent",
		AgentVersion: "2026-07-13",
		Attributes:   map[string]any{"llm.model": "gpt-4.1-mini"},
	}
	if record.TraceID == "" || record.AgentName != "support-agent" {
		t.Fatalf("bad record: %+v", record)
	}
}
```

- [ ] **Step 2: Run failing test**

Run: `go test ./internal/ingest`

Expected: FAIL with missing package.

- [ ] **Step 3: Implement ingestion model and decoder**

Create `internal/ingest/otlp.go` with `SpanRecord` and decoder using `go.opentelemetry.io/proto/otlp/collector/trace/v1` protobuf types. Normalize IDs to lowercase hex. Extract attributes:

```text
agentscope.run.name
agentscope.run.id
agentscope.agent.name
agentscope.agent.version
```

- [ ] **Step 4: Implement ingestion handler**

`POST /v1/traces` must:

1. Read bearer token.
2. Authenticate token with `otlp:write`.
3. Decode spans.
4. Create or find a run by trace id.
5. Insert spans and span events.
6. Return HTTP 200 with an empty OTLP response body.

- [ ] **Step 5: Add run query handlers**

`GET /api/projects/{projectID}/runs` returns recent runs. `GET /api/runs/{runID}` returns run, spans, and span events.

- [ ] **Step 6: Run tests**

Run:

```bash
go get go.opentelemetry.io/proto/otlp@latest google.golang.org/protobuf@latest
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/runs internal/ingest internal/httpapi queries migrations
git commit -m "feat: add otlp trace ingestion"
```

---

### Task 6: Datasets, Experiments, and Runner Job API

**Files:**
- Create: `internal/datasets/service.go`
- Create: `internal/experiments/service.go`
- Create: `internal/runnerapi/service.go`
- Create: `internal/runnerapi/service_test.go`
- Create: `internal/httpapi/dataset_handlers.go`
- Create: `internal/httpapi/experiment_handlers.go`
- Create: `internal/httpapi/runner_handlers.go`
- Modify: `internal/httpapi/router.go`

**Interfaces:**
- Produces: `datasets.Service.CreateDataset(ctx, projectID, name string)`.
- Produces: `experiments.Service.CreateExperiment(ctx, projectID, datasetID, name string, evaluatorIDs []uuid.UUID)`.
- Produces: `runnerapi.Service.ClaimJob(ctx, projectID uuid.UUID, runnerName string)`.
- Produces runner endpoints `/runner/jobs/claim`, `/runner/jobs/{id}/heartbeat`, `/runner/jobs/{id}/results`, `/runner/jobs/{id}/fail`.

- [ ] **Step 1: Write failing job lease test**

Create `internal/runnerapi/service_test.go`:

```go
func TestClaimJobPreventsDuplicateLease(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	job := runnerapi.Job{ID: "job-1", Status: "queued"}
	claimed, ok := runnerapi.TryClaim(job, "runner-a", now)
	if !ok {
		t.Fatal("expected first claim")
	}
	_, ok = runnerapi.TryClaim(claimed, "runner-b", now.Add(time.Minute))
	if ok {
		t.Fatal("expected second claim to fail while lease is active")
	}
}
```

- [ ] **Step 2: Run failing test**

Run: `go test ./internal/runnerapi`

Expected: FAIL with missing package.

- [ ] **Step 3: Implement lease rules**

Create pure lease helper:

```go
func TryClaim(job Job, runnerName string, now time.Time) (Job, bool) {
	if job.Status != "queued" && !(job.Status == "claimed" && job.LeaseExpiresAt.Before(now)) {
		return Job{}, false
	}
	job.Status = "claimed"
	job.RunnerName = runnerName
	job.LeaseExpiresAt = now.Add(2 * time.Minute)
	return job, true
}
```

Back the service with SQL using `FOR UPDATE SKIP LOCKED`.

- [ ] **Step 4: Implement dataset and experiment handlers**

Add JSON APIs:

```text
POST /api/projects/{projectID}/datasets
POST /api/datasets/{datasetID}/cases
POST /api/projects/{projectID}/experiments
GET /api/experiments/{experimentID}
```

Creating an experiment creates one `eval_jobs` row per dataset case.

- [ ] **Step 5: Implement runner handlers**

Runner claim response includes case input, expected output, metadata, target name, agent version, evaluator configs, and OTLP endpoint URL.

- [ ] **Step 6: Run tests**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/datasets internal/experiments internal/runnerapi internal/httpapi queries
git commit -m "feat: add datasets experiments and runner jobs"
```

---

### Task 7: Python SDK, Runner, and Rule Evaluators

**Files:**
- Create: `python/pyproject.toml`
- Create: `python/agentscope_sdk/__init__.py`
- Create: `python/agentscope_sdk/target.py`
- Create: `python/agentscope_sdk/runner.py`
- Create: `python/agentscope_sdk/evaluators.py`
- Create: `python/tests/test_target.py`
- Create: `python/tests/test_evaluators.py`
- Create: `python/tests/test_runner_protocol.py`

**Interfaces:**
- Produces: `agentscope_sdk.target(name: str, version: str)`.
- Produces: `agentscope_sdk.get_target(name: str)`.
- Produces: `RuleEvaluator.evaluate(case_input, expected, actual, trace) -> EvaluationResult`.
- Produces CLI command `agentscope-runner start --project-token ... --server-url ...`.

- [ ] **Step 1: Write failing target registration test**

Create `python/tests/test_target.py`:

```python
from agentscope_sdk import get_target, target


def test_target_registration():
    @target(name="support-agent", version="2026-07-13")
    def run_agent(case_input, context):
        return {"answer": case_input["question"]}

    registered = get_target("support-agent")
    assert registered.name == "support-agent"
    assert registered.version == "2026-07-13"
    assert registered.func({"question": "hi"}, {}) == {"answer": "hi"}
```

- [ ] **Step 2: Write failing evaluator test**

Create `python/tests/test_evaluators.py`:

```python
from agentscope_sdk.evaluators import ContainsEvaluator


def test_contains_evaluator_passes():
    evaluator = ContainsEvaluator(needle="refund")
    result = evaluator.evaluate({}, {}, {"answer": "Your refund is approved."}, {})
    assert result.passed is True
    assert result.score == 1.0
    assert "refund" in result.reason
```

- [ ] **Step 3: Run failing tests**

Run:

```bash
cd python
python -m venv .venv
. .venv/bin/activate
pip install -e ".[dev]"
pytest
```

Expected: FAIL before package files exist.

- [ ] **Step 4: Implement Python package**

Create `python/pyproject.toml` with dependencies `httpx`, `pydantic`, `typer`, and `opentelemetry-sdk`.

Create target registry and evaluators. Include exact match, regex, contains, not contains, numeric threshold, and latency threshold classes. Each returns:

```python
class EvaluationResult(BaseModel):
    score: float
    passed: bool
    reason: str
    details: dict[str, Any] = {}
```

- [ ] **Step 5: Implement runner protocol**

`runner.py` must claim a job, invoke the registered target, run rule evaluators, and post results. Use httpx with bearer token auth. If target execution raises, submit `/fail` with the exception class and message.

- [ ] **Step 6: Run Python tests**

Run:

```bash
cd python
. .venv/bin/activate
pytest
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add python
git commit -m "feat: add python sdk runner and rule evaluators"
```

---

### Task 8: LLM Judge Provider Abstraction

**Files:**
- Create: `python/agentscope_sdk/judges.py`
- Create: `python/tests/test_judges.py`
- Modify: `python/agentscope_sdk/evaluators.py`
- Modify: `internal/evaluators/service.go`
- Create: `internal/evaluators/service_test.go`

**Interfaces:**
- Produces Python `JudgeProvider.evaluate(request: JudgeRequest) -> JudgeResult`.
- Produces provider names `openai-compatible` and `anthropic-compatible`.
- Produces backend validation that provider configs reference env var names instead of secret values.

- [ ] **Step 1: Write failing judge JSON parsing test**

Create `python/tests/test_judges.py`:

```python
from agentscope_sdk.judges import parse_judge_response


def test_parse_judge_response():
    result = parse_judge_response('{"score":0.8,"pass":true,"reason":"mostly correct"}')
    assert result.score == 0.8
    assert result.passed is True
    assert result.reason == "mostly correct"
```

- [ ] **Step 2: Run failing test**

Run: `cd python && . .venv/bin/activate && pytest python/tests/test_judges.py`

Expected: FAIL with missing module.

- [ ] **Step 3: Implement judge module**

Create `judges.py` with:

```python
class JudgeRequest(BaseModel):
    input: dict[str, Any]
    expected: Any
    actual: Any
    rubric: str
    context: dict[str, Any] = {}

class JudgeResult(BaseModel):
    score: float
    passed: bool
    reason: str
    raw_summary: str
```

`parse_judge_response` must accept JSON key `pass` and map it to `passed`. Invalid JSON raises `JudgeResponseError`.

- [ ] **Step 4: Implement provider clients**

Implement `OpenAICompatibleJudgeProvider` and `AnthropicCompatibleJudgeProvider` using `httpx`. Both must request structured JSON, temperature 0 by default, and include timeout and retry settings.

- [ ] **Step 5: Add backend provider config validation test**

Create `internal/evaluators/service_test.go` that rejects config values where `api_key_env` is empty or contains a string beginning with `sk-`.

- [ ] **Step 6: Run tests**

Run:

```bash
go test ./...
cd python && . .venv/bin/activate && pytest
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/evaluators python
git commit -m "feat: add llm judge abstraction"
```

---

### Task 9: React UI MVP

**Files:**
- Create: `web/package.json`
- Create: `web/vite.config.ts`
- Create: `web/tsconfig.json`
- Create: `web/src/main.tsx`
- Create: `web/src/api/client.ts`
- Create: `web/src/routes/App.tsx`
- Create: `web/src/routes/Login.tsx`
- Create: `web/src/routes/ProjectHome.tsx`
- Create: `web/src/routes/RunDetail.tsx`
- Create: `web/src/routes/ExperimentDetail.tsx`
- Create: `web/src/styles.css`
- Create: `web/tests/app.spec.ts`
- Modify: `internal/httpapi/router.go`

**Interfaces:**
- Produces UI routes `/login`, `/projects/:projectId`, `/runs/:runId`, `/experiments/:experimentId`.
- Consumes backend JSON APIs from previous tasks.

- [ ] **Step 1: Write failing Playwright smoke test**

Create `web/tests/app.spec.ts`:

```ts
import { expect, test } from "@playwright/test";

test("project home renders recent sections", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByText("Recent runs")).toBeVisible();
  await expect(page.getByText("Recent evals")).toBeVisible();
});
```

- [ ] **Step 2: Run failing frontend test**

Run:

```bash
cd web
npm install
npx playwright install --with-deps chromium
npm test
```

Expected: FAIL before the app exists.

- [ ] **Step 3: Implement Vite app**

Create a dense developer UI with:

- Login form.
- Project home with recent runs and recent evals.
- Run detail with span tree, timeline rows, LLM/tool sections, events, and raw attributes.
- Experiment detail with aggregate metrics, case result table, failure reasons, and trace links.

Use TanStack Query for data fetching. Use plain Tailwind utility classes and avoid nested cards.

- [ ] **Step 4: Serve static assets from Go**

Modify router so production serves `web/dist` for non-API routes after `npm run build`.

- [ ] **Step 5: Run frontend and Go tests**

Run:

```bash
cd web && npm run build && npm test
cd .. && go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web internal/httpapi
git commit -m "feat: add react ui mvp"
```

---

### Task 10: End-to-End Demo and Verification

**Files:**
- Create: `examples/python_support_agent.py`
- Create: `examples/sample_dataset.jsonl`
- Create: `scripts/e2e.sh`
- Create: `docs/development.md`
- Modify: `Makefile`

**Interfaces:**
- Produces: `make e2e`.
- Produces a demo that creates a project token, starts a runner, runs a dataset, and verifies an experiment result with a linked trace.

- [ ] **Step 1: Write failing e2e script skeleton**

Create `scripts/e2e.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

curl -fsS http://localhost:8080/healthz >/dev/null
echo "server healthy"
```

Run: `chmod +x scripts/e2e.sh && ./scripts/e2e.sh`

Expected: FAIL if the server is not running.

- [ ] **Step 2: Add demo agent and dataset**

Create `examples/sample_dataset.jsonl`:

```jsonl
{"input":{"question":"Can I get a refund?"},"expected":{"contains":"refund"},"metadata":{"case":"refund"}}
{"input":{"question":"What is your support email?"},"expected":{"contains":"support"},"metadata":{"case":"support-email"}}
```

Create `examples/python_support_agent.py` with an `@agentscope_sdk.target` that returns deterministic answers for those questions.

- [ ] **Step 3: Extend e2e script**

The script must:

1. Start Postgres through Docker Compose.
2. Run migrations.
3. Start the Go server.
4. Register a user.
5. Create a project and token.
6. Import the JSONL dataset.
7. Create an experiment.
8. Start the Python runner against the demo target.
9. Poll experiment results until all cases finish.
10. Fetch the failed-or-passed case detail and verify it includes a `run_id`.

- [ ] **Step 4: Add development docs**

Create `docs/development.md` with exact commands for:

```bash
make db-up
goose -dir migrations postgres "$AGENTSCOPE_DATABASE_URL" up
make server
cd web && npm run dev
cd python && . .venv/bin/activate && agentscope-runner start --project-token "$TOKEN" --server-url http://localhost:8080
```

- [ ] **Step 5: Run all verification**

Run:

```bash
make test
make e2e
cd python && . .venv/bin/activate && pytest
cd web && npm test
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add examples scripts docs/development.md Makefile
git commit -m "test: add end-to-end demo"
```

---

## Self-Review

- Spec coverage: The plan covers the Go monolith, PostgreSQL schema, project token auth, OTLP ingestion, unified runs, datasets, experiments, remote Python runner, rule evaluators, LLM judge abstraction, React UI, and end-to-end verification.
- Scope adjustment: The spec contains several subsystems. This plan implements them as one vertical MVP and keeps high-scale infrastructure, SaaS-grade tenancy, prompt management, and alerting outside the implementation scope, matching the non-goals.
- Unresolved-marker scan: The plan gives concrete file paths, commands, interfaces, and expected outcomes for every task.
- Type consistency: The primary cross-task concepts are stable: `Run`, `Project`, `Dataset`, `Experiment`, `EvalJob`, `EvaluationResult`, `JudgeRequest`, and `JudgeResult`.
