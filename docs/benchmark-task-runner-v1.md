# OpenHands Benchmark Task Runner HTTP v1

The owned OpenHands fork exposes a small private HTTP service for a benchmark
worker to use in a single-task Agent sidecar. It is intentionally separate from
the interactive Agent Server API.

## Entrypoint and image use

The Python package exposes this entrypoint:

```bash
openhands-task-runner --host 0.0.0.0 --port 8001
```

The repository provides a dedicated, production Docker build entry at
[`Dockerfile.task-runner`](../Dockerfile.task-runner). It uses the locally
cached `python@sha256:9662417aace5ae7b8e2609cce472b72a8958e134ba372808abe9cc1a0c0125e6`
base image and a host-prefetched, SHA-256-verified uv archive. Docker therefore
does not need Docker Hub or GHCR metadata/authentication to resolve a build
image. It installs the complete locked workspace in a builder stage, copies the
ready-to-run virtual environment into the final image, and performs no
dependency installation when a task starts. The final image runs as the
unprivileged `openhands` user (UID/GID `10001` by default).

On Linux x86_64, first prepare the ignored, local build asset. The bootstrap
script honors ordinary `HTTP_PROXY` / `HTTPS_PROXY` environment variables; on
this workstation, use `http://127.0.0.1:7897` if one is not already set:

```bash
HTTPS_PROXY=http://127.0.0.1:7897 ./scripts/bootstrap-docker-build-assets.sh
```

Build from the repository root using Docker's classic builder. `--add-host` is
required on Linux so the build container can reach the host proxy. The proxy is
passed only through Docker's predefined build arguments: it is not declared as
a Dockerfile `ARG` or `ENV`, so it is not retained in image configuration,
history, or runtime environment.

```bash
DOCKER_BUILDKIT=0 docker build \
  --pull=false \
  --add-host host.docker.internal:host-gateway \
  --build-arg HTTP_PROXY=http://host.docker.internal:7897 \
  --build-arg HTTPS_PROXY=http://host.docker.internal:7897 \
  -f Dockerfile.task-runner \
  -t agentlab/openhands-task-runner:local \
  .
```

The same Dockerfile provides the interactive API target:

```bash
DOCKER_BUILDKIT=0 docker build \
  --pull=false \
  --add-host host.docker.internal:host-gateway \
  --build-arg HTTP_PROXY=http://host.docker.internal:7897 \
  --build-arg HTTPS_PROXY=http://host.docker.internal:7897 \
  --target interactive \
  -f Dockerfile.task-runner \
  -t agentlab/openhands:local \
  .
```

Do not use build arguments for LLM, AgentScope, Tool Gateway, or other runtime
credentials. The benchmark orchestrator supplies those only at container run
time. The task-runner target defaults to:

```dockerfile
ENTRYPOINT ["tini", "--", "openhands-task-runner"]
CMD ["--host", "0.0.0.0", "--port", "8001"]
```

A benchmark sidecar should expose only its private container port. Do not
publish port `8001` to an untrusted network: the Task Runner has no end-user
authentication because it is intended for the benchmark worker's private
container network.

## HTTP API

| Method | Path | Meaning |
| --- | --- | --- |
| `GET` | `/healthz` | Liveness and protocol/executor identity |
| `POST` | `/v1/task-runs` | Create one task; responds `202` |
| `GET` | `/v1/task-runs/{task_run_id}` | Poll task state |
| `POST` | `/v1/task-runs/{task_run_id}/cancel` | Request cancellation |

The task request is strict JSON. The required fields are:

```json
{
  "task_run_id": "f58fc42f-5bd4-4d6f-93ba-ae05ea570ce7",
  "task": {
    "task_id": "terminal-bench-smoke",
    "instruction": "Fix the assigned repository.",
    "workspace": "/workspace",
    "timeout_seconds": 1800
  },
  "tool_gateway": {
    "protocol_version": "agentlab.harbor-tool-gateway.v1",
    "endpoint": "http://tool-gateway.internal:8080"
  },
  "profiles": {
    "agent_profile": "openhands-default",
    "model_profile": "coding-model-deterministic"
  },
  "correlation": {
    "experiment_id": "...",
    "eval_job_id": "...",
    "trial_index": 0
  }
}
```

States are `queued`, `running`, `succeeded`, `failed`, `cancelled`, and
`timed_out`. A duplicate `task_run_id` returns `409`. Cancellation wins over a
late executor result.

`GET /healthz` advertises `tool_gateway_protocols` containing
`agentlab.harbor-tool-gateway.v1`. Workers must require that capability before
using this image. `tool_gateway` is a required bounded execution dependency,
and the Task Runner fail-closes by rejecting any other gateway protocol value.
The native benchmark executor requires a non-empty
`AGENTLAB_TOOL_GATEWAY_TOKEN` runtime environment variable and sends every
Terminal Tool command only to:

```text
POST <tool_gateway.endpoint>/terminal/exec
X-AgentLab-Tool-Gateway-Token: <runtime token>
{ "command": "...", "timeout_seconds": 30 }
```

The gateway response must be `{ "stdout": string, "stderr": string,
"return_code": integer }`.  The token is an HTTP header only; it never appears in the task request body,
task state, response, logs, or trace attributes.  The sidecar creates only an empty
private scratch workspace for OpenHands runtime metadata; it does **not** mount
or use `task.workspace` for terminal execution.  The benchmark tool surface is
limited to `TerminalTool` (gateway-backed) and `TaskTrackerTool`; `FileEditorTool`
is deliberately absent, so file changes must occur through the gateway shell in
the Harbor sandbox.  Persistent-terminal modes (`is_input`, `reset`, and empty
local-terminal polling) return a public Tool error and never fall back to a
local process.

## Executors

The production default is the native OpenHands executor. It creates a real
`Conversation` with a gateway-backed terminal and task-tracker tools; it does
not expose a local file-editor tool in benchmark mode. It reads only
runtime configuration:

```bash
LLM_API_KEY=...                  # required by the native executor
LLM_MODEL=...                    # optional; defaults to gpt-5.5
LLM_BASE_URL=...                 # optional
OPENHANDS_AGENTSCOPE_ENABLED=true
AGENTSCOPE_SERVER_URL=http://agentscope:8080
AGENTSCOPE_PROJECT_TOKEN=...     # runtime secret only
AGENTSCOPE_EXPERIMENT_ID=...
AGENTSCOPE_EVAL_JOB_ID=...
AGENTSCOPE_BENCHMARK_KEY=harbor
AGENTSCOPE_BENCHMARK_TASK_ID=...
AGENTSCOPE_TRIAL_INDEX=0
```

The Task Runner never accepts API keys or AgentScope tokens in HTTP requests,
never writes them to task records, and never returns raw executor exceptions.
The existing OpenHands native AgentScope bridge is reused through the real
`Conversation` lifecycle; it reads AgentScope settings and benchmark correlation
from the runtime environment. The task request does not mutate process
variables, so one sidecar must execute one benchmark task.

For local contract/API testing without an LLM credential:

```bash
OPENHANDS_TASK_RUNNER_EXECUTOR=fake \
  openhands-task-runner --host 127.0.0.1 --port 8001
curl http://127.0.0.1:8001/healthz
```

`fake` is deliberately explicit and must not be selected in a benchmark run.
