from __future__ import annotations

import json
from dataclasses import dataclass, field
from pathlib import Path
from typing import Mapping

import pytest

from benchmark_worker.platform import HTTPResponse, PlatformClient, PlatformError, catalog_sync_payload
from benchmarkctl.catalog import load_catalog

ROOT = Path(__file__).resolve().parents[1]


@dataclass
class FakeTransport:
    responses: list[HTTPResponse]
    calls: list[tuple[str, str, dict[str, str], bytes | None]] = field(default_factory=list)

    def request(
        self,
        method: str,
        url: str,
        headers: Mapping[str, str],
        body: bytes | None,
        timeout_s: float,
    ) -> HTTPResponse:
        self.calls.append((method, url, dict(headers), body))
        return self.responses.pop(0)


def json_response(payload: dict, status_code: int = 200) -> HTTPResponse:
    return HTTPResponse(status_code, json.dumps(payload).encode("utf-8"))


def test_catalog_sync_converts_source_manifest_to_strict_platform_payload() -> None:
    source_manifest = load_catalog(ROOT).to_manifest()
    source_manifest["objects"]["run_templates"][0]["execution_overrides"] = {"task_timeout_s": 1}
    transport = FakeTransport([json_response({"status": "ok"})])
    client = PlatformClient("http://platform.test", transport=transport)

    assert client.sync_catalog("project / smoke", source_manifest, "session=very-secret-cookie") == {"status": "ok"}

    method, url, headers, body = transport.calls[0]
    payload = json.loads(body or b"{}")
    assert method == "POST"
    assert url == "http://platform.test/api/projects/project%20%2F%20smoke/benchmark-catalog/sync"
    assert headers["Cookie"] == "session=very-secret-cookie"
    assert set(payload) == {"revision", "artifacts", "profiles", "run_templates"}
    assert "objects" not in payload
    assert "catalog_sha256" not in payload
    assert payload["revision"] == f"git:{source_manifest['catalog_sha256']}"

    profiles = {profile["id"]: profile for profile in payload["profiles"]}
    assert {
        "deepseek-v4-flash-deterministic",
        "hermes-ark-deepseek-v4-flash",
        "hermes-default",
        "openclaw-default",
        "openhands-default",
        "harbor-local-agent-smoke",
        "harbor-smoke",
    } <= set(profiles)
    assert profiles["hermes-kuaipao-gpt-5-4-mini"]["kind"] == "model"
    assert profiles["harbor-terminal-bench-all"]["kind"] == "execution"
    assert profiles["harbor-terminal-bench-real-five"]["kind"] == "execution"
    assert profiles["deepseek-v4-flash-deterministic"] == {
        "id": "deepseek-v4-flash-deterministic",
        "kind": "model",
        "config": {
            "provider": "openai-compatible",
            "model": "openai/deepseek-v4-flash-ga-260731",
            "parameters": {"temperature": 0, "top_p": 1, "max_output_tokens": 8192, "seed": 42},
        },
    }
    assert profiles["hermes-ark-deepseek-v4-flash"] == {
        "id": "hermes-ark-deepseek-v4-flash",
        "kind": "model",
        "config": {
            "provider": "volcengine-ark-openai-compatible",
            "model": "deepseek-v4-flash-ga-260731",
            "parameters": {},
        },
    }
    assert profiles["hermes-default"] == {
        "id": "hermes-default",
        "kind": "agent",
        "framework": "hermes",
        "config": {
            "max_iterations": 80,
            "prompt_version": "hermes-agentlab-native-v1",
            "enable_browser": False,
            "tool_policy": "harbor-terminal-gateway-only",
        },
    }
    assert profiles["openclaw-default"] == {
        "id": "openclaw-default",
        "kind": "agent",
        "framework": "openclaw",
        "config": {"max_turns": 50, "thinking": False, "tool_policy": "coding"},
    }
    assert profiles["openhands-default"] == {
        "id": "openhands-default",
        "kind": "agent",
        "framework": "openhands",
        "config": {"max_iterations": 50, "prompt_version": "openhands-se-v3", "enable_browser": False},
    }
    assert profiles["harbor-smoke"] == {
        "id": "harbor-smoke",
        "kind": "execution",
        "config": {
            "concurrency": 1,
            "task_timeout_s": 900,
            "max_retries": 0,
            "fail_fast": True,
            "artifact_policy": "all",
            "harbor": {
                "version": "0.20.0",
                "dataset": "terminal-bench@2.0",
                "agent_import_path": "benchmark_worker.harbor_agent:TaskRunnerAgent",
                "jobs_root": "harbor-jobs",
                "job_name": "terminal-bench-smoke",
            },
        },
    }
    assert profiles["harbor-local-agent-smoke"] == {
        "id": "harbor-local-agent-smoke",
        "kind": "execution",
        "config": {
            "concurrency": 1,
            "task_timeout_s": 180,
            "max_retries": 0,
            "fail_fast": True,
            "artifact_policy": "all",
            "harbor": {
                "version": "0.20.0",
                "task_path": "fixtures/harbor-agent-smoke",
                "agent_import_path": "benchmark_worker.harbor_agent:TaskRunnerAgent",
                "jobs_root": "harbor-jobs",
                "job_name": "agentlab-harbor-smoke",
            },
        },
    }

    template = next(item for item in payload["run_templates"] if item["id"] == "openhands-terminal-bench-smoke")
    assert template == {
        "id": "openhands-terminal-bench-smoke",
        "suite_id": "terminal-bench-fixtures-legacy",
        "agent_artifact_id": "openhands-dev-smoke",
        "model_profile_id": "deepseek-v4-flash-deterministic",
        "agent_profile_id": "openhands-default",
        "execution_profile_id": "harbor-smoke",
        "task_selection": {"mode": "ids", "include": ["smoke-task-001"]},
        "execution_overrides": {},
        "trials_per_case": 1,
    }


def test_sync_payload_is_strict_and_source_manifest_is_unchanged() -> None:
    source_manifest = load_catalog(ROOT).to_manifest()
    payload = catalog_sync_payload(source_manifest)

    assert set(payload) == {"revision", "artifacts", "profiles", "run_templates"}
    assert set(source_manifest) == {"schema_version", "catalog_sha256", "objects"}
    assert payload["artifacts"] == [
        {
            "id": artifact["id"],
            "framework": artifact["framework"],
            "image": artifact["image"],
            "image_digest": artifact["image_digest"],
            "source_commit": artifact["source_commit"],
            "task_runner_protocol": artifact["task_runner_protocol"],
            "capabilities": artifact["capabilities"],
        }
        for artifact in source_manifest["objects"]["artifacts"]
    ]


def test_http_error_redacts_cookie_and_project_token() -> None:
    cookie = "session=very-secret-cookie"
    cookie_transport = FakeTransport([json_response({"error": f"echo {cookie}", "session_cookie": cookie}, 500)])
    with pytest.raises(PlatformError) as cookie_error:
        PlatformClient("http://platform.test", transport=cookie_transport).sync_catalog(
            "project", load_catalog(ROOT).to_manifest(), cookie
        )
    assert cookie not in str(cookie_error.value)
    assert "[REDACTED]" in str(cookie_error.value)

    token = "project-token-value"
    token_transport = FakeTransport([json_response({"error": f"echo {token}", "authorization": token}, 500)])
    with pytest.raises(PlatformError) as token_error:
        PlatformClient("http://platform.test", transport=token_transport).register_benchmark_worker(token, "harbor-local")
    assert token not in str(token_error.value)
    assert "[REDACTED]" in str(token_error.value)


def test_runner_registration_claim_and_heartbeat_use_harbor_contract() -> None:
    from benchmark_worker.platform import HTTPPlatformWorkerBackend

    token = "runner-project-token"
    transport = FakeTransport(
        [
            json_response({"status": "registered"}, 201),
            json_response(
                {
                    "job": {
                        "id": "job-1",
                        "experiment_id": "experiment-1",
                        "dataset_case_id": "case-1",
                        "execution_kind": "harbor",
                        "trial_index": 2,
                        "execution_config": {"workspace": "/workspace", "task_timeout_s": 300},
                    },
                    "case_input": "Repair the project.",
                    "eval_config": {
                        "tool_gateway_endpoint": "http://gateway.internal",
                        "tool_gateway_protocol": "agentlab.harbor-tool-gateway.v1",
                        "agent_profile": "openhands-default",
                        "model_profile": "deepseek-v4-flash-deterministic",
                    },
                    "benchmark": {"harness_kind": "harbor"},
                }
            ),
            json_response({"status": "ok"}),
        ]
    )
    client = PlatformClient("http://platform.test", transport=transport)
    backend = HTTPPlatformWorkerBackend(client, token)

    assert client.register_benchmark_worker(token, "harbor-local") == {"status": "registered"}
    job = backend.claim("harbor-local")
    assert job is not None
    backend.heartbeat("harbor-local", job.eval_job_id)

    register_call, claim_call, heartbeat_call = transport.calls
    assert register_call[0:2] == ("POST", "http://platform.test/runner/benchmark-workers/register")
    assert json.loads(register_call[3] or b"{}") == {
        "worker_name": "harbor-local",
        "execution_kinds": ["harbor"],
        "capacity": 1,
        "version": "benchmark-worker/v1",
    }
    assert claim_call[0:2] == ("GET", "http://platform.test/runner/jobs/claim?runner_name=harbor-local&execution_kind=harbor")
    assert json.loads(heartbeat_call[3] or b"{}") == {"runner_name": "harbor-local"}
    assert all(call[2]["Authorization"] == f"Bearer {token}" for call in transport.calls)
    assert job.task.instruction == "Repair the project."
    assert job.task.workspace == "/workspace"
    assert job.task.timeout_seconds == 300
    assert job.task.correlation == job.task.correlation.__class__("experiment-1", "job-1", 2)


def test_platform_backend_refuses_pending_contract_result() -> None:
    from benchmark_worker.contracts import JobResult, TaskRun, TaskRunState
    from benchmark_worker.platform import HTTPPlatformWorkerBackend

    transport = FakeTransport([])
    backend = HTTPPlatformWorkerBackend(PlatformClient("http://platform.test", transport=transport), "project-token")
    result = JobResult("job-1", TaskRun("task-1", TaskRunState.RUNNING), "pending", None)

    with pytest.raises(ValueError, match="official verifier verdict"):
        backend.submit_result("harbor-local", result)
    assert transport.calls == []


def test_platform_submits_only_official_verifier_metrics_and_safe_failure() -> None:
    from benchmark_worker.contracts import JobResult, TaskRun, TaskRunState

    token = "project-token-that-must-not-print"
    transport = FakeTransport([json_response({"status": "ok"}), json_response({"status": "ok"})])
    client = PlatformClient("http://platform.test", transport=transport)
    result = JobResult(
        "job-1",
        TaskRun("task-1", TaskRunState.SUCCEEDED, final_text="done"),
        "passed",
        1.0,
        official_metrics={"reward": 1.0, "checks": 3},
        artifact_manifest=({"kind": "harbor_result", "uri": "file:///safe/result.json", "content_type": "application/json"},),
    )
    client.submit_result(token, "harbor-local", result)
    client.fail_job(token, "harbor-local", "job-2", "verifier_error")

    submitted = json.loads(transport.calls[0][3] or b"{}")
    assert submitted["benchmark_result"] == {
        "valid": True,
        "official_metrics": {"reward": 1.0, "checks": 3},
        "artifact_manifest": [{"kind": "harbor_result", "uri": "file:///safe/result.json", "content_type": "application/json"}],
    }
    failed = json.loads(transport.calls[1][3] or b"{}")
    assert failed["benchmark_result"]["valid"] is False
    assert failed["benchmark_result"]["error_type"] == "verifier_error"
    assert token not in (transport.calls[0][3] or b"").decode("utf-8")
    assert token not in (transport.calls[1][3] or b"").decode("utf-8")


def test_platform_backend_preserves_snapshot_and_upstream_task_id() -> None:
    from benchmark_worker.platform import HTTPPlatformWorkerBackend

    backend = HTTPPlatformWorkerBackend(PlatformClient("http://platform.test", transport=FakeTransport([])), "project-token")
    job = backend._leased_job({
        "job": {
            "id": "job-1", "experiment_id": "experiment-1", "dataset_case_id": "case-uuid", "trial_index": 0,
            "execution_config": {
                "task_timeout_s": 120,
                "agent_profile": {"id": "openhands-default", "config": {"max_iterations": 50}},
                "model_profile": {"id": "model-fixed", "config": {"model": "openai/example"}},
                "agent_artifact": {"framework": "openhands", "image_digest": "sha256:" + "a" * 64},
            },
        },
        "case_input": "Repair task.",
        "case_metadata": {"task_id": "terminal-bench/task-1"},
    })
    assert job.task.task_id == "terminal-bench/task-1"
    assert job.task.agent_profile_id == "openhands-default"
    assert job.task.model_profile_id == "model-fixed"
    assert job.task.execution_config is not None
    assert job.task.execution_config["agent_artifact"]["framework"] == "openhands"
