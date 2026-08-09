"""Tests for Runner HTTP client."""

import pytest


@pytest.fixture
def claim_response_json():
    """Sample claim response that matches the backend's ClaimResponse."""
    return {
        "job": {
            "id": "job-0001",
            "experiment_id": "exp-0001",
            "dataset_case_id": "case-0001",
            "status": "running",
            "runner_name": "my-runner",
            "lease_expires_at": "2026-07-15T12:00:00Z",
            "attempts": 1,
            "error": "",
            "created_at": "2026-07-15T11:00:00Z",
            "updated_at": "2026-07-15T11:00:00Z",
        },
        "status": "running",
        "lease_duration": "2m0s",
        "case_input": "What is the capital of France?",
        "case_expected": "Paris",
        "case_metadata": {"source": "test"},
        "target_name": "support-agent",
        "target_version": "1.0",
        "eval_config": {"type": "exact"},
    }


class TestClaimJob:
    def test_claim_job_returns_job(self, httpx_mock, claim_response_json):
        from agentscope_sdk.client import RunnerClient

        httpx_mock.add_response(
            url="http://localhost:8080/runner/jobs/claim?runner_name=my-runner",
            method="GET",
            status_code=200,
            json=claim_response_json,
        )

        client = RunnerClient(
            server_url="http://localhost:8080",
            project_token="test-token",
            runner_name="my-runner",
        )
        job = client.claim_job()
        assert job is not None
        assert job.case_input == "What is the capital of France?"
        assert job.target_name == "support-agent"
        assert job.target_version == "1.0"
        assert job.execution_kind == "function"
        assert job.execution_config == {}

    def test_claim_job_deserializes_optional_benchmark_execution_fields(self, httpx_mock, claim_response_json):
        """New benchmark fields remain optional for older server responses."""
        from agentscope_sdk.client import RunnerClient

        claim_response_json["execution_kind"] = "native_benchmark"
        claim_response_json["execution_config"] = {
            "benchmark": "native-contract",
            "task_id": "task-42",
        }
        httpx_mock.add_response(
            url="http://localhost:8080/runner/jobs/claim?runner_name=my-runner",
            method="GET",
            status_code=200,
            json=claim_response_json,
        )

        client = RunnerClient(
            server_url="http://localhost:8080",
            project_token="test-token",
            runner_name="my-runner",
        )
        job = client.claim_job()

        assert job is not None
        assert job.execution_kind == "native_benchmark"
        assert job.execution_config == {"benchmark": "native-contract", "task_id": "task-42"}

    def test_claim_job_supports_execution_fields_nested_under_job(self, httpx_mock, claim_response_json):
        """Accept the same optional fields while the backend schema evolves."""
        from agentscope_sdk.client import RunnerClient

        claim_response_json["job"]["execution_kind"] = "benchmark_contract"
        claim_response_json["job"]["execution_config"] = {"task_id": "nested-task"}
        httpx_mock.add_response(
            url="http://localhost:8080/runner/jobs/claim?runner_name=my-runner",
            method="GET",
            status_code=200,
            json=claim_response_json,
        )

        client = RunnerClient(
            server_url="http://localhost:8080",
            project_token="test-token",
            runner_name="my-runner",
        )
        job = client.claim_job()

        assert job is not None
        assert job.execution_kind == "benchmark_contract"
        assert job.execution_config == {"task_id": "nested-task"}

    def test_claim_job_no_jobs_returns_none(self, httpx_mock):
        from agentscope_sdk.client import RunnerClient

        httpx_mock.add_response(
            url="http://localhost:8080/runner/jobs/claim?runner_name=my-runner",
            method="GET",
            status_code=404,
            json={"error": "no_jobs", "message": "No claimable jobs available"},
        )

        client = RunnerClient(
            server_url="http://localhost:8080",
            project_token="test-token",
            runner_name="my-runner",
        )
        job = client.claim_job()
        assert job is None

    def test_claim_job_sends_auth_header(self, httpx_mock):
        from agentscope_sdk.client import RunnerClient

        request_history = []

        def store_request(request):
            request_history.append(request)
            import httpx

            raise httpx.HTTPStatusError(
                "404",
                request=request,
                response=httpx.Response(404, request=request),
            )

        httpx_mock.add_callback(store_request, url="http://localhost:8080/runner/jobs/claim?runner_name=my-runner")

        client = RunnerClient(
            server_url="http://localhost:8080",
            project_token="my-secret-token",
            runner_name="my-runner",
        )
        try:
            client.claim_job()
        except Exception:
            pass

        assert len(request_history) == 1
        auth = request_history[0].headers.get("Authorization")
        assert auth == "Bearer my-secret-token"

    def test_claim_job_runner_name_in_query(self, httpx_mock):
        """runner_name should be passed as query parameter to match backend."""
        from agentscope_sdk.client import RunnerClient

        request_history = []

        def store_request(request):
            request_history.append(request)
            import httpx

            raise httpx.HTTPStatusError("404", request=request, response=httpx.Response(404, request=request))

        httpx_mock.add_callback(store_request, url="http://localhost:8080/runner/jobs/claim?runner_name=custom-runner")

        client = RunnerClient(
            server_url="http://localhost:8080",
            project_token="token",
            runner_name="custom-runner",
        )
        try:
            client.claim_job()
        except Exception:
            pass

        assert len(request_history) == 1
        assert "runner_name=custom-runner" in str(request_history[0].url)

    def test_claim_job_sends_deduplicated_execution_kind_filters(self, httpx_mock, claim_response_json):
        from agentscope_sdk.client import RunnerClient

        request_history = []

        def store_request(request):
            request_history.append(request)
            import httpx

            raise httpx.HTTPStatusError("404", request=request, response=httpx.Response(404, request=request))

        httpx_mock.add_callback(store_request)
        client = RunnerClient(
            server_url="http://localhost:8080",
            project_token="token",
            runner_name="benchmark-runner",
        )

        try:
            client.claim_job(execution_kinds=["harbor", "TAU2", "harbor", ""])
        except Exception:
            pass

        assert len(request_history) == 1
        query = request_history[0].url.params
        assert query.get_list("execution_kind") == ["harbor", "tau2"]
        assert query["runner_name"] == "benchmark-runner"

    def test_claim_job_401_raises_error(self, httpx_mock):
        from agentscope_sdk.client import RunnerClient, RunnerAPIError

        httpx_mock.add_response(
            url="http://localhost:8080/runner/jobs/claim?runner_name=my-runner",
            method="GET",
            status_code=401,
            json={"error": "unauthorized", "message": "Invalid token"},
        )

        client = RunnerClient(
            server_url="http://localhost:8080",
            project_token="bad-token",
            runner_name="my-runner",
        )
        with pytest.raises(RunnerAPIError) as excinfo:
            client.claim_job()
        # Error message must not contain token
        assert "bad-token" not in str(excinfo.value)


class TestHeartbeat:
    def test_heartbeat_sends_correct_body(self, httpx_mock):
        from agentscope_sdk.client import RunnerClient
        import json

        request_history = []

        def store_request(request):
            request_history.append(request)
            import httpx

            return httpx.Response(200, json={"status": "ok"}, request=request)

        httpx_mock.add_callback(
            store_request,
            url="http://localhost:8080/runner/jobs/job-001/heartbeat",
            method="POST",
        )

        client = RunnerClient(
            server_url="http://localhost:8080",
            project_token="token",
            runner_name="my-runner",
        )
        client.heartbeat("job-001")
        assert len(request_history) == 1
        body = json.loads(request_history[0].content)
        assert body["runner_name"] == "my-runner"


def test_submit_judge_votes_returns_server_aggregation(httpx_mock):
    from agentscope_sdk.client import RunnerClient
    import json

    httpx_mock.add_response(
        url="http://localhost:8080/runner/runs/run-1/judge-votes",
        method="POST",
        status_code=201,
        json={"id": "agg-1", "result": {"score": 0.8, "passed": True}},
    )
    client = RunnerClient(
        server_url="http://localhost:8080",
        project_token="token",
        runner_name="runner",
    )
    result = client.submit_judge_votes(
        "run-1",
        metric_name="quality",
        metric_kind="final_answer",
        votes=[
            {
                "provider": "fake",
                "score": 0.8,
                "passed": True,
                "details": {"dimension_scores": {"accuracy": 0.9}},
            }
        ],
        aggregation={"method": "mean", "min_valid_votes": 1},
    )
    assert result["id"] == "agg-1"
    request = httpx_mock.get_request()
    assert request is not None
    body = json.loads(request.content)
    assert body["votes"][0]["details"] == json.dumps(
        {"dimension_scores": {"accuracy": 0.9}},
        ensure_ascii=False,
        separators=(",", ":"),
        sort_keys=True,
    )


class TestSubmitResult:
    def test_submit_result_sends_payload(self, httpx_mock):
        from agentscope_sdk.client import RunnerClient
        import json

        request_history = []

        def store_request(request):
            request_history.append(request)
            import httpx

            return httpx.Response(200, json={"status": "ok"}, request=request)

        httpx_mock.add_callback(
            store_request,
            url="http://localhost:8080/runner/jobs/job-001/results",
            method="POST",
        )

        client = RunnerClient(
            server_url="http://localhost:8080",
            project_token="token",
            runner_name="my-runner",
        )
        from agentscope_sdk.client import JobResultPayload

        payload = JobResultPayload(
            run_id="run-001",
            score=0.95,
            passed=True,
            reason="correct answer",
            actual_output="Paris",
            latency_ms=500,
            cost=0.01,
        )
        client.submit_result("job-001", payload)

        assert len(request_history) == 1
        body = json.loads(request_history[0].content)
        assert body["runner_name"] == "my-runner"
        assert body["run_id"] == "run-001"
        assert body["score"] == 0.95
        assert body["passed"] is True
        assert body["reason"] == "correct answer"
        assert body["actual_output"] == "Paris"
        assert body["latency_ms"] == 500
        assert body["cost"] == 0.01

    def test_submit_result_sends_details(self, httpx_mock):
        """Submit result should include details in the request JSON."""
        from agentscope_sdk.client import RunnerClient
        import json

        request_history = []

        def store_request(request):
            request_history.append(request)
            import httpx

            return httpx.Response(200, json={"status": "ok"}, request=request)

        httpx_mock.add_callback(
            store_request,
            url="http://localhost:8080/runner/jobs/job-002/results",
            method="POST",
        )

        client = RunnerClient(
            server_url="http://localhost:8080",
            project_token="token",
            runner_name="my-runner",
        )
        from agentscope_sdk.client import JobResultPayload

        payload = JobResultPayload(
            run_id="run-002",
            score=1.0,
            passed=True,
            reason="correct",
            actual_output="output",
            details={"key": "val"},
        )
        client.submit_result("job-002", payload)

        assert len(request_history) == 1
        body = json.loads(request_history[0].content)
        assert body["details"] == {"key": "val"}

    def test_submit_result_serializes_benchmark_details(self, httpx_mock):
        """Benchmark metadata uses the existing details payload, not a new API."""
        from agentscope_sdk.client import JobResultPayload, RunnerClient
        import json

        request_history = []

        def store_request(request):
            request_history.append(request)
            import httpx

            return httpx.Response(200, json={"status": "ok"}, request=request)
        httpx_mock.add_callback(
            store_request,
            url="http://localhost:8080/runner/jobs/job-003/results",
            method="POST",
        )

        client = RunnerClient(
            server_url="http://localhost:8080",
            project_token="token",
            runner_name="my-runner",
        )
        client.submit_result(
            "job-003",
            JobResultPayload(
                score=1.0,
                passed=True,
                details={
                    "benchmark": {
                        "valid": True,
                        "official_metrics": {"task_success": 1.0},
                        "artifacts": [{"kind": "report", "uri": "file:///tmp/report.json"}],
                    },
                },
            ),
        )

        body = json.loads(request_history[0].content)
        assert body["details"]["benchmark"]["valid"] is True
        assert body["details"]["benchmark"]["artifacts"][0]["kind"] == "report"


class TestFailJob:
    def test_fail_job_sends_payload(self, httpx_mock):
        from agentscope_sdk.client import RunnerClient
        import json

        request_history = []

        def store_request(request):
            request_history.append(request)
            import httpx

            return httpx.Response(200, json={"status": "ok"}, request=request)

        httpx_mock.add_callback(
            store_request,
            url="http://localhost:8080/runner/jobs/job-001/fail",
            method="POST",
        )

        client = RunnerClient(
            server_url="http://localhost:8080",
            project_token="token",
            runner_name="my-runner",
        )
        client.fail_job("job-001", error_type="target_error", message="agent crashed")

        assert len(request_history) == 1
        body = json.loads(request_history[0].content)
        assert body["runner_name"] == "my-runner"
        assert body["error"] == "target_error: agent crashed"

    def test_fail_job_error_message_no_token(self, httpx_mock):
        """Error messages from RunnerAPIError must not contain the project token."""
        from agentscope_sdk.client import RunnerClient, RunnerAPIError

        httpx_mock.add_response(
            url="http://localhost:8080/runner/jobs/job-001/fail",
            method="POST",
            status_code=400,
            json={"error": "fail_failed", "message": "something went wrong"},
        )

        client = RunnerClient(
            server_url="http://localhost:8080",
            project_token="super-secret-token-abc123",
            runner_name="my-runner",
        )
        with pytest.raises(RunnerAPIError) as excinfo:
            client.fail_job("job-001", error_type="test", message="test")

        assert "super-secret-token-abc123" not in str(excinfo.value)


class TestURLHandling:
    def test_server_url_without_trailing_slash(self, httpx_mock):
        """URL without trailing slash should work correctly."""
        from agentscope_sdk.client import RunnerClient

        httpx_mock.add_response(
            url="http://localhost:8080/runner/jobs/claim?runner_name=my-runner",
            method="GET",
            status_code=404,
        )

        client = RunnerClient(
            server_url="http://localhost:8080",
            project_token="token",
            runner_name="my-runner",
        )
        result = client.claim_job()
        assert result is None

    def test_server_url_with_trailing_slash(self, httpx_mock):
        """URL with trailing slash should not produce double-slash paths."""
        from agentscope_sdk.client import RunnerClient

        httpx_mock.add_response(
            url="http://localhost:8080/runner/jobs/claim?runner_name=my-runner",
            method="GET",
            status_code=404,
        )

        client = RunnerClient(
            server_url="http://localhost:8080/",
            project_token="token",
            runner_name="my-runner",
        )
        result = client.claim_job()
        assert result is None
