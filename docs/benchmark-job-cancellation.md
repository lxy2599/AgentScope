# Benchmark evaluation-job cancellation contract

This is the administrator-only recovery path for benchmark experiments whose
immutable execution snapshot is known to be stale or incorrect (for example,
queued jobs that reference an obsolete image digest). It is deliberately a
**durable control-plane cancellation**, not a database repair command and not a
server-to-sidecar network call.

## API

```http
POST /api/projects/{projectID}/benchmark-experiments/{experimentID}/cancel
Content-Type: application/json

{"reason":"obsolete immutable image snapshot"}
```

Only project `owner` and `admin` session identities may call this endpoint. The
body is strict JSON and `reason` is required. It is capped at 512 bytes and is
rejected if it contains an obvious credential pattern. Validation errors never
echo the supplied value.

A successful response intentionally does not echo the reason:

```json
{
  "experiment_id": "...",
  "status": "cancelled",
  "cancelled_jobs": 7,
  "audit_events": 7
}
```

To inspect retained audit facts, the same administrator boundary provides:

```http
GET /api/projects/{projectID}/benchmark-experiments/{experimentID}/cancellation-audit
```

The response contains the affected `eval_job_id`, requesting administrator,
reason, original `queued`/`running` state, and timestamp.

## Transactional semantics

The store locks the benchmark experiment with its project and administrator
membership predicate, then locks all jobs in `queued` or `running` state. For
every affected job, one transaction:

1. changes the job to `cancelled`;
2. clears `runner_name` and `lease_expires_at`, preventing a stale lease from
   being renewed or completed;
3. stores only the generic job error `cancelled by project administrator`;
4. writes one immutable `benchmark_eval_job_cancellation_audits` row containing
   the reviewed reason and pre-cancellation status; and
5. marks the experiment `cancelled`.

Succeeded, failed, timed-out, and already-cancelled jobs are never rewritten.
A repeated cancellation is idempotent: it creates no more audit rows and does
not overwrite the original reason. If a fully terminal non-cancelled experiment
is targeted, it remains in its existing terminal state and the response reports
zero changed jobs.

## Task Runner interaction

The benchmark Task Runner v1 contract treats cancellation as terminal and gives
cancellation precedence over a late executor result. AgentScope enforces the
same invariant at the authoritative queue boundary: the existing runner result
and failure paths accept only a job still in `running` state and owned by the
claiming runner. Once this operation commits, a late sidecar result cannot make
the cancelled job pass or affect the experiment score.

AgentScope never dials a task runner directly: task-runner endpoints and
per-trial capabilities remain private to the benchmark worker network. A worker
that observes its task can no longer heartbeat or submit must cooperatively call
its own private `POST /v1/task-runs/{task_run_id}/cancel` endpoint and must not
reinterpret a late task result as an official benchmark outcome.
