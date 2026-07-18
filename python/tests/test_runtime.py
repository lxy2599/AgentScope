"""Tests for Agent Runtime (agent registration and heartbeat)."""

import json
import time

import pytest


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
                "201", request=request,
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

        rt1 = init(server_url="http://localhost:8080", project_token="tok", agent_name="a1", instance_id="i1",
                   heartbeat_interval=60)
        assert rt1 is not None

        with pytest.raises(RuntimeError, match="already initialized"):
            init(server_url="http://localhost:8080", project_token="tok", agent_name="a2")

        shutdown_runtime()

    def test_shutdown_without_init(self):
        from agentscope_sdk.runtime import shutdown_runtime

        # Should not raise
        shutdown_runtime()
