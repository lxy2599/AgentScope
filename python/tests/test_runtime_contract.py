"""Tests for Phase 3.1 Runtime Contract helpers."""

import pytest


class TestBuildAgentMetadata:
    """Tests for build_agent_metadata()."""

    def test_defaults_to_contract_v1(self):
        from agentscope_sdk.runtime import build_agent_metadata

        meta = build_agent_metadata(
            agent_version="1.2.3",
            framework="hermes",
            runtime="python",
            source="adapter",
            environment="dev",
            model_provider="glm",
            model_name="glm-4.7-flash",
            harness_version="harness-v1",
            prompt_version="prompt-v1",
            tags=["demo"],
            extra={"owner": "platform"},
        )

        assert meta["contract_version"] == "agent-runtime-v1"
        assert meta["agent_version"] == "1.2.3"
        assert meta["framework"] == "hermes"
        assert meta["runtime"] == "python"
        assert meta["source"] == "adapter"
        assert meta["environment"] == "dev"
        assert meta["model_provider"] == "glm"
        assert meta["model_name"] == "glm-4.7-flash"
        assert meta["harness_version"] == "harness-v1"
        assert meta["prompt_version"] == "prompt-v1"
        assert meta["tags"] == ["demo"]
        assert meta["owner"] == "platform"

    def test_minimal_metadata(self):
        from agentscope_sdk.runtime import build_agent_metadata

        meta = build_agent_metadata(agent_version="0.1.0")

        assert meta["contract_version"] == "agent-runtime-v1"
        assert meta["agent_version"] == "0.1.0"
        assert meta["framework"] == "custom-python"
        assert meta["runtime"] == "python"
        assert meta["source"] == "sdk"
        assert meta["environment"] == "dev"

    def test_extra_overrides_defaults(self):
        from agentscope_sdk.runtime import build_agent_metadata

        meta = build_agent_metadata(
            agent_version="2.0.0",
            framework="langgraph",
            extra={"framework": "overridden"},
        )

        assert meta["framework"] == "overridden"


class TestBuildRunMetadata:
    """Tests for build_run_metadata()."""

    def test_preserves_ids_and_flags(self):
        from agentscope_sdk.runtime import build_run_metadata

        meta = build_run_metadata(
            session_id="sess-1",
            conversation_id="conv-1",
            task_id="task-1",
            parent_run_id="parent-run",
            framework="openhands",
            runtime="python",
            source="adapter",
            environment="dev",
            model_provider="deepseek",
            model_name="deepseek-chat",
            harness_version="h1",
            prompt_version="p1",
            feature_flags={"use_rag": True},
            ablation_flags={"disable_tools": False},
            extra={"case_id": "case-1"},
        )

        assert meta["contract_version"] == "agent-runtime-v1"
        assert meta["session_id"] == "sess-1"
        assert meta["conversation_id"] == "conv-1"
        assert meta["task_id"] == "task-1"
        assert meta["parent_run_id"] == "parent-run"
        assert meta["feature_flags"]["use_rag"] is True
        assert meta["ablation_flags"]["disable_tools"] is False
        assert meta["case_id"] == "case-1"

    def test_minimal_run_metadata(self):
        from agentscope_sdk.runtime import build_run_metadata

        meta = build_run_metadata()

        assert meta["contract_version"] == "agent-runtime-v1"
        assert meta["feature_flags"] == {}
        assert meta["ablation_flags"] == {}


class TestNormalizeCapability:
    """Tests for normalize_capability()."""

    def test_normalizes_known_kind(self):
        from agentscope_sdk.runtime import normalize_capability

        cap = normalize_capability(
            kind="tool",
            name="query_order",
            version="1.0.0",
            description="查询订单",
            schema={"input": {"type": "object"}},
            side_effect_risk="low",
            metadata={"owner": "support"},
        )

        assert cap["name"] == "query_order"
        assert cap["description"] == "查询订单"
        assert cap["metadata"]["contract_version"] == "agent-runtime-v1"
        assert cap["metadata"]["kind"] == "tool"
        assert cap["metadata"]["version"] == "1.0.0"
        assert cap["metadata"]["schema"]["input"]["type"] == "object"
        assert cap["metadata"]["side_effect_risk"] == "low"

    def test_rejects_unknown_kind(self):
        from agentscope_sdk.runtime import normalize_capability

        with pytest.raises(ValueError, match="unsupported capability kind"):
            normalize_capability(kind="unknown", name="x")

    def test_requires_name(self):
        from agentscope_sdk.runtime import normalize_capability

        with pytest.raises(ValueError, match="capability name is required"):
            normalize_capability(kind="tool", name="")

    def test_all_known_kinds(self):
        from agentscope_sdk.runtime import normalize_capability, CAPABILITY_KINDS

        for kind in CAPABILITY_KINDS:
            cap = normalize_capability(kind=kind, name=f"test-{kind}")
            assert cap["metadata"]["kind"] == kind


class TestRunStatusConstants:
    """Tests for new run status constants."""

    def test_thinking_status(self):
        from agentscope_sdk.runtime import RUN_STATUS_THINKING

        assert RUN_STATUS_THINKING == "thinking"

    def test_waiting_memory_status(self):
        from agentscope_sdk.runtime import RUN_STATUS_WAITING_MEMORY

        assert RUN_STATUS_WAITING_MEMORY == "waiting_memory"

    def test_waiting_user_status(self):
        from agentscope_sdk.runtime import RUN_STATUS_WAITING_USER

        assert RUN_STATUS_WAITING_USER == "waiting_user"


class TestInitExtended:
    """Tests for extended init() parameters."""

    def test_init_accepts_contract_params(self, httpx_mock):
        from agentscope_sdk.runtime import init, shutdown_runtime

        # Clean up any previous runtime
        shutdown_runtime()

        httpx_mock.add_response(
            url="http://localhost:8080/api/v1/agents/register",
            method="POST",
            status_code=201,
            json={
                "id": "agent-ct-001",
                "project_id": "proj-1",
                "name": "contract-agent",
                "description": "Runtime Contract test",
                "status": "active",
                "version": 1,
                "metadata": {
                    "contract_version": "agent-runtime-v1",
                    "agent_version": "1.0.0",
                    "framework": "custom-python",
                },
                "created_at": "2026-07-22T00:00:00Z",
                "updated_at": "2026-07-22T00:00:00Z",
            },
        )

        import httpx

        # Capabilities endpoint mock
        httpx_mock.add_callback(
            lambda req: httpx.Response(201, json={"status": "created"}, request=req),
            url="http://localhost:8080/api/v1/agents/agent-ct-001/capabilities",
            method="POST",
            is_reusable=True,
        )
        # Heartbeat endpoint mock
        httpx_mock.add_callback(
            lambda req: httpx.Response(200, json={"status": "ok"}, request=req),
            url="http://localhost:8080/api/v1/agents/agent-ct-001/heartbeat",
            method="POST",
            is_reusable=True,
        )

        rt = init(
            server_url="http://localhost:8080",
            project_token="test-token",
            agent_name="contract-agent",
            agent_version="1.0.0",
            framework="custom-python",
            runtime="python",
            source="sdk",
            environment="dev",
            model_provider="glm",
            model_name="glm-4.7-flash",
            harness_version="enterprise-demo-v1",
            prompt_version="support-policy-v1",
            tags=["demo", "agentops"],
            capabilities=[
                {
                    "name": "query_order",
                    "description": "查询订单",
                    "metadata": {
                        "contract_version": "agent-runtime-v1",
                        "kind": "tool",
                        "version": "1.0.0",
                    },
                },
            ],
        )

        assert rt is not None

        # Verify agent registration metadata was sent
        import json

        requests = httpx_mock.get_requests()
        reg_request = next(r for r in requests if "register" in str(r.url))
        body = json.loads(reg_request.content)
        assert body["metadata"]["contract_version"] == "agent-runtime-v1"
        assert body["metadata"]["agent_version"] == "1.0.0"
        assert body["metadata"]["framework"] == "custom-python"

        shutdown_runtime()

    def test_start_run_accepts_contract_params(self, httpx_mock):
        from agentscope_sdk.runtime import Runtime

        rt = Runtime(
            agent_id="agent-1",
            agent_name="test-agent",
            instance_id="inst-1",
            client=None,
            _base_url="http://server",
            _project_token="tok",
        )

        httpx_mock.add_response(
            method="POST",
            url="http://server/api/sdk/runs/start",
            json={"id": "run-1", "status": "running"},
            status_code=201,
        )

        run = rt.start_run(
            input="hello",
            trace_id="trace-1",
            session_id="sess-1",
            conversation_id="conv-1",
            task_id="task-1",
            parent_run_id="",
            feature_flags={"use_rag": True},
            ablation_flags={"disable_tools": False},
            metadata={"case_id": "case-1"},
        )

        assert run is not None
        import json

        payload = json.loads(httpx_mock.get_request().content)
        assert payload["metadata"]["contract_version"] == "agent-runtime-v1"
        assert payload["metadata"]["session_id"] == "sess-1"
        assert payload["metadata"]["task_id"] == "task-1"
        assert payload["metadata"]["feature_flags"]["use_rag"] is True


class TestRunMetadataInheritsInitDefaults:
    """Tests that start_run() inherits contract defaults from init()."""

    def _make_runtime_with_defaults(self):
        """Create a Runtime with contract defaults like init() would."""
        from agentscope_sdk.runtime import Runtime

        rt = Runtime(
            agent_id="agent-1",
            agent_name="test-agent",
            instance_id="inst-1",
            client=None,
            _base_url="http://server",
            _project_token="tok",
            _framework="custom-python",
            _runtime="python",
            _source="demo",
            _environment="dev",
            _model_provider="glm",
            _model_name="glm-4.7-flash",
            _harness_version="demo-v1",
            _prompt_version="demo-p1",
            _agent_version="2.0.0",
        )
        return rt

    def test_start_run_uses_runtime_defaults(self, httpx_mock):
        rt = self._make_runtime_with_defaults()

        httpx_mock.add_response(
            method="POST",
            url="http://server/api/sdk/runs/start",
            json={"id": "run-1", "status": "running"},
            status_code=201,
        )

        rt.start_run(input="hello")

        import json

        payload = json.loads(httpx_mock.get_request().content)
        meta = payload["metadata"]
        assert meta["source"] == "demo"
        assert meta["model_provider"] == "glm"
        assert meta["model_name"] == "glm-4.7-flash"
        assert meta["framework"] == "custom-python"
        assert meta["runtime"] == "python"
        assert meta["environment"] == "dev"
        assert meta["harness_version"] == "demo-v1"
        assert meta["prompt_version"] == "demo-p1"
        assert meta["agent_version"] == "2.0.0"

    def test_start_run_explicit_overrides_defaults(self, httpx_mock):
        rt = self._make_runtime_with_defaults()

        httpx_mock.add_response(
            method="POST",
            url="http://server/api/sdk/runs/start",
            json={"id": "run-1", "status": "running"},
            status_code=201,
        )

        rt.start_run(input="hello", metadata={"source": "override-src"})

        import json

        payload = json.loads(httpx_mock.get_request().content)
        meta = payload["metadata"]
        # Explicit metadata overrides the default
        assert meta["source"] == "override-src"
        # Non-overridden fields still come from defaults
        assert meta["model_provider"] == "glm"
        assert meta["framework"] == "custom-python"


class TestHeartbeatPayload:
    """Tests that send_heartbeat sends structured instance metadata."""

    def test_heartbeat_contains_instance_metadata(self, httpx_mock):
        from agentscope_sdk.runtime import Runtime
        from agentscope_sdk.client import AgentClient
        import json

        client = AgentClient(
            server_url="http://server",
            project_token="tok",
        )

        rt = Runtime(
            agent_id="agent-1",
            agent_name="test-agent",
            instance_id="inst-1",
            client=client,
            _base_url="http://server",
            _project_token="tok",
            _framework="custom-python",
            _runtime="python",
            _source="demo",
            _environment="dev",
            _process_start_time="2026-07-22T00:00:00Z",
        )

        request_body = {}

        def capture_request(request):
            nonlocal request_body
            import httpx

            request_body = json.loads(request.content)
            return httpx.Response(200, json={"status": "ok"}, request=request)

        httpx_mock.add_callback(
            capture_request,
            url="http://server/api/v1/agents/agent-1/heartbeat",
            method="POST",
            is_reusable=True,
        )

        rt.send_heartbeat()

        assert request_body["instance_id"] == "inst-1"
        assert request_body["status"] == "online"
        payload = request_body.get("payload", {})
        assert payload.get("contract_version") == "agent-runtime-v1"
        assert payload.get("runtime") == "python"
        assert payload.get("framework") == "custom-python"
        assert payload.get("environment") == "dev"
        assert payload.get("hostname") is not None
        assert payload.get("pid") is not None
        assert payload.get("process_start_time") is not None
        assert payload.get("sdk_version") is not None
        assert "capacity" in payload
        assert "inflight_runs" in payload
        assert "export_metrics" in payload
