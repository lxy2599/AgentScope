"""Tests for Agent Runtime (agent registration and heartbeat)."""

import json
import time

import pytest
import unittest
from unittest.mock import patch


class TestAgentClient:
    """Tests for AgentClient HTTP wrapper."""

    def test_register_agent(self, httpx_mock):
        from agentscope_sdk.client import AgentClient, AgentInfo

        httpx_mock.add_response(
            url="http://localhost:8080/api/v1/agents/register",
            method="POST",
            status_code=201,
            json={
                "id": "agent-001",
                "project_id": "proj-1",
                "name": "my-agent",
                "description": "My test agent",
                "status": "active",
                "version": 1,
                "metadata": {"framework": "langchain"},
                "created_at": "2026-07-18T12:00:00Z",
                "updated_at": "2026-07-18T12:00:00Z",
            },
        )

        client = AgentClient(
            server_url="http://localhost:8080",
            project_token="test-token",
        )
        info = client.register_agent("my-agent", "My test agent", {"framework": "langchain"})

        assert info.id == "agent-001"
        assert info.name == "my-agent"
        assert info.status == "active"

    def test_register_agent_minimal(self, httpx_mock):
        from agentscope_sdk.client import AgentClient

        httpx_mock.add_response(
            url="http://localhost:8080/api/v1/agents/register",
            method="POST",
            status_code=201,
            json={
                "id": "agent-002",
                "project_id": "proj-1",
                "name": "minimal-agent",
                "description": "",
                "status": "active",
                "version": 1,
                "metadata": {},
                "created_at": "2026-07-18T12:00:00Z",
                "updated_at": "2026-07-18T12:00:00Z",
            },
        )

        client = AgentClient(
            server_url="http://localhost:8080",
            project_token="test-token",
        )
        info = client.register_agent("minimal-agent")

        assert info.name == "minimal-agent"
        assert info.description == ""

    def test_register_agent_sends_auth(self, httpx_mock):
        from agentscope_sdk.client import AgentClient

        request_history = []

        def store_request(request):
            request_history.append(request)
            import httpx

            raise httpx.HTTPStatusError(
                "201",
                request=request,
                response=httpx.Response(201, json={"id": "x"}, request=request),
            )

        httpx_mock.add_callback(
            store_request,
            url="http://localhost:8080/api/v1/agents/register",
            method="POST",
        )

        client = AgentClient(
            server_url="http://localhost:8080",
            project_token="my-secret-token",
        )
        try:
            client.register_agent("test")
        except Exception:
            pass

        assert len(request_history) == 1
        auth = request_history[0].headers.get("Authorization")
        assert auth == "Bearer my-secret-token"

    def test_register_agent_error(self, httpx_mock):
        from agentscope_sdk.client import AgentClient, AgentAPIError

        httpx_mock.add_response(
            url="http://localhost:8080/api/v1/agents/register",
            method="POST",
            status_code=400,
            json={"error": "register_failed", "message": "Agent name already exists"},
        )

        client = AgentClient(
            server_url="http://localhost:8080",
            project_token="test-token",
        )
        with pytest.raises(AgentAPIError) as excinfo:
            client.register_agent("dup-agent")

        assert "already exists" in str(excinfo.value)

    def test_send_heartbeat(self, httpx_mock):
        from agentscope_sdk.client import AgentClient

        httpx_mock.add_response(
            url="http://localhost:8080/api/v1/agents/agent-001/heartbeat",
            method="POST",
            status_code=200,
            json={"status": "ok"},
        )

        client = AgentClient(
            server_url="http://localhost:8080",
            project_token="test-token",
        )
        client.send_heartbeat("agent-001", "inst-1", "online")

    def test_send_heartbeat_with_payload(self, httpx_mock):
        from agentscope_sdk.client import AgentClient
        import json as j

        request_history = []

        def store_request(request):
            request_history.append(request)
            return httpx.Response(200, json={"status": "ok"}, request=request)

        import httpx

        httpx_mock.add_callback(
            store_request,
            url="http://localhost:8080/api/v1/agents/agent-001/heartbeat",
            method="POST",
        )

        client = AgentClient(
            server_url="http://localhost:8080",
            project_token="test-token",
        )
        client.send_heartbeat("agent-001", "inst-1", "online", {"cpu": 0.5, "mem": 1024})

        assert len(request_history) == 1
        body = j.loads(request_history[0].content)
        assert body["instance_id"] == "inst-1"
        assert body["status"] == "online"
        assert body["payload"] == {"cpu": 0.5, "mem": 1024}

    def test_send_heartbeat_error(self, httpx_mock):
        from agentscope_sdk.client import AgentClient, AgentAPIError

        httpx_mock.add_response(
            url="http://localhost:8080/api/v1/agents/nonexistent/heartbeat",
            method="POST",
            status_code=400,
            json={"error": "heartbeat_failed", "message": "agent not found"},
        )

        client = AgentClient(
            server_url="http://localhost:8080",
            project_token="test-token",
        )
        with pytest.raises(AgentAPIError):
            client.send_heartbeat("nonexistent", "inst-1", "online")


class TestRuntime:
    """Tests for the Runtime context manager (init / shutdown / heartbeat loop)."""

    def _cleanup(self):
        """Ensure runtime is shut down between tests."""
        from agentscope_sdk.runtime import shutdown_runtime

        shutdown_runtime()

    def test_init_runtime(self, httpx_mock):
        from agentscope_sdk.runtime import init, get_runtime, shutdown_runtime

        self._cleanup()

        # Register returns agent info
        httpx_mock.add_response(
            url="http://localhost:8080/api/v1/agents/register",
            method="POST",
            status_code=201,
            json={
                "id": "agent-rt-001",
                "project_id": "proj-1",
                "name": "runtime-agent",
                "description": "Runtime test agent",
                "status": "active",
                "version": 1,
                "metadata": {},
                "created_at": "2026-07-18T12:00:00Z",
                "updated_at": "2026-07-18T12:00:00Z",
            },
        )

        heartbeat_url = "http://localhost:8080/api/v1/agents/agent-rt-001/heartbeat"

        import httpx

        httpx_mock.add_callback(
            lambda req: httpx.Response(200, json={"status": "ok"}, request=req),
            url=heartbeat_url,
            method="POST",
            is_reusable=True,
        )

        # Initialize runtime
        rt = init(
            server_url="http://localhost:8080",
            project_token="test-token",
            agent_name="runtime-agent",
            instance_id="inst-main",
        )

        assert rt is not None
        assert rt.agent_id == "agent-rt-001"
        assert rt.agent_name == "runtime-agent"
        assert rt.instance_id == "inst-main"

        # get_runtime returns the same instance
        rt2 = get_runtime()
        assert rt2 is rt

        # Clean shutdown stops heartbeat
        shutdown_runtime()

        # After shutdown, get_runtime returns None
        assert get_runtime() is None

    def test_init_uses_default_instance_id(self, httpx_mock):
        from agentscope_sdk.runtime import init, shutdown_runtime

        self._cleanup()

        httpx_mock.add_response(
            url="http://localhost:8080/api/v1/agents/register",
            method="POST",
            status_code=201,
            json={
                "id": "agent-002",
                "project_id": "proj-1",
                "name": "default-inst-agent",
                "status": "active",
                "version": 1,
                "metadata": {},
                "created_at": "2026-07-18T12:00:00Z",
                "updated_at": "2026-07-18T12:00:00Z",
            },
        )

        import httpx

        httpx_mock.add_callback(
            lambda req: httpx.Response(200, json={"status": "ok"}, request=req),
            url="http://localhost:8080/api/v1/agents/agent-002/heartbeat",
            method="POST",
            is_reusable=True,
        )

        rt = init(
            server_url="http://localhost:8080",
            project_token="test-token",
            agent_name="default-inst-agent",
        )

        assert rt.instance_id is not None
        assert len(rt.instance_id) > 0

        shutdown_runtime()

    def test_heartbeat_runs_on_interval(self, httpx_mock):
        """Verify that heartbeat is sent at the configured interval."""
        from agentscope_sdk.runtime import init, shutdown_runtime
        import threading

        self._cleanup()

        received_hbs = []
        lock = threading.Lock()

        httpx_mock.add_response(
            url="http://localhost:8080/api/v1/agents/register",
            method="POST",
            status_code=201,
            json={
                "id": "agent-hb-001",
                "project_id": "proj-1",
                "name": "hb-interval-agent",
                "status": "active",
                "version": 1,
                "metadata": {},
                "created_at": "2026-07-18T12:00:00Z",
                "updated_at": "2026-07-18T12:00:00Z",
            },
        )

        def hb_handler(request):
            with lock:
                received_hbs.append(1)
            return httpx.Response(200, json={"status": "ok"}, request=request)

        import httpx

        httpx_mock.add_callback(
            hb_handler,
            url="http://localhost:8080/api/v1/agents/agent-hb-001/heartbeat",
            method="POST",
            is_reusable=True,
        )

        rt = init(
            server_url="http://localhost:8080",
            project_token="test-token",
            agent_name="hb-interval-agent",
            instance_id="hb-inst",
            heartbeat_interval=0.05,  # 50ms for fast test
        )

        time.sleep(0.15)  # Wait for at least 2 heartbeat cycles

        shutdown_runtime()

        with lock:
            # Should have received at least 2 heartbeats
            assert len(received_hbs) >= 2, f"Expected >=2 heartbeats, got {len(received_hbs)}"

    def test_double_init_raises(self, httpx_mock):
        from agentscope_sdk.runtime import init, shutdown_runtime

        self._cleanup()

        httpx_mock.add_response(
            url="http://localhost:8080/api/v1/agents/register",
            method="POST",
            status_code=201,
            json={"id": "a1", "project_id": "p1", "name": "a1", "status": "active", "version": 1, "metadata": {}},
        )
        import httpx

        httpx_mock.add_callback(
            lambda req: httpx.Response(200, json={"status": "ok"}, request=req),
            url="http://localhost:8080/api/v1/agents/a1/heartbeat",
            method="POST",
            is_reusable=True,
        )

        rt1 = init(
            server_url="http://localhost:8080",
            project_token="tok",
            agent_name="a1",
            instance_id="i1",
            heartbeat_interval=60,
        )
        assert rt1 is not None

        with pytest.raises(RuntimeError, match="already initialized"):
            init(server_url="http://localhost:8080", project_token="tok", agent_name="a2")

        shutdown_runtime()

    def test_shutdown_without_init(self):
        from agentscope_sdk.runtime import shutdown_runtime

        # Should not raise
        shutdown_runtime()


# ---- Runtime run lifecycle tests ----


class TestRuntimeRunLifecycle:
    """Tests for runtime run lifecycle (start, update, finish, fail)."""

    def _make_runtime(self):
        """Helper to create a Runtime instance without server registration."""
        from agentscope_sdk.runtime import Runtime

        return Runtime(
            agent_id="agent-1",
            agent_name="test-agent",
            instance_id="inst-1",
            client=None,
            _base_url="http://server",
            _project_token="tok",
        )

    def test_start_run_contains_agent_and_instance_id(self, httpx_mock):
        rt = self._make_runtime()
        httpx_mock.add_response(
            method="POST",
            url="http://server/api/sdk/runs/start",
            json={"id": "run-1", "status": "running", "agent_id": "agent-1"},
            status_code=201,
        )

        run = rt.start_run(input="hello", trace_id="trace-1")

        assert run is not None
        assert run.id == "run-1"
        import json as _json

        payload = _json.loads(httpx_mock.get_request().content)
        assert payload["agent_id"] == "agent-1"
        assert payload["agent_instance_id"] == "inst-1"
        assert payload["input"] == "hello"

    def test_update_run_status_contains_required_fields(self, httpx_mock):
        rt = self._make_runtime()
        httpx_mock.add_response(
            method="POST",
            url="http://server/api/sdk/runs/start",
            json={"id": "run-1", "status": "running"},
            status_code=201,
        )
        rt.start_run(input="hello")

        httpx_mock.add_response(
            method="POST",
            url="http://server/api/sdk/runs/run-1/state",
            json={"id": "run-1", "status": "waiting_llm"},
            status_code=200,
        )

        result = rt.update_run_status("waiting_llm", message="calling LLM", attributes={"model": "glm"})

        assert result is True
        import json as _json

        requests = httpx_mock.get_requests()
        payload = _json.loads(requests[-1].content)
        assert payload["status"] == "waiting_llm"
        assert payload["message"] == "calling LLM"
        assert payload["agent_id"] == "agent-1"
        assert payload["instance_id"] == "inst-1"

    def test_finish_run_contains_output_and_agent_instance_id(self, httpx_mock):
        rt = self._make_runtime()
        httpx_mock.add_response(
            method="POST",
            url="http://server/api/sdk/runs/start",
            json={"id": "run-1", "status": "running"},
            status_code=201,
        )
        rt.start_run(input="hello")

        httpx_mock.add_response(
            method="POST",
            url="http://server/api/sdk/runs/run-1/finish",
            json={"id": "run-1", "status": "completed"},
            status_code=200,
        )

        result = rt.finish_run(output="world", metrics={"latency_ms": 500})

        assert result is True
        import json as _json

        requests = httpx_mock.get_requests()
        payload = _json.loads(requests[-1].content)
        assert payload["output"] == "world"
        assert payload["agent_id"] == "agent-1"
        assert payload["agent_instance_id"] == "inst-1"

    def test_finish_run_returns_false_on_network_error(self):
        import httpx

        from agentscope_sdk.runtime import RuntimeRun

        rt = self._make_runtime()
        rt._current_run = RuntimeRun(id="run-1")

        with patch("httpx.Client") as mock_client_class:
            mock_client = mock_client_class.return_value.__enter__.return_value
            mock_client.post.side_effect = httpx.ConnectError("network down")

            result = rt.finish_run(output="world")

        assert result is False
        assert isinstance(rt.last_error, httpx.ConnectError)

    def test_fail_run_contains_error_and_agent_instance_id(self, httpx_mock):
        rt = self._make_runtime()
        httpx_mock.add_response(
            method="POST",
            url="http://server/api/sdk/runs/start",
            json={"id": "run-1", "status": "running"},
            status_code=201,
        )
        rt.start_run(input="hello")

        httpx_mock.add_response(
            method="POST",
            url="http://server/api/sdk/runs/run-1/fail",
            json={"id": "run-1", "status": "failed"},
            status_code=200,
        )

        result = rt.fail_run(error="something broke", attributes={"reason": "timeout"})

        assert result is True
        import json as _json

        requests = httpx_mock.get_requests()
        payload = _json.loads(requests[-1].content)
        assert payload["error"] == "something broke"
        assert payload["agent_id"] == "agent-1"
        assert payload["agent_instance_id"] == "inst-1"

    def test_fail_run_returns_false_on_network_error(self):
        import httpx

        from agentscope_sdk.runtime import RuntimeRun

        rt = self._make_runtime()
        rt._current_run = RuntimeRun(id="run-1")

        with patch("httpx.Client") as mock_client_class:
            mock_client = mock_client_class.return_value.__enter__.return_value
            mock_client.post.side_effect = httpx.ConnectError("network down")

            result = rt.fail_run(error="something broke")

        assert result is False
        assert isinstance(rt.last_error, httpx.ConnectError)

    def test_run_context_finishes_on_success(self, httpx_mock):
        rt = self._make_runtime()

        httpx_mock.add_response(
            method="POST",
            url="http://server/api/sdk/runs/start",
            json={"id": "run-1", "status": "running"},
            status_code=201,
        )
        httpx_mock.add_response(
            method="POST",
            url="http://server/api/sdk/runs/run-1/finish",
            json={"id": "run-1", "status": "completed"},
            status_code=200,
        )

        with rt.run(input="hello world") as run:
            run.set_output("result output")

        import json as _json

        requests = httpx_mock.get_requests()
        assert len(requests) >= 2
        assert requests[0].url.path == "/api/sdk/runs/start"
        finish_req = [r for r in requests if "finish" in r.url.path]
        assert len(finish_req) == 1
        payload = _json.loads(finish_req[0].content)
        assert payload["output"] == "result output"

    def test_run_context_fails_and_reraises_on_exception(self, httpx_mock):
        rt = self._make_runtime()

        httpx_mock.add_response(
            method="POST",
            url="http://server/api/sdk/runs/start",
            json={"id": "run-1", "status": "running"},
            status_code=201,
        )
        httpx_mock.add_response(
            method="POST",
            url="http://server/api/sdk/runs/run-1/fail",
            json={"id": "run-1", "status": "failed"},
            status_code=200,
        )

        with pytest.raises(ValueError):
            with rt.run(input="hello"):
                raise ValueError("boom")

        import json as _json

        requests = httpx_mock.get_requests()
        fail_req = [r for r in requests if "fail" in r.url.path]
        assert len(fail_req) == 1
        payload = _json.loads(fail_req[0].content)
        assert payload["error"] == "boom"


class TestRuntimeTraceIDPayload(unittest.TestCase):
    """SDK runtime payload includes trace_id for all lifecycle APIs."""

    def setUp(self):
        from agentscope_sdk.runtime import Runtime

        self.rt = Runtime(
            agent_id="agent-1",
            agent_name="test-agent",
            instance_id="inst-1",
            client=unittest.mock.MagicMock(),
            _base_url="http://localhost:8080",
            _project_token="test-token",
        )
        self.rt._current_run = unittest.mock.MagicMock()
        self.rt._current_run.id = "run-123"
        self.rt._current_run.trace_id = "trace-abc-123"

    @patch("httpx.Client")
    def test_update_run_status_includes_trace_id(self, mock_client_class):
        """update_run_status payload includes trace_id from current run."""
        mock_client = unittest.mock.MagicMock()
        mock_client_class.return_value.__enter__.return_value = mock_client
        mock_response = unittest.mock.MagicMock()
        mock_response.json.return_value = {"status": "running"}
        mock_client.post.return_value = mock_response

        result = self.rt.update_run_status("waiting_llm", message="thinking")

        call_kwargs = mock_client.post.call_args[1]
        payload = call_kwargs.get("json", {})
        self.assertEqual(payload.get("trace_id"), "trace-abc-123")
        self.assertEqual(payload.get("status"), "waiting_llm")
        self.assertTrue(result)

    @patch("httpx.Client")
    def test_finish_run_includes_trace_id(self, mock_client_class):
        """finish_run payload includes trace_id."""
        mock_client = unittest.mock.MagicMock()
        mock_client_class.return_value.__enter__.return_value = mock_client
        mock_response = unittest.mock.MagicMock()
        mock_response.json.return_value = {"status": "completed"}
        mock_client.post.return_value = mock_response

        self.rt.finish_run(output="done", latency_ms=100)

        call_kwargs = mock_client.post.call_args[1]
        payload = call_kwargs.get("json", {})
        self.assertEqual(payload.get("trace_id"), "trace-abc-123")
        self.assertEqual(payload.get("latency_ms"), 100)

    @patch("httpx.Client")
    def test_fail_run_includes_trace_id(self, mock_client_class):
        """fail_run payload includes trace_id."""
        mock_client = unittest.mock.MagicMock()
        mock_client_class.return_value.__enter__.return_value = mock_client
        mock_response = unittest.mock.MagicMock()
        mock_response.json.return_value = {"status": "failed"}
        mock_client.post.return_value = mock_response

        self.rt.fail_run(error="something broke")

        call_kwargs = mock_client.post.call_args[1]
        payload = call_kwargs.get("json", {})
        self.assertEqual(payload.get("trace_id"), "trace-abc-123")
        self.assertIn("something broke", payload.get("error", ""))


if __name__ == "__main__":
    unittest.main()
