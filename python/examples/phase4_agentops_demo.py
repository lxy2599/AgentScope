#!/usr/bin/env python3
"""Continue a real OpenHands run into the complete Phase 4 AgentOps loop."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import subprocess
import tempfile
import time
import uuid
from pathlib import Path
from typing import Any

import httpx


REQUIRED_OPENHANDS_FIELDS = {
    "email",
    "password",
    "project_id",
    "agent_id",
    "run_id",
    "trace_id",
    "snapshot_id",
}
FORBIDDEN_RESULT_KEYS = {
    "api_key",
    "authorization",
    "project_token",
    "session",
    "session_cookie",
    "token",
}
TARGET_NAME = "phase4-governed-agent"
TARGET_VERSION = "2.0.0"


def load_openhands_result(path: Path) -> dict[str, Any]:
    result = json.loads(path.read_text(encoding="utf-8"))
    missing = sorted(REQUIRED_OPENHANDS_FIELDS - set(result))
    if missing:
        raise ValueError(f"OpenHands result missing fields: {', '.join(missing)}")
    return result


def build_judge_configs() -> list[dict[str, Any]]:
    base_url = os.environ.get("OPENHANDS_LLM_BASE_URL", "").strip()
    model = os.environ.get("OPENHANDS_LLM_MODEL", "").strip()
    if not base_url or not model:
        raise ValueError("OPENHANDS_LLM_BASE_URL and OPENHANDS_LLM_MODEL are required")
    if "/" in model:
        model = model.split("/", 1)[1]
    rubric = (
        "Compare the agent output with the expected answer. Score factual "
        "correctness, completeness, clarity, and safety from 0 to 1. Return "
        "only JSON with score, passed, reason, and dimension_scores."
    )
    return [
        {
            "provider": "openai-compatible",
            "model": model,
            "base_url": base_url,
            "api_key_env": "OPENHANDS_LLM_API_KEY",
            "prompt_version": prompt_version,
            "metric_name": "quality",
            "metric_kind": "final_answer",
            "rubric": rubric,
            "temperature": 0,
            "max_tokens": 512,
        }
        for prompt_version in ("phase4-primary-v1", "phase4-reviewer-v1")
    ]


def validate_public_result(result: dict[str, Any]) -> None:
    def visit(value: Any, path: str) -> None:
        if isinstance(value, dict):
            for key, item in value.items():
                normalized = key.lower().replace("-", "_")
                if normalized in FORBIDDEN_RESULT_KEYS:
                    raise ValueError(f"credential field is forbidden: {path}{key}")
                visit(item, f"{path}{key}.")
        elif isinstance(value, list):
            for index, item in enumerate(value):
                visit(item, f"{path}{index}.")

    visit(result, "")


def api(
    client: httpx.Client,
    method: str,
    base_url: str,
    path: str,
    **kwargs: Any,
) -> Any:
    response = client.request(method, base_url + path, timeout=60, **kwargs)
    if response.status_code >= 400:
        raise RuntimeError(
            f"{method} {path}: {response.status_code} {response.text[:800]}"
        )
    return response.json() if response.content else {}


def log(step: str, message: str) -> None:
    print(f"  [{step}] {message}", flush=True)


def verify_openhands_evidence(
    client: httpx.Client,
    base_url: str,
    source: dict[str, Any],
) -> dict[str, Any]:
    snapshot = api(
        client,
        "GET",
        base_url,
        f"/api/runs/{source['run_id']}/snapshot",
    )
    payload = snapshot["canonical_payload"]
    encoded = json.dumps(
        payload,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    if hashlib.sha256(encoded).hexdigest() != snapshot["payload_hash"]:
        raise RuntimeError("OpenHands snapshot hash does not match canonical payload")
    if snapshot["id"] != source["snapshot_id"]:
        raise RuntimeError("OpenHands snapshot identity changed")

    events = source.get("sse_events") or []
    nonterminal = {
        "run.started",
        "run.state.changed",
        "run.semantic.appended",
    }
    first_live = next(
        (
            index
            for index, event in enumerate(events)
            if event.get("type") in nonterminal
        ),
        None,
    )
    terminal = next(
        (
            index
            for index, event in enumerate(events)
            if event.get("type") in {"run.completed", "run.failed"}
        ),
        None,
    )
    if first_live is None or (terminal is not None and first_live >= terminal):
        raise RuntimeError("OpenHands SSE event order is invalid")
    return snapshot


def create_runner_token(
    client: httpx.Client,
    base_url: str,
    project_id: str,
    description: str,
) -> str:
    response = api(
        client,
        "POST",
        base_url,
        f"/api/projects/{project_id}/tokens",
        json={
            "description": description,
            "scopes": [
                "runner:write",
                "otlp:write",
                "agent:register",
                "agent:runtime",
            ],
        },
    )
    return str(response["plain_token"])


def run_jobs(runner: Any, expected_count: int, label: str) -> None:
    from agentscope_sdk.runner import run_once

    processed = 0
    idle = 0
    while processed < expected_count and idle < 20:
        if run_once(runner):
            processed += 1
            idle = 0
            log(label, f"完成 {processed}/{expected_count}")
        else:
            idle += 1
            time.sleep(0.5)
    if processed != expected_count:
        raise RuntimeError(
            f"{label} processed {processed}/{expected_count} jobs"
        )


def create_experiment(
    client: httpx.Client,
    base_url: str,
    project_id: str,
    dataset_id: str,
    case_ids: list[str],
    version: str,
    evaluator_config: dict[str, Any],
) -> dict[str, Any]:
    return api(
        client,
        "POST",
        base_url,
        f"/api/projects/{project_id}/experiments",
        json={
            "dataset_id": dataset_id,
            "target_name": TARGET_NAME,
            "target_version": version,
            "case_ids": case_ids,
            "evaluator_config": evaluator_config,
            "model_provider": "openai-compatible",
            "model_name": os.environ["OPENHANDS_LLM_MODEL"],
            "model_version": version,
            "prompt_version": f"phase4-{version}",
            "harness_version": "phase4-agentops-v1",
            "dataset_version": "phase4-dataset-v1",
            "toolset_version": "openhands-tools-v1",
            "skillset_version": "agentscope-sdk-v1",
            "rag_config_version": "phase4-rag-v1",
            "feature_flags": json.dumps({"realtime": True, "governance": True}),
        },
    )


def emit_anomaly_baseline(
    tracer: Any,
    project_id: str,
) -> None:
    now_ns = time.time_ns()
    hour_ns = 60 * 60 * 1_000_000_000
    attributes = {
        "agentscope.project.id": project_id,
        "agentscope.agent.name": TARGET_NAME,
        "agentscope.agent.version": TARGET_VERSION,
        "agentscope.environment": "phase4-demo",
        "agentscope.span.kind": "AGENT",
    }
    for hours_ago in range(7, 1, -1):
        start = now_ns - hours_ago * hour_ns
        span = tracer.start_span(
            "agent.run",
            attributes=attributes,
            start_time=start,
        )
        span.end(end_time=start + 100 * 1_000_000)
    recent_start = now_ns - 10 * 60 * 1_000_000_000
    span = tracer.start_span(
        "agent.run",
        attributes=attributes,
        start_time=recent_start,
    )
    span.end(end_time=recent_start + 4_000 * 1_000_000)


def run_small_load(
    root: Path,
    base_url: str,
    project_id: str,
    token: str,
    session_cookie: str,
    agent_id: str,
    instance_id: str,
    report_path: Path,
) -> dict[str, Any]:
    target = {
        "project_id": project_id,
        "token": token,
        "session_cookie": session_cookie,
        "agent_id": agent_id,
        "instance_id": instance_id,
        "agent_name": "phase4-load-agent",
        "agent_version": "load-v1",
        "environment": "phase4-demo",
    }
    with tempfile.NamedTemporaryFile(
        mode="w",
        suffix=".json",
        encoding="utf-8",
        delete=False,
    ) as stream:
        json.dump([target], stream)
        target_path = Path(stream.name)
    target_path.chmod(0o600)
    try:
        report_path.parent.mkdir(parents=True, exist_ok=True)
        command = [
            "go",
            "run",
            "./cmd/agentscope-loadtest",
            "--server-url",
            base_url,
            "--targets",
            str(target_path),
            "--requests",
            "8",
            "--concurrency",
            "2",
            "--batch-size",
            "4",
            "--sse",
            "--runtime",
            "--max-p95",
            "3s",
            "--max-error-rate",
            "0",
            "--max-drop-rate",
            "0",
            "--output",
            "json",
        ]
        completed = subprocess.run(
            command,
            cwd=root,
            check=True,
            capture_output=True,
            text=True,
        )
        report_path.write_text(completed.stdout, encoding="utf-8")
        report = json.loads(completed.stdout)
        if not report.get("passed"):
            raise RuntimeError("small load report did not pass thresholds")
        return report
    finally:
        target_path.unlink(missing_ok=True)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--server-url", default="http://127.0.0.1:8080")
    parser.add_argument("--openhands-result", required=True)
    parser.add_argument("--result-file", required=True)
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[2]
    base_url = args.server_url.rstrip("/")
    source = load_openhands_result(Path(args.openhands_result))
    project_id = str(source["project_id"])
    suffix = uuid.uuid4().hex[:8]
    client = httpx.Client()
    runtime = None

    print("=" * 68)
    print("AgentScope Phase 4 企业 AgentOps 统一闭环")
    print("=" * 68)
    try:
        print("\n[1/10] 复用 OpenHands 单一账号与项目")
        api(
            client,
            "POST",
            base_url,
            "/api/auth/login",
            json={"email": source["email"], "password": source["password"]},
        )
        snapshot = verify_openhands_evidence(client, base_url, source)
        log("identity", f"project={project_id} run={source['run_id']}")

        print("\n[2/10] 配置项目配额与 Runner")
        quota_status = api(
            client,
            "GET",
            base_url,
            f"/api/projects/{project_id}/quotas",
        )
        quota = dict(quota_status["quota"])
        quota.update(
            {
                "ingest_requests_per_minute": 20_000,
                "ingest_spans_per_minute": 500_000,
                "ingest_bytes_per_minute": 1 << 30,
                "sse_connections": 100,
                "tokens_per_day": 100_000_000,
                "cost_micros_per_day": 100_000_000_000,
            }
        )
        api(
            client,
            "PUT",
            base_url,
            f"/api/projects/{project_id}/quotas",
            json=quota,
        )
        runner_token = create_runner_token(
            client,
            base_url,
            project_id,
            "phase4-evaluation-runner",
        )

        print("\n[3/10] 创建并发布版本化 Rubric")
        rubric_template = api(
            client,
            "POST",
            base_url,
            f"/api/projects/{project_id}/rubrics",
            json={
                "name": f"Phase 4 Agent Quality {suffix}",
                "description": "Accuracy, completeness, clarity and safety",
            },
        )
        rubric_version = api(
            client,
            "POST",
            base_url,
            f"/api/projects/{project_id}/rubrics/{rubric_template['id']}/versions",
            json={
                "rubric": {
                    "name": "phase4-quality-v1",
                    "dimensions": [
                        {"name": "accuracy", "weight": 0.4},
                        {"name": "completeness", "weight": 0.2},
                        {"name": "clarity", "weight": 0.2},
                        {"name": "safety", "weight": 0.2},
                    ],
                    "pass_threshold": 0.7,
                }
            },
        )
        rubric_version = api(
            client,
            "POST",
            base_url,
            f"/api/projects/{project_id}/rubric-versions/{rubric_version['id']}/publish",
        )
        judges = build_judge_configs()
        metric_definition = api(
            client,
            "POST",
            base_url,
            f"/api/projects/{project_id}/metric-definitions",
            json={
                "name": f"phase4-judge-quality-{suffix}",
                "kind": "final_answer",
                "weight": 1,
                "pass_threshold": 0.7,
                "config": json.dumps(
                    {
                        "type": "llm_judge",
                        "rubric_version_id": rubric_version["id"],
                        "judges": judges,
                        "judge_aggregation": {
                            "method": "mean",
                            "min_valid_votes": 2,
                            "pass_threshold": 0.7,
                        },
                    }
                ),
            },
        )

        print("\n[4/10] 创建数据集和确定性目标")
        dataset = api(
            client,
            "POST",
            base_url,
            f"/api/projects/{project_id}/datasets",
            json={
                "name": f"Phase 4 Governance Dataset {suffix}",
                "description": "Multi-Judge and calibration acceptance data",
            },
        )
        cases = [
            ("法国的首都是哪里？", "巴黎"),
            ("2 + 2 等于多少？", "4"),
            ("谁写了《哈姆雷特》？", "莎士比亚"),
        ]
        case_ids: list[str] = []
        expected_by_input: dict[str, str] = {}
        for input_text, expected in cases:
            created = api(
                client,
                "POST",
                base_url,
                f"/api/datasets/{dataset['id']}/cases",
                json={
                    "input": input_text,
                    "expected": expected,
                    "metadata": {"phase": 4, "source": "governance-demo"},
                },
            )
            case_ids.append(str(created["id"]))
            expected_by_input[input_text] = expected

        from agentscope_sdk.client import RunnerClient
        from agentscope_sdk.target import target
        from agentscope_sdk.tracing import (
            configure_tracing,
            force_flush,
            get_tracing_stats,
            record_export_metrics,
            shutdown_tracing,
        )

        @target(name=TARGET_NAME, version=TARGET_VERSION)
        def governed_target(input_text: str, context: dict[str, Any]) -> str:
            return expected_by_input.get(input_text, str(context.get("expected") or ""))

        runner = RunnerClient(
            server_url=base_url,
            project_token=runner_token,
            runner_name="phase4-governed-runner",
            timeout=90,
        )

        print("\n[5/10] 执行 Baseline 与双 Judge Candidate 实验")
        baseline = create_experiment(
            client,
            base_url,
            project_id,
            str(dataset["id"]),
            case_ids,
            "1.0.0",
            {
                "name": f"phase4-baseline-{suffix}",
                "type": "multi",
                "pass_threshold": 0.7,
                "multi_metrics": [
                    {
                        "type": "exact",
                        "metric_name": "quality",
                        "metric_kind": "final_answer",
                        "weight": 1,
                        "pass_threshold": 0.7,
                    }
                ],
            },
        )
        run_jobs(runner, len(case_ids), "baseline")
        candidate = create_experiment(
            client,
            base_url,
            project_id,
            str(dataset["id"]),
            case_ids,
            TARGET_VERSION,
            {
                "name": f"phase4-multi-judge-{suffix}",
                "type": "multi",
                "pass_threshold": 0.7,
                "multi_metrics": [
                    {
                        "type": "llm_judge",
                        "metric_name": "quality",
                        "metric_kind": "final_answer",
                        "provider": judges[0]["provider"],
                        "model": judges[0]["model"],
                        "base_url": judges[0]["base_url"],
                        "api_key_env": judges[0]["api_key_env"],
                        "rubric": judges[0]["rubric"],
                        "weight": 1,
                        "pass_threshold": 0.7,
                        "rubric_version_id": rubric_version["id"],
                        "judges": judges,
                        "judge_aggregation": {
                            "method": "mean",
                            "min_valid_votes": 2,
                            "pass_threshold": 0.7,
                        },
                    }
                ],
            },
        )
        run_jobs(runner, len(case_ids), "candidate")
        candidate_results = api(
            client,
            "GET",
            base_url,
            f"/api/projects/{project_id}/experiments/{candidate['id']}/results",
        )
        if len(candidate_results) != len(case_ids):
            raise RuntimeError("candidate result count is incomplete")
        candidate_run_ids = [str(item["run_id"]) for item in candidate_results]
        judge_vote_count = 0
        judge_error_vote_count = 0
        low_confidence_count = 0
        for run_id in candidate_run_ids:
            evaluation = api(
                client,
                "GET",
                base_url,
                f"/api/projects/{project_id}/runs/{run_id}/judge-evaluation",
            )
            if len(evaluation["votes"]) != 2 or len(evaluation["aggregations"]) != 1:
                raise RuntimeError(f"run {run_id} does not have 2 Judge votes")
            judge_vote_count += len(evaluation["votes"])
            judge_error_vote_count += sum(
                1 for vote in evaluation["votes"] if vote.get("error")
            )
            low_confidence_count += sum(
                1
                for aggregation in evaluation["aggregations"]
                if aggregation["result"].get("low_confidence")
            )

        print("\n[6/10] 创建 Gold 并生成 Judge 校准报告")
        annotation_ids: list[str] = []
        for result in candidate_results:
            run_id = str(result["run_id"])
            evaluation = api(
                client,
                "GET",
                base_url,
                f"/api/projects/{project_id}/runs/{run_id}/judge-evaluation",
            )
            judge_score = float(evaluation["aggregations"][0]["result"]["score"])
            annotation = api(
                client,
                "POST",
                base_url,
                f"/api/projects/{project_id}/runs/{run_id}/annotation-items",
                json={},
            )
            annotation = api(
                client,
                "POST",
                base_url,
                f"/api/annotation-items/{annotation['id']}/gold",
                json={"gold_scores": {"quality": judge_score}},
            )
            annotation_ids.append(str(annotation["id"]))
        calibration = api(
            client,
            "POST",
            base_url,
            f"/api/projects/{project_id}/eval-calibrations",
            json={
                "rubric_version_id": rubric_version["id"],
                "sample_run_ids": candidate_run_ids,
                "pass_threshold": 0.7,
            },
        )
        if calibration["status"] != "completed":
            raise RuntimeError("Judge calibration did not complete")

        print("\n[7/10] 生成实验对比和显著性结果")
        comparison = api(
            client,
            "POST",
            base_url,
            f"/api/projects/{project_id}/comparisons",
            json={
                "baseline_experiment_id": baseline["id"],
                "candidate_experiment_id": candidate["id"],
                "comparison_type": "general",
                "regression_gates": json.dumps(
                    {
                        "min_pass_rate_delta": -1,
                        "min_score_delta": -1,
                        "max_cost_increase_ratio": 100,
                        "max_p95_latency_inc_pct": 10000,
                        "safety_veto_enabled": False,
                    }
                ),
            },
        )

        print("\n[8/10] 生成成本性能基线、异常和 SDK 背压证据")
        main_tracer = configure_tracing(
            base_url,
            runner_token,
            service_name="phase4-governance-demo",
        )
        emit_anomaly_baseline(main_tracer, project_id)
        force_flush()
        anomaly = None
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline and anomaly is None:
            api(
                client,
                "POST",
                base_url,
                f"/api/projects/{project_id}/anomalies/detect",
            )
            anomalies = api(
                client,
                "GET",
                base_url,
                f"/api/projects/{project_id}/anomalies?limit=100",
            )
            anomaly = next(
                (
                    item
                    for item in anomalies
                    if item["group"]["metric"] == "run_latency_ms"
                    and item["group"]["agent_name"] == TARGET_NAME
                    and item["status"] == "anomaly"
                ),
                None,
            )
            if anomaly is None:
                time.sleep(0.5)
        if anomaly is None or anomaly["status"] != "anomaly":
            raise RuntimeError("synthetic latency anomaly was not detected")

        pressure_token = create_runner_token(
            client,
            base_url,
            project_id,
            "phase4-sdk-pressure",
        )
        pressure_tracer = configure_tracing(
            base_url,
            pressure_token,
            service_name="phase4-sdk-pressure",
            export_batch_size=8,
            export_delay_ms=10_000,
            export_queue_size=16,
            max_export_retries=0,
        )
        for index in range(4_000):
            with pressure_tracer.start_as_current_span("phase4.pressure") as span:
                span.set_attribute("phase4.sequence", index)
        pressure_stats = get_tracing_stats(base_url, pressure_token)
        if pressure_stats.spans_dropped <= 0:
            raise RuntimeError("SDK pressure did not produce observable queue drops")
        force_flush()
        record_export_metrics(base_url, pressure_token)
        force_flush()

        print("\n[9/10] 执行同项目并发 OTLP、Runtime 与 SSE 流量")
        from agentscope_sdk.runtime import init as agent_init
        from agentscope_sdk.runtime import shutdown_runtime

        runtime = agent_init(
            server_url=base_url,
            project_token=pressure_token,
            agent_name="phase4-load-agent",
            description="Phase 4 unified demo load target",
            heartbeat_interval=5,
            agent_version="load-v1",
            framework="agentscope-sdk",
            runtime="python",
            source="phase4-demo",
        )
        report_path = root / ".tmp" / "phase4-demo-load.json"
        load_report = run_small_load(
            root,
            base_url,
            project_id,
            pressure_token,
            str(client.cookies.get("session") or ""),
            runtime.agent_id,
            runtime.instance_id,
            report_path,
        )
        quota_status = api(
            client,
            "GET",
            base_url,
            f"/api/projects/{project_id}/quotas",
        )
        ingest_stats = api(
            client,
            "GET",
            base_url,
            f"/api/projects/{project_id}/ingest-stats?minutes=60",
        )

        print("\n[10/10] 写入不含 Project Token/LLM Key 的公共结果")
        result = {
            **source,
            "snapshot_hash": snapshot["payload_hash"],
            "rubric_template_id": rubric_template["id"],
            "rubric_version_id": rubric_version["id"],
            "metric_definition_id": metric_definition["id"],
            "dataset_id": dataset["id"],
            "baseline_experiment_id": baseline["id"],
            "candidate_experiment_id": candidate["id"],
            "candidate_run_ids": candidate_run_ids,
            "judge_vote_count": judge_vote_count,
            "judge_error_vote_count": judge_error_vote_count,
            "judge_low_confidence_count": low_confidence_count,
            "annotation_item_ids": annotation_ids,
            "calibration_id": calibration["id"],
            "comparison_id": comparison["id"],
            "anomaly_id": anomaly["id"],
            "sdk_spans_dropped": pressure_stats.spans_dropped,
            "load_report": {
                "requests": load_report["requests"],
                "spans": load_report["spans"],
                "p95_ms": load_report["latency"]["p95_ms"],
                "sse_events": load_report["sse"]["events"],
            },
            "quota_usage": quota_status["usage"],
            "ingest_stats": ingest_stats,
            "project_url": f"http://127.0.0.1:5173/projects/{project_id}",
            "agents_url": f"http://127.0.0.1:5173/projects/{project_id}/agents",
            "openhands_run_url": f"http://127.0.0.1:5173/runs/{source['run_id']}",
            "candidate_experiment_url": (
                f"http://127.0.0.1:5173/experiments/{candidate['id']}"
            ),
            "calibration_url": (
                f"http://127.0.0.1:5173/projects/{project_id}/eval-calibrations"
            ),
            "comparison_url": (
                f"http://127.0.0.1:5173/comparisons/{comparison['id']}"
            ),
            "ingest_url": f"http://127.0.0.1:5173/projects/{project_id}/ingest",
        }
        validate_public_result(result)
        Path(args.result_file).write_text(
            json.dumps(result, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
        log("done", f"result={args.result_file}")
        print(json.dumps(result, ensure_ascii=False, indent=2))
        shutdown_tracing()
        shutdown_runtime()
        runtime = None
    finally:
        if runtime is not None:
            from agentscope_sdk.runtime import shutdown_runtime

            shutdown_runtime()
        client.close()


if __name__ == "__main__":
    main()
