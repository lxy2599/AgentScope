"""Run one real, offline Harbor sentinel trial from the Worker image.

This is a fixture-level E2E gate, not a benchmark score.  It proves the exact
trust chain that a normal job uses:

    Harbor task sandbox -> digest-pinned Task Runner sidecar -> per-trial
    Harbor Tool Gateway -> Harbor official verifier reward

The fixture sidecar is deterministic so no model/provider credential or runtime
agent installation is needed.  The final assertion remains Harbor's official
verifier reward; HTTP success and a Task Runner success state are not passes.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any
from uuid import uuid4

from .contracts import Correlation, JobResult, LeasedJob, TaskRun, TaskRunState, TaskSpec, WorkerBackend
from .harbor_executor import HarborJobExecutor, HarborRuntimeOptions

_IMAGE_DIGEST = re.compile(r"^sha256:[a-f0-9]{64}$")
_SAFE_IMAGE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._/:+-]{0,511}$")


@dataclass
class _FixtureBackend(WorkerBackend):
    job: LeasedJob
    claimed: bool = False
    heartbeats: list[tuple[str, str]] = field(default_factory=list)
    results: list[JobResult] = field(default_factory=list)
    failures: list[tuple[str, str, str]] = field(default_factory=list)

    def claim(self, worker_id: str) -> LeasedJob | None:
        del worker_id
        if self.claimed:
            return None
        self.claimed = True
        return self.job

    def heartbeat(self, worker_id: str, eval_job_id: str) -> None:
        self.heartbeats.append((worker_id, eval_job_id))

    def submit_result(self, worker_id: str, result: JobResult) -> None:
        del worker_id
        self.results.append(result)

    def fail(self, worker_id: str, eval_job_id: str, error_type: str) -> None:
        self.failures.append((worker_id, eval_job_id, error_type))


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="run AgentLab's real Harbor sentinel E2E")
    parser.add_argument("--catalog-root", type=Path, required=True)
    parser.add_argument("--work-root", type=Path, required=True)
    parser.add_argument("--sidecar-image", required=True)
    parser.add_argument("--sidecar-digest", required=True)
    parser.add_argument("--report", type=Path, required=True)
    return parser


def _main(argv: list[str]) -> int:
    args = _parser().parse_args(argv)
    catalog_root = args.catalog_root.expanduser().resolve()
    work_root = args.work_root.expanduser().resolve()
    report = args.report.expanduser().resolve()
    if not catalog_root.is_dir():
        return _fail(report, "catalog_root_unavailable")
    if not _SAFE_IMAGE.fullmatch(args.sidecar_image) or "@" in args.sidecar_image:
        return _fail(report, "sidecar_image_invalid")
    if not _IMAGE_DIGEST.fullmatch(args.sidecar_digest):
        return _fail(report, "sidecar_digest_invalid")
    if not os.environ.get("AGENTLAB_SIDECAR_NETWORK"):
        return _fail(report, "sidecar_network_required")
    if not os.environ.get("AGENTLAB_TOOL_GATEWAY_HOST"):
        return _fail(report, "tool_gateway_host_required")

    # The work root must be shared at the identical absolute path with Docker;
    # its paths become binds for the short-lived sidecar and Harbor sandbox.
    if work_root.exists():
        shutil.rmtree(work_root)
    work_root.mkdir(mode=0o700, parents=True)
    task_run_id = str(uuid4())
    job = LeasedJob(
        eval_job_id=f"harbor-sentinel-{uuid4().hex}",
        task=TaskSpec(
            task_run_id=task_run_id,
            task_id="harbor-sentinel",
            instruction="Create the Harbor sentinel file exactly as requested.",
            workspace="/workspace",
            timeout_seconds=60,
            tool_gateway_endpoint="http://unused.invalid/v1",
            tool_gateway_protocol="agentlab.harbor-tool-gateway.v1",
            agent_profile_id="harbor-sentinel",
            model_profile_id="deterministic-fixture",
            correlation=Correlation("harbor-sentinel", "harbor-sentinel", 0),
            execution_config={
                "execution": {
                    "harbor": {
                        "version": "0.20.0",
                        "task_path": "fixtures/harbor-sentinel",
                        "agent_import_path": "benchmark_worker.harbor_agent:TaskRunnerAgent",
                        "jobs_root": "harbor-jobs",
                        "job_name": "harbor-sentinel",
                    }
                },
                "agent_artifact": {
                    "framework": "harbor-sentinel",
                    "image": args.sidecar_image,
                    "image_digest": args.sidecar_digest,
                },
            },
        ),
    )
    backend = _FixtureBackend(job)
    executor = HarborJobExecutor(
        "harbor-sentinel-worker",
        backend,
        options=HarborRuntimeOptions(
            work_root=work_root,
            catalog_root=catalog_root,
            artifact_root=work_root / "artifacts",
            cleanup_workspaces=False,
        ),
        heartbeat_interval_s=120.0,
    )
    try:
        processed = executor.process_once()
    except Exception:
        return _fail(report, "worker_execution_exception")
    if not processed:
        return _fail(report, "fixture_job_not_claimed")
    if backend.failures:
        return _fail(report, f"worker_failure_{backend.failures[-1][2]}")
    if len(backend.results) != 1:
        return _fail(report, "official_result_not_submitted")
    result = backend.results[0]
    if result.verifier_status != "passed" or result.score != 1.0:
        return _fail(report, "official_verifier_not_passed")
    evidence = _read_evidence(work_root)
    if evidence != {"task_sandbox_file": True, "content_matches": True}:
        return _fail(report, "official_verifier_evidence_invalid")
    _write_report(
        report,
        {
            "schema_version": "agentlab.harbor-sentinel-e2e.v1",
            "status": "passed",
            "harbor_version": "0.20.0",
            "sidecar": {
                "digest_pinned": True,
                "framework": "harbor-sentinel",
            },
            "tool_gateway": {"protocol": "agentlab.harbor-tool-gateway.v1"},
            "official_verifier": {
                "status": result.verifier_status,
                "score": result.score,
                **evidence,
            },
        },
    )
    print(json.dumps({"harbor_sentinel_e2e": "passed", "official_verifier": "passed"}, sort_keys=True))
    return 0


def _read_evidence(work_root: Path) -> dict[str, bool] | None:
    # This file is emitted by the task's official verifier, not by the sidecar.
    # Do not surface Harbor result/log text: it may carry task or provider data.
    matches = list(work_root.glob("harbor-jobs/**/verifier/sentinel-evidence.json"))
    if len(matches) != 1:
        return None
    try:
        payload = json.loads(matches[0].read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError):
        return None
    if not isinstance(payload, dict) or payload.get("schema_version") != "agentlab.harbor-sentinel-evidence.v1":
        return None
    present = payload.get("task_sandbox_file")
    matched = payload.get("content_matches")
    if not isinstance(present, bool) or not isinstance(matched, bool):
        return None
    return {"task_sandbox_file": present, "content_matches": matched}


def _fail(report: Path, reason: str) -> int:
    _write_report(report, {"schema_version": "agentlab.harbor-sentinel-e2e.v1", "status": "failed", "reason": reason})
    print(json.dumps({"harbor_sentinel_e2e": "failed", "reason": reason}, sort_keys=True), file=sys.stderr)
    return 1


def _write_report(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, sort_keys=True, separators=(",", ":")), encoding="utf-8")
    path.chmod(0o600)


def main() -> None:
    raise SystemExit(_main(sys.argv[1:]))


if __name__ == "__main__":
    main()
