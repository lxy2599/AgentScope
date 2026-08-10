# Benchmark queue-only control plane migration

AgentScope now treats benchmark execution as a bounded control-plane concern.
It creates and observes durable `eval_jobs`; it does **not** create a local
Harbor process, mint an ephemeral local-runner token, or inject model/provider
credentials into a child process.

## What changed

The established endpoint remains available:

```http
POST /api/projects/{projectID}/benchmark-experiments/{experimentID}/launch
```

Its meaning is now **queue-only**. Benchmark jobs already exist in durable
`eval_jobs` when the experiment is created. A successful response is always
HTTP `202` with `launch.status: "queued"`; it reports whether a matching
external worker is currently registered. It does not return a new server PID
or start a process.

The previous `runner_name`, `pid`, and `started_at` response fields remain
optional compatibility fields. Queue-only responses leave them empty.

A missing worker does not discard queued work. `launch.available` is `false`
and `launch.unavailable_reason` explains why; a worker may register later and
claim the same jobs. Preflight reports this condition as a warning rather than
a blocker, so a benchmark experiment can be created before its Worker is online.

## External worker registration

A long-running benchmark worker uses an existing project token with
`runner:write` scope and refreshes its registration before it expires:

```http
POST /runner/benchmark-workers/register
Authorization: Bearer <project-token>
Content-Type: application/json

{
  "worker_name": "harbor_worker",
  "execution_kinds": ["harbor"],
  "capacity": 2,
  "version": "benchmarks-2026-08-09"
}
```

Registration is an upsert and also acts as the heartbeat. The server keeps the
in-memory liveness record for 45 seconds. Workers should refresh it at least
every 30 seconds. After registering, workers continue to claim and complete
jobs through the existing runner APIs:

```http
GET  /runner/jobs/claim?runner_name=harbor_worker&execution_kind=harbor
POST /runner/jobs/{jobID}/heartbeat
POST /runner/jobs/{jobID}/results
POST /runner/jobs/{jobID}/fail
```

For a non-breaking transition, an existing worker that already calls the claim
endpoint with `execution_kind` is implicitly registered/refreshed. Explicit
registration is still required for accurate availability before a job exists.

Project members can inspect the read model through:

```http
GET /api/projects/{projectID}/benchmark-workers
GET /api/projects/{projectID}/benchmark-workers/{executionKind}/availability
```

## Preflight and catalog read APIs

`POST /api/projects/{projectID}/benchmark-preflight` preserves its request and
response contract. The existing `runner` JSON field is retained, but now
contains external worker availability rather than local process diagnostics:

```json
{
  "available": true,
  "state": "ready",
  "registered_workers": 1,
  "matching_workers": 1,
  "advertised_capacity": 2
}
```

The catalog read APIs are available for UI and external tooling:

```http
GET /api/projects/{projectID}/benchmark-catalog
GET /api/projects/{projectID}/benchmark-artifacts
GET /api/projects/{projectID}/benchmark-profiles
GET /api/projects/{projectID}/benchmark-run-templates
```

A benchmarks CLI can upsert a complete project catalog snapshot through the
administrator-session-only API:

```http
POST /api/projects/{projectID}/benchmark-catalog/sync
Content-Type: application/json

{
  "revision": "git:abc123",
  "artifacts": [...],
  "profiles": [...],
  "run_templates": [...]
}
```

The API accepts no partial update and no project identifier from the body: the
route project is authoritative. It validates immutable SHA-256 image digests,
Task Runner protocol versions, unique IDs and template references, JSON-object
profile/task configuration, and rejects unknown JSON fields. It recursively
rejects secret-bearing keys such as `api_key`, `authorization`, `password`,
`token`, and obvious credential values. Validation failures return a generic
400 response and do not replace the prior catalog revision.

They expose the minimal `AgentArtifact`, `Profile` (`model`, `agent`, or
`execution`), and `RunTemplate` shape. The external benchmarks project remains
the owner of Git/YAML parsing and validation; AgentScope receives only its
already-versioned manifest. The current `StaticCatalogReader` implementation is
in-memory, so a server restart resets synchronized catalog data to the explicit
empty revision `"unconfigured"`; replace it with a durable adapter before a
multi-instance deployment.

## Legacy server configuration

`AGENTSCOPE_BENCHMARK_RUNNER_*` settings and the internal `benchlauncher`
package are no longer wired into `agentscope-server`. They are retained only as
source compatibility during the benchmark-project extraction; setting
`AGENTSCOPE_BENCHMARK_RUNNER_ENABLED=true` no longer authorizes or causes a
server-local benchmark launch.

Move runner executable, Docker/Harbor configuration, model credentials, and
HTTP/HTTPS proxy configuration to the external `benchmarks-worker` deployment.
For local Docker development, proxy configuration may use
`http://host.docker.internal:7897` with a Linux `host-gateway` mapping, but it
must remain deployment configuration rather than a required production value.

## Template-based experiment creation

The preferred launch input for both preflight and experiment creation is now a
single immutable catalog reference:

```http
POST /api/projects/{projectID}/benchmark-preflight
POST /api/projects/{projectID}/benchmark-experiments
Content-Type: application/json

{"run_template_id":"openhands-terminal-bench-smoke"}
```

`run_template_id` is mutually exclusive with the legacy request-time fields
`suite_id`, `target_name`, `target_version`, `trials_per_case`, `case_ids`, and
`benchmark_config`. Legacy requests remain available only during migration;
the template path intentionally has no mutable execution override channel.

For a template request, AgentScope resolves the synchronized catalog revision,
artifact image and digest, model/agent/execution profiles, template selection,
and `execution_overrides`. It resolves the catalog suite ID to exactly one
project `benchmark_suites` record by `benchmark_key`. If an upstream catalog
suite identifier cannot be stored in `benchmark_key` (for example it contains a
dot), the suite may instead declare the exact identifier in non-secret suite
metadata:

```json
{"catalog_suite_id":"terminal-bench-2.0"}
```

The template selector supports `all`, explicit `ids`, deterministic `sample`
with a pinned seed, and `previous_failures`. Explicit catalog task IDs match
`dataset_cases.metadata.task_id`; older datasets without this metadata fall
back to their database case ID. This preserves upstream task identities while
AgentScope continues to use generated database IDs internally.

Before durable `eval_jobs` are created, AgentScope freezes a normalized
`agentlab.benchmark-execution.v1` snapshot into both experiment
`benchmark_config` and job `execution_config`. The snapshot includes the
catalog revision, template ID, selected database cases, effective execution
configuration (`suite default < execution profile < template overrides`),
artifact image/digest/source commit/protocol/capabilities, profile configs,
and suite identity. Workers must execute this snapshot and must not reread the
mutable catalog after claim.

Catalog artifacts therefore include their immutable `capabilities` field and
run templates include optional versioned `execution_overrides`; both fields are
strictly decoded and recursively secret-checked during catalog sync.
