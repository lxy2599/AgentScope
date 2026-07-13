# AgentScope Agent Observability and Evaluation Design

Date: 2026-07-13

## Summary

AgentScope is a team-shared developer platform for observing and evaluating agent runs. The first version focuses on agent developers who need to debug individual runs, run offline evaluations, inspect failures, and compare agent versions.

The central abstraction is `Run`. A live agent execution and an offline evaluation case both produce a run. OpenTelemetry spans, span events, logs, evaluator outputs, and failure details attach to the same run model so evaluation failures can link directly to the trace that explains them.

## Goals

- Receive OpenTelemetry-compatible traces from agent applications.
- Provide a Python SDK and remote runner for offline evaluation.
- Store projects, users, runs, traces, datasets, experiments, jobs, and results in PostgreSQL.
- Support both deterministic rule evaluators and LLM judge evaluators.
- Provide a Web UI for recent runs, trace debugging, datasets, experiments, failure analysis, and experiment comparison.
- Support a single-team deployment with simple login, project membership, and project tokens.

## Non-Goals

- ClickHouse, Kafka, or high-throughput observability infrastructure.
- SaaS-grade multi-tenancy, complex RBAC, audit logs, billing, or organization hierarchy.
- Server-side execution of arbitrary user agent code.
- Full prompt management, model gateway, alerting, SLO dashboards, or complete metrics backend.
- Full OpenTelemetry metrics storage. The first version stores run-related trace data and selected derived metrics.

## Recommended Technical Stack

### Backend

- Go service using a lightweight HTTP router such as `chi`.
- PostgreSQL as the only persistent database.
- `pgx` plus `sqlc` for typed SQL access.
- `goose` for migrations.
- Cookie sessions and bcrypt passwords for user login.
- Project tokens for OTLP ingestion and runner access.
- OTLP HTTP ingestion, with traces as the first priority.

### Frontend

- React, TypeScript, and Vite.
- TanStack Query for API data loading and caching.
- React Router for routing.
- Tailwind CSS plus small custom components for a dense developer-tool UI.
- Static frontend assets served by the Go server for simple deployment.

### Python SDK and Runner

- Python 3.11+.
- OpenTelemetry Python SDK for trace creation and export.
- Pydantic for config and payload models.
- Typer for the `agentscope-runner` CLI.
- httpx for service communication.
- Provider abstraction for LLM judges, with OpenAI-compatible and Anthropic-compatible implementations in the first version.

### Deployment

- Docker Compose for `server` and `postgres`.
- Runner runs separately in the user's local, CI, or private execution environment.

## Architecture

AgentScope is a deployable Go monolith backed by PostgreSQL. The monolith owns ingestion, API, auth, job orchestration, persistence, and static UI serving. Boundaries should remain clear inside the codebase even though deployment is a single service.

Major components:

- Ingestion: accepts OTLP HTTP traces, validates project token, maps spans into runs.
- Runs: creates and queries live and eval runs.
- Trace storage: stores spans, span events, and selected derived fields.
- Auth and projects: manages users, project membership, and project tokens.
- Datasets: stores datasets and cases.
- Experiments: creates evaluation runs and aggregates results.
- Runner API: lets remote Python runners claim jobs, heartbeat, submit results, and report failures.
- Evaluators: stores deterministic and LLM judge evaluator configs and results.
- Web API: serves data to the React frontend.

## Data Model

Core tables:

- `users`: team member accounts.
- `projects`: project workspaces.
- `project_members`: project membership with `owner` and `member` roles.
- `project_tokens`: scoped tokens for OTLP ingestion and runner access.
- `runs`: unified live and eval run records.
- `spans`: structured OpenTelemetry spans.
- `span_events`: span-attached events such as tool calls, LLM usage, retrieval hits, and errors.
- `datasets`: evaluation datasets.
- `dataset_cases`: input, expected output, metadata, and tags.
- `experiments`: evaluation configuration and aggregate status.
- `eval_jobs`: work items claimed by remote runners.
- `eval_case_results`: per-case output, scores, pass/fail status, reasons, and linked run.
- `evaluators`: deterministic or LLM judge evaluator definitions.
- `judge_provider_configs`: provider references and non-secret settings.

`runs` should include:

- `project_id`
- `source`: `live` or `eval`
- `status`
- `name`
- `trace_id`
- `agent_name`
- `agent_version`
- `input_summary`
- `output_summary`
- `started_at`
- `ended_at`
- `latency_ms`
- `cost`

Span attributes and evaluator details can use `JSONB`, while frequently queried values should be duplicated into typed columns and indexed.

## Live Run Flow

1. A Python SDK client or other OpenTelemetry client creates a trace.
2. The client sends spans to AgentScope's OTLP HTTP endpoint with a project token.
3. Ingestion validates the token and reads AgentScope-specific attributes such as run id, run name, agent name, and agent version.
4. If a run id exists, spans attach to the existing run. Otherwise, ingestion creates a `live` run for the trace.
5. Spans and events are persisted in PostgreSQL.
6. The UI queries the run, span tree, timeline, LLM calls, tool calls, raw attributes, and errors.

## Offline Evaluation Flow

1. A user creates a dataset and experiment in the UI or API.
2. The service creates eval jobs for dataset cases.
3. A remote Python runner authenticates with a project token and claims jobs.
4. The runner invokes a local Python target registered by the SDK.
5. The target emits OpenTelemetry traces while executing the agent.
6. The runner executes deterministic evaluators and LLM judge evaluators.
7. The runner submits outputs, evaluator results, trace/run identifiers, and errors.
8. The service updates case results and experiment aggregates.
9. The UI shows aggregate scores, failed cases, evaluator explanations, and links to corresponding run traces.

## Python SDK and Runner

The Python SDK should let developers register an eval target in their own environment:

```python
@agentscope.target(name="support-agent", version="2026-07-13")
def run_agent(case_input, context):
    return my_agent.invoke(case_input)
```

The runner starts from the user's environment:

```bash
agentscope-runner start --project-token "$AGENTSCOPE_PROJECT_TOKEN" --server-url "https://agentscope.example.com"
```

Runner API:

- `GET /runner/jobs/claim`: claim executable work.
- `POST /runner/jobs/{id}/heartbeat`: keep a lease alive.
- `POST /runner/jobs/{id}/results`: submit case output, evaluator results, and trace/run links.
- `POST /runner/jobs/{id}/fail`: report runner-side failure.

Jobs use leases and heartbeats. Expired jobs may be retried. Case execution failures are recorded as case errors and should not make the experiment unreadable.

## Evaluators

### Rule Evaluators

The first version should support:

- Exact match.
- Regex match.
- JSONPath assertion.
- Contains and not contains.
- Numeric threshold.
- Tool call sequence assertion.
- Latency and cost thresholds.

Each evaluator returns:

- `score`
- `pass`
- `reason`
- optional structured `details`

### LLM Judge Evaluators

LLM judge providers share a common interface:

```text
JudgeProvider.evaluate(input, output, expected, rubric, context) -> score, pass, reason
```

The first version implements:

- OpenAI-compatible provider.
- Anthropic-compatible provider.

Judge config includes:

- provider
- model
- rubric
- score scale
- pass threshold
- temperature, defaulting to 0
- max tokens
- retry policy

Judge outputs must be structured JSON. Parse failures are evaluator errors, not silent failed cases. The system stores judge prompt template version, model, request parameters, raw response summary, token usage, score, pass/fail, and reason. Provider secrets are referenced through environment variables rather than stored directly in the database.

## Web UI

The app should open directly into the work surface after login.

Pages:

- Projects: project list, project creation, token management, and member management.
- Project Home: recent live runs, recent eval experiments, recent failures, latency/cost/error summaries.
- Runs: run list with status, source, agent, version, latency, cost, and error state.
- Run Detail: span tree, timeline, LLM calls, tool calls, events, errors, and raw attributes.
- Datasets: dataset list, case list, case editor, and JSONL import.
- Experiments: create experiment, view aggregate metrics, inspect case results, and compare two experiments.
- Runner: runner setup, recent heartbeat, claimed jobs, and runner failures.
- Settings: user settings, project members, project tokens, and judge provider references.

The UI should prioritize dense, scan-friendly developer workflows. Failed eval cases must link directly to the associated run detail page.

## Error Handling

- Invalid project tokens return 401.
- Missing run identifiers in OTLP data create a live run from the trace id.
- Oversized attributes or event payloads are truncated and marked with `truncated=true`.
- Runner leases prevent duplicate job execution under normal operation.
- Expired runner leases allow retry.
- Case-level execution failures are stored per result.
- Rule evaluator config errors are shown as evaluator errors.
- LLM judge failures follow retry policy and then store error details.
- Retention cleanup can remove old spans and events while preserving experiment summaries.

## Testing Strategy

### Go Backend

- Repository integration tests against PostgreSQL.
- OTLP ingestion mapping tests from payload to run, span, and event records.
- Runner job lease tests, including concurrent claim attempts.
- Auth and project token tests.
- Experiment aggregation tests.

### Python SDK and Runner

- Target registration and invocation tests.
- Evaluator unit tests.
- Runner protocol tests with a mock server.
- OpenTelemetry attribute injection tests.

### Frontend

- API client and state tests for key views.
- Playwright tests for login, run inspection, dataset creation, experiment creation, failed case inspection, and trace navigation.

### End-to-End

- Docker Compose starts server and PostgreSQL.
- A sample Python agent runs a small dataset through the runner.
- The test verifies that experiment results and linked traces are visible through API and UI.

## Acceptance Criteria

The first version is complete when a team can:

1. Create a user, project, and project token.
2. Use Python SDK or OpenTelemetry to submit an agent run.
3. View run trace, LLM calls, tool calls, events, and errors in the UI.
4. Create a dataset and experiment.
5. Start a remote Python runner.
6. Execute evaluations with both rule evaluators and LLM judge evaluators.
7. Inspect failed cases and jump to the corresponding run trace.
8. Compare two experiments at case level.

