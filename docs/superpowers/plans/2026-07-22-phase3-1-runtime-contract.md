# Phase 3.1 Runtime Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `using-git-worktrees` before implementation. Use `test-driven-development` for every task. Use `verification-before-completion` before claiming complete. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 标准化 AgentScope 的通用 Agent Runtime Contract，让任意 Agent 通过 Python SDK 或后续 adapter 接入时，都能稳定表达 agent、instance、capability、session、conversation、task、run、trace、framework、runtime、source 和版本信息。

**Architecture:** 本阶段做兼容式扩展，不做破坏性 schema 重构。PostgreSQL 已有 `agents.metadata`、`agent_instances.metadata`、`agent_capabilities.metadata`、`runs.metadata`，先把标准 contract 写入这些 JSONB 字段，并在 Go/Python/前端定义强类型读写边界。后续如果某些字段成为高频查询，再单独迁移成专栏字段。

**Tech Stack:** Go、PostgreSQL/sqlc、Python SDK、React/Vite/Ant Design、React Query、Go test、pytest、Vitest。

## Global Constraints

- 所有提交信息必须使用中文。
- 开发必须在独立 git worktree 中完成，不直接污染主工作区。
- 每个 Task 至少一个 commit。
- 每个实现任务必须先写失败测试，再实现，再验证。
- 不允许只服务 enterprise demo、support demo 或某一个开源 Agent；contract 必须面向任意 Agent。
- 不删除兼容字段：`agent_instance_id` 仍作为 `instance_id` 的兼容 alias 保留。
- 本阶段不做破坏性数据库迁移；优先使用已有 JSONB metadata。
- 实现完成后必须跑完整 demo，并查询 PostgreSQL/ClickHouse 验证数据正确。

---

## 1. Runtime Contract 定义

### 1.1 Agent metadata 标准字段

写入 `agents.metadata`：

```json
{
  "contract_version": "agent-runtime-v1",
  "agent_version": "1.0.0",
  "framework": "custom-python",
  "runtime": "python",
  "source": "sdk",
  "environment": "dev",
  "model_provider": "glm",
  "model_name": "glm-4.7-flash",
  "harness_version": "enterprise-demo-v1",
  "prompt_version": "support-policy-v1",
  "tags": ["demo", "customer-support"]
}
```

字段含义：

- `contract_version`: 当前固定为 `agent-runtime-v1`。
- `agent_version`: 用户定义的 Agent 版本，字符串，不等同于现有 `agents.version` 整数。
- `framework`: `custom-python`、`langgraph`、`openhands`、`hermes`、`openclaw` 等。
- `runtime`: `python`、`node`、`go`、`container`、`desktop` 等。
- `source`: `sdk`、`adapter`、`runner`、`demo`。
- `environment`: `dev`、`staging`、`prod`、`local`。
- `model_provider` / `model_name`: 默认模型信息。
- `harness_version`: Harness/编排层版本。
- `prompt_version`: 默认 prompt 或 system prompt 版本。
- `tags`: 低基数标签，不放高频动态值。

### 1.2 Agent instance metadata 标准字段

写入 `agent_instances.metadata`：

```json
{
  "contract_version": "agent-runtime-v1",
  "runtime": "python",
  "framework": "custom-python",
  "environment": "dev",
  "hostname": "devbox",
  "pid": 12345,
  "process_start_time": "2026-07-22T10:00:00Z",
  "sdk_version": "0.1.0",
  "adapter_name": "",
  "adapter_version": "",
  "capacity": 1,
  "inflight_runs": 0
}
```

`hostname` 和 `pid` 仍写入现有列，同时也保留在 metadata 中，方便外部 adapter 上报非本机 runtime。

### 1.3 Capability schema

写入 `agent_capabilities.metadata`：

```json
{
  "contract_version": "agent-runtime-v1",
  "kind": "tool",
  "version": "1.0.0",
  "schema": {
    "input": {
      "type": "object",
      "properties": {
        "order_id": { "type": "string" }
      },
      "required": ["order_id"]
    },
    "output": {
      "type": "object"
    }
  },
  "side_effect_risk": "medium",
  "metadata": {
    "description": "查询订单"
  }
}
```

允许的 `kind`：

- `tool`
- `skill`
- `knowledge_base`
- `memory`
- `guardrail`
- `model`

### 1.4 Run metadata 标准字段

写入 `runs.metadata`：

```json
{
  "contract_version": "agent-runtime-v1",
  "session_id": "sess_123",
  "conversation_id": "conv_123",
  "task_id": "task_123",
  "parent_run_id": "",
  "agent_id": "uuid",
  "instance_id": "instance-abc",
  "framework": "custom-python",
  "runtime": "python",
  "source": "sdk",
  "environment": "dev",
  "model_provider": "glm",
  "model_name": "glm-4.7-flash",
  "harness_version": "enterprise-demo-v1",
  "prompt_version": "support-policy-v1",
  "feature_flags": {
    "use_rag": true,
    "use_safety_guard": true
  },
  "ablation_flags": {
    "disable_memory": false,
    "disable_tools": false
  }
}
```

这些字段为后续 OpenClaw/OpenHands/Hermes 接入、实验对比、消融和 prompt 版本评估打基础。

### 1.5 Run 状态扩展

当前已有：

- `queued`
- `running`
- `waiting_llm`
- `waiting_tool`
- `waiting_retrieval`
- `retrying`
- `completed`
- `failed`
- `timeout`
- `cancelled`

本阶段新增非终态：

- `thinking`
- `waiting_memory`
- `waiting_user`

新增状态必须同时更新：

- Go `internal/runs/runtime.go`
- Python `python/agentscope_sdk/runtime.py`
- 前端 `web/src/api/types.ts`
- React Query live polling status 列表
- SQL active run 查询

---

## 2. Files To Modify

### Python SDK

- Modify: `python/agentscope_sdk/runtime.py`
  - 新增 runtime contract dataclass/helper。
  - 扩展 `init(...)` 参数。
  - 扩展 `start_run(...)` 参数。
  - 扩展 heartbeat payload。
- Modify: `python/agentscope_sdk/client.py`
  - 扩展 `register_agent` payload。
  - 扩展 `send_heartbeat` payload。
  - 扩展 `set_capability` payload。
- Modify: `python/agentscope_sdk/__init__.py`
  - 导出 contract 类型和状态常量。
- Create: `python/tests/test_runtime_contract.py`
  - SDK contract 单元测试。

### Go backend

- Modify: `internal/httpapi/handlers.go`
  - 扩展 request/response DTO。
  - 扩展 `RunRuntimeStatus` 相关响应字段。
- Modify: `internal/httpapi/agent_handlers.go`
  - 接收并校验标准 metadata。
  - capability 支持 kind/version/schema。
- Modify: `internal/httpapi/sdk_runtime_handlers.go`
  - start/update/finish/fail run 接收标准 metadata。
- Modify: `internal/agents/service.go`
  - 合并并标准化 metadata。
  - 校验 capability kind。
- Modify: `internal/runs/runtime.go`
  - 扩展状态枚举。
  - start run 合并标准 metadata。
- Modify: `internal/adapter/agent_http.go`
  - response 暴露标准 metadata 字段。
- Modify: `internal/adapter/runtime_http.go`
  - request metadata 转 service input。
- Modify: `queries/runs.sql`
  - active run 查询包含新增状态。
- Regenerate: `internal/store/runs.sql.go`
- Test: `internal/agents/service_test.go`
- Test: `internal/runs/runtime_test.go`
- Test: `internal/httpapi/agent_handlers_test.go`
- Test: `internal/httpapi/sdk_runtime_handlers_test.go`

### Frontend

- Modify: `web/src/api/types.ts`
  - 新增 `AgentRuntimeMetadata`、`AgentCapabilityKind`、`RunRuntimeMetadata`。
  - 扩展 `RunRuntimeStatus`。
- Modify: `web/src/api/hooks.ts`
  - live polling status 包含新增非终态。
- Modify: `web/src/features/agents/AgentDetailPage.tsx`
  - 展示 framework/runtime/environment/agent_version/capability kind。
- Modify: `web/src/features/agents/AgentsPage.tsx`
  - 展示 framework/runtime/source/environment。
- Modify: `web/src/features/runs/ActiveRunsPage.tsx`
  - 展示 session/task/conversation/prompt/harness 信息摘要。
- Test: `web/src/api/__tests__/hooks.test.tsx`
- Test: `web/src/features/agents/agents.test.tsx`
- Test: `web/src/features/runs/active-runs.test.tsx`

---

## 3. Task 1: Python SDK Runtime Contract

**Goal:** 用户可以通过 SDK init/start_run 明确上报通用 contract 字段。

**Files:**

- Modify: `python/agentscope_sdk/runtime.py`
- Modify: `python/agentscope_sdk/client.py`
- Modify: `python/agentscope_sdk/__init__.py`
- Create: `python/tests/test_runtime_contract.py`

**Interfaces:**

- Produces: `build_agent_metadata(...) -> dict[str, Any]`
- Produces: `build_run_metadata(...) -> dict[str, Any]`
- Produces: `normalize_capability(...) -> dict[str, Any]`
- Consumes: Existing `AgentClient.register_agent`, `Runtime.start_run`

### Steps

- [ ] **Step 1: Write failing SDK metadata tests**

Create `python/tests/test_runtime_contract.py`:

```python
from agentscope_sdk.runtime import (
    build_agent_metadata,
    build_run_metadata,
    normalize_capability,
)


def test_build_agent_metadata_uses_runtime_contract_v1():
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


def test_build_run_metadata_preserves_ids_and_flags():
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


def test_normalize_capability_requires_known_kind():
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
    assert cap["metadata"]["kind"] == "tool"
    assert cap["metadata"]["version"] == "1.0.0"
    assert cap["metadata"]["schema"]["input"]["type"] == "object"
    assert cap["metadata"]["side_effect_risk"] == "low"


def test_normalize_capability_rejects_unknown_kind():
    try:
        normalize_capability(kind="unknown", name="x")
    except ValueError as exc:
        assert "unsupported capability kind" in str(exc)
    else:
        raise AssertionError("expected ValueError")
```

- [ ] **Step 2: Run failing test**

```bash
cd python
PYTHONPATH=. pytest -q tests/test_runtime_contract.py
```

Expected: FAIL because helpers do not exist.

- [ ] **Step 3: Implement metadata helpers**

In `python/agentscope_sdk/runtime.py`, add:

```python
from typing import Any

CONTRACT_VERSION = "agent-runtime-v1"
CAPABILITY_KINDS = {
    "tool",
    "skill",
    "knowledge_base",
    "memory",
    "guardrail",
    "model",
}


def _merge_non_empty(base: dict[str, Any], extra: dict[str, Any] | None) -> dict[str, Any]:
    result = {k: v for k, v in base.items() if v not in ("", None, [], {})}
    if extra:
        result.update(extra)
    return result


def build_agent_metadata(
    *,
    agent_version: str = "",
    framework: str = "custom-python",
    runtime: str = "python",
    source: str = "sdk",
    environment: str = "dev",
    model_provider: str = "",
    model_name: str = "",
    harness_version: str = "",
    prompt_version: str = "",
    tags: list[str] | None = None,
    extra: dict[str, Any] | None = None,
) -> dict[str, Any]:
    return _merge_non_empty(
        {
            "contract_version": CONTRACT_VERSION,
            "agent_version": agent_version,
            "framework": framework,
            "runtime": runtime,
            "source": source,
            "environment": environment,
            "model_provider": model_provider,
            "model_name": model_name,
            "harness_version": harness_version,
            "prompt_version": prompt_version,
            "tags": tags or [],
        },
        extra,
    )


def build_run_metadata(
    *,
    session_id: str = "",
    conversation_id: str = "",
    task_id: str = "",
    parent_run_id: str = "",
    framework: str = "custom-python",
    runtime: str = "python",
    source: str = "sdk",
    environment: str = "dev",
    model_provider: str = "",
    model_name: str = "",
    harness_version: str = "",
    prompt_version: str = "",
    feature_flags: dict[str, Any] | None = None,
    ablation_flags: dict[str, Any] | None = None,
    extra: dict[str, Any] | None = None,
) -> dict[str, Any]:
    return _merge_non_empty(
        {
            "contract_version": CONTRACT_VERSION,
            "session_id": session_id,
            "conversation_id": conversation_id,
            "task_id": task_id,
            "parent_run_id": parent_run_id,
            "framework": framework,
            "runtime": runtime,
            "source": source,
            "environment": environment,
            "model_provider": model_provider,
            "model_name": model_name,
            "harness_version": harness_version,
            "prompt_version": prompt_version,
            "feature_flags": feature_flags or {},
            "ablation_flags": ablation_flags or {},
        },
        extra,
    )


def normalize_capability(
    *,
    kind: str,
    name: str,
    version: str = "",
    description: str = "",
    schema: dict[str, Any] | None = None,
    side_effect_risk: str = "",
    metadata: dict[str, Any] | None = None,
) -> dict[str, Any]:
    if kind not in CAPABILITY_KINDS:
        raise ValueError(f"unsupported capability kind: {kind}")
    if not name:
        raise ValueError("capability name is required")
    cap_meta = {
        "contract_version": CONTRACT_VERSION,
        "kind": kind,
        "version": version,
        "schema": schema or {},
        "side_effect_risk": side_effect_risk,
        "metadata": metadata or {},
    }
    return {
        "name": name,
        "description": description,
        "metadata": _merge_non_empty(cap_meta, None),
    }
```

- [ ] **Step 4: Extend SDK init and run**

Extend `init(...)` with optional parameters:

```python
def init(
    server_url: str,
    project_token: str,
    agent_name: str,
    *,
    description: str = "",
    metadata: dict[str, Any] | None = None,
    instance_id: str | None = None,
    heartbeat_interval: float = 30.0,
    agent_version: str = "",
    framework: str = "custom-python",
    runtime: str = "python",
    source: str = "sdk",
    environment: str = "dev",
    model_provider: str = "",
    model_name: str = "",
    harness_version: str = "",
    prompt_version: str = "",
    tags: list[str] | None = None,
    capabilities: list[dict[str, Any]] | None = None,
) -> Runtime:
```

Inside `init`, build metadata:

```python
agent_meta = build_agent_metadata(
    agent_version=agent_version,
    framework=framework,
    runtime=runtime,
    source=source,
    environment=environment,
    model_provider=model_provider,
    model_name=model_name,
    harness_version=harness_version,
    prompt_version=prompt_version,
    tags=tags,
    extra=metadata,
)
```

Send `agent_meta` to `client.register_agent`.

After runtime is created, send capabilities:

```python
for capability in capabilities or []:
    client.set_capability(
        agent_id=agent_info.id,
        name=capability["name"],
        description=capability.get("description", ""),
        metadata=capability.get("metadata", {}),
    )
```

Extend `Runtime.start_run(...)`:

```python
def start_run(
    self,
    input: str = "",
    metadata: dict[str, Any] | None = None,
    trace_id: str = "",
    *,
    session_id: str = "",
    conversation_id: str = "",
    task_id: str = "",
    parent_run_id: str = "",
    feature_flags: dict[str, Any] | None = None,
    ablation_flags: dict[str, Any] | None = None,
) -> RuntimeRun | None:
```

Build run metadata by merging runtime defaults and per-run metadata.

- [ ] **Step 5: Export helpers**

In `python/agentscope_sdk/__init__.py`, export:

```python
from agentscope_sdk.runtime import (
    CONTRACT_VERSION,
    CAPABILITY_KINDS,
    build_agent_metadata,
    build_run_metadata,
    normalize_capability,
)
```

- [ ] **Step 6: Verify Python**

```bash
cd python
PYTHONPATH=. pytest -q tests/test_runtime_contract.py
python -m pytest
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add python/agentscope_sdk/runtime.py python/agentscope_sdk/client.py python/agentscope_sdk/__init__.py python/tests/test_runtime_contract.py
git commit -m "feat: 标准化 Python SDK Runtime Contract"
```

---

## 4. Task 2: Backend Contract Validation And Persistence

**Goal:** 后端接收 SDK contract，并稳定保存到 agent/run/capability metadata。

**Files:**

- Modify: `internal/httpapi/handlers.go`
- Modify: `internal/httpapi/agent_handlers.go`
- Modify: `internal/httpapi/sdk_runtime_handlers.go`
- Modify: `internal/agents/service.go`
- Modify: `internal/runs/runtime.go`
- Modify: `internal/adapter/agent_http.go`
- Modify: `internal/adapter/runtime_http.go`
- Test: `internal/agents/service_test.go`
- Test: `internal/runs/runtime_test.go`
- Test: `internal/httpapi/agent_handlers_test.go`
- Test: `internal/httpapi/sdk_runtime_handlers_test.go`

**Interfaces:**

- Consumes: SDK payload metadata from Task 1.
- Produces: persisted JSONB metadata with `contract_version=agent-runtime-v1`.

### Steps

- [ ] **Step 1: Write agent service tests**

Append to `internal/agents/service_test.go`:

```go
func TestRegisterAgentPreservesRuntimeContractMetadata(t *testing.T) {
    f := newAgentFixture(t)
    meta := map[string]interface{}{
        "contract_version": "agent-runtime-v1",
        "agent_version": "1.2.3",
        "framework": "hermes",
        "runtime": "python",
        "source": "adapter",
        "environment": "dev",
    }

    agent, err := f.svc.RegisterAgent(context.Background(), f.projID, "hermes-agent", "Hermes", meta)
    if err != nil {
        t.Fatalf("RegisterAgent: %v", err)
    }
    if agent.Metadata["contract_version"] != "agent-runtime-v1" {
        t.Fatalf("contract_version=%v", agent.Metadata["contract_version"])
    }
    if agent.Metadata["framework"] != "hermes" {
        t.Fatalf("framework=%v", agent.Metadata["framework"])
    }
}

func TestSetCapabilityRejectsUnknownKind(t *testing.T) {
    f := newAgentFixture(t)
    agent, err := f.svc.RegisterAgent(context.Background(), f.projID, "cap-agent", "", nil)
    if err != nil {
        t.Fatalf("RegisterAgent: %v", err)
    }

    err = f.svc.SetCapability(context.Background(), agent.ID, "bad-cap", "", map[string]interface{}{
        "kind": "unknown",
    })
    if err == nil {
        t.Fatal("expected unsupported capability kind error")
    }
}
```

- [ ] **Step 2: Write runtime service test**

Append to `internal/runs/runtime_test.go`:

```go
func TestRuntimeServiceStartRunPreservesContractMetadata(t *testing.T) {
    q := newFakeRuntimeQuerier()
    svc := NewRuntimeService(q)

    run, err := svc.StartRun(context.Background(), StartRunInput{
        ProjectID: "p1",
        Input: "hello",
        Metadata: map[string]interface{}{
            "contract_version": "agent-runtime-v1",
            "session_id": "sess-1",
            "conversation_id": "conv-1",
            "task_id": "task-1",
            "framework": "openhands",
            "feature_flags": map[string]interface{}{"use_rag": true},
        },
    })
    if err != nil {
        t.Fatalf("StartRun: %v", err)
    }

    stored := q.runs[run.ID]
    if stored.Metadata["session_id"] != "sess-1" {
        t.Fatalf("session_id=%v", stored.Metadata["session_id"])
    }
    if stored.Metadata["framework"] != "openhands" {
        t.Fatalf("framework=%v", stored.Metadata["framework"])
    }
}
```

Use the existing fake runtime querier fields in `internal/runs/runtime_test.go`; if its map is not exported, add a small helper method on the fake such as `getRunMetadata(runID string) map[string]interface{}` inside the test file.

- [ ] **Step 3: Write HTTP handler test**

Append to `internal/httpapi/sdk_runtime_handlers_test.go`:

```go
func TestSDKStartRunAcceptsRuntimeContractMetadata(t *testing.T) {
    deps, fakeSvc := newSDKRuntimeDepsWithToken("agent:runtime")
    body := `{
        "agent_id":"agent-1",
        "instance_id":"inst-1",
        "input":"hello",
        "metadata":{
            "contract_version":"agent-runtime-v1",
            "session_id":"sess-1",
            "task_id":"task-1",
            "framework":"hermes"
        }
    }`

    req := httptest.NewRequest(http.MethodPost, "/api/sdk/runs/start", strings.NewReader(body))
    req.Header.Set("Authorization", "Bearer test-token")
    rr := httptest.NewRecorder()

    handleSDKStartRun(deps).ServeHTTP(rr, req)

    if rr.Code != http.StatusCreated {
        t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
    }
    if fakeSvc.startReq.Metadata["session_id"] != "sess-1" {
        t.Fatalf("session_id=%v", fakeSvc.startReq.Metadata["session_id"])
    }
}
```

Use the existing helper names in `internal/httpapi/sdk_runtime_handlers_test.go`; if no helper exposes the captured request, add `startReq SDKStartRunRequest` to the fake runtime service and assign it inside `StartRun`.

- [ ] **Step 4: Add capability kind validation**

In `internal/agents/service.go`, add:

```go
var supportedCapabilityKinds = map[string]bool{
    "tool": true,
    "skill": true,
    "knowledge_base": true,
    "memory": true,
    "guardrail": true,
    "model": true,
}

func validateCapabilityMetadata(metadata map[string]interface{}) error {
    if metadata == nil {
        return nil
    }
    kindRaw, ok := metadata["kind"]
    if !ok {
        return nil
    }
    kind, ok := kindRaw.(string)
    if !ok || !supportedCapabilityKinds[kind] {
        return fmt.Errorf("unsupported capability kind %q", kindRaw)
    }
    return nil
}
```

Call it in `SetCapability` before marshal.

- [ ] **Step 5: Preserve metadata through adapters**

Ensure `internal/adapter/agent_http.go` and `internal/adapter/runtime_http.go` pass through metadata unchanged. If response DTOs already carry `Metadata`, do not strip fields.

- [ ] **Step 6: Verify Go**

```bash
go test ./internal/agents ./internal/runs ./internal/httpapi -count=1
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/httpapi/handlers.go internal/httpapi/agent_handlers.go internal/httpapi/sdk_runtime_handlers.go internal/agents/service.go internal/runs/runtime.go internal/adapter/agent_http.go internal/adapter/runtime_http.go internal/agents/service_test.go internal/runs/runtime_test.go internal/httpapi/agent_handlers_test.go internal/httpapi/sdk_runtime_handlers_test.go
git commit -m "feat: 后端保存 Runtime Contract 元数据"
```

---

## 5. Task 3: Run Status Expansion

**Goal:** 支持 `thinking`、`waiting_memory`、`waiting_user` 三个通用 Agent 状态，覆盖更多真实 Agent 执行阶段。

**Files:**

- Modify: `internal/runs/runtime.go`
- Modify: `queries/runs.sql`
- Regenerate: `internal/store/runs.sql.go`
- Modify: `python/agentscope_sdk/runtime.py`
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/hooks.ts`
- Test: `internal/runs/runtime_test.go`
- Test: `internal/store/store_test.go`
- Test: `python/tests/test_runtime_contract.py`
- Test: `web/src/api/__tests__/hooks.test.tsx`

**Interfaces:**

- Produces: new active statuses available across backend, SDK, frontend.

### Steps

- [ ] **Step 1: Write Go status tests**

Append to `internal/runs/runtime_test.go`:

```go
func TestRuntimeStatusExpansion(t *testing.T) {
    statuses := []string{"thinking", "waiting_memory", "waiting_user"}
    for _, status := range statuses {
        if !IsValidRunStatus(status) {
            t.Fatalf("status %s should be valid", status)
        }
        if IsTerminalRunStatus(status) {
            t.Fatalf("status %s should be non-terminal", status)
        }
    }
}
```

- [ ] **Step 2: Write frontend polling test**

Append to `web/src/api/__tests__/hooks.test.tsx`:

```tsx
import { getRunDetailRefetchInterval } from "../hooks";

it("polls expanded live runtime statuses", () => {
  expect(getRunDetailRefetchInterval("thinking")).toBe(2500);
  expect(getRunDetailRefetchInterval("waiting_memory")).toBe(2500);
  expect(getRunDetailRefetchInterval("waiting_user")).toBe(2500);
});
```

Place the test in the existing `web/src/api/__tests__/hooks.test.tsx` file and import from `../hooks`, which is the correct relative path from that directory.

- [ ] **Step 3: Add Python constants**

In `python/agentscope_sdk/runtime.py`, add:

```python
RUN_STATUS_THINKING = "thinking"
RUN_STATUS_WAITING_MEMORY = "waiting_memory"
RUN_STATUS_WAITING_USER = "waiting_user"
```

- [ ] **Step 4: Add Go statuses**

In `internal/runs/runtime.go`, add constants:

```go
RunStatusThinking      RunStatus = "thinking"
RunStatusWaitingMemory RunStatus = "waiting_memory"
RunStatusWaitingUser   RunStatus = "waiting_user"
```

Add them to:

- `IsValidRunStatus`
- `ActiveRunStatuses`

- [ ] **Step 5: Update SQL active run query**

In `queries/runs.sql`, update `ListActiveRunsByProject`:

```sql
AND r.status IN (
  'queued',
  'running',
  'thinking',
  'waiting_llm',
  'waiting_tool',
  'waiting_retrieval',
  'waiting_memory',
  'waiting_user',
  'retrying'
)
```

Then regenerate:

```bash
sqlc generate
```

- [ ] **Step 6: Update frontend types and polling**

In `web/src/api/types.ts`, extend `RunRuntimeStatus` with:

```ts
  | 'thinking'
  | 'waiting_memory'
  | 'waiting_user'
```

In `web/src/api/hooks.ts`, add these statuses to `LIVE_STATUSES`.

- [ ] **Step 7: Verify**

```bash
go test ./internal/runs ./internal/store -count=1
cd python && PYTHONPATH=. pytest -q tests/test_runtime_contract.py
cd ../web && npm test -- --run web/src/api/__tests__/hooks.test.tsx
```

- [ ] **Step 8: Commit**

```bash
git add internal/runs/runtime.go queries/runs.sql internal/store/runs.sql.go python/agentscope_sdk/runtime.py web/src/api/types.ts web/src/api/hooks.ts internal/runs/runtime_test.go internal/store/store_test.go python/tests/test_runtime_contract.py web/src/api/__tests__/hooks.test.tsx
git commit -m "feat: 扩展 Agent 运行状态"
```

---

## 6. Task 4: Frontend Contract Display

**Goal:** 前端控制中心和运行页能展示 contract 字段，用户能判断这个 Agent 来自哪个 framework/runtime/source/version。

**Files:**

- Modify: `web/src/api/types.ts`
- Modify: `web/src/features/agents/AgentsPage.tsx`
- Modify: `web/src/features/agents/AgentDetailPage.tsx`
- Modify: `web/src/features/runs/ActiveRunsPage.tsx`
- Test: `web/src/features/agents/agents.test.tsx`
- Test: `web/src/features/runs/active-runs.test.tsx`

**Interfaces:**

- Consumes: `AgentInfo.metadata`, `AgentCapabilityInfo.metadata`, `ActiveRun.metadata` if exposed later.
- Produces: User-visible contract display.

### Steps

- [ ] **Step 1: Add frontend types**

In `web/src/api/types.ts`, add:

```ts
export type AgentCapabilityKind =
  | "tool"
  | "skill"
  | "knowledge_base"
  | "memory"
  | "guardrail"
  | "model";

export interface AgentRuntimeMetadata {
  contract_version?: string;
  agent_version?: string;
  framework?: string;
  runtime?: string;
  source?: string;
  environment?: string;
  model_provider?: string;
  model_name?: string;
  harness_version?: string;
  prompt_version?: string;
  tags?: string[];
}

export interface CapabilityMetadata {
  contract_version?: string;
  kind?: AgentCapabilityKind;
  version?: string;
  schema?: Record<string, unknown>;
  side_effect_risk?: string;
  metadata?: Record<string, unknown>;
}
```

Update `AgentInfo.metadata` and `AgentCapabilityInfo.metadata` types from generic `Record<string, unknown>` to these interfaces where practical.

- [ ] **Step 2: Write Agent page test**

In `web/src/features/agents/agents.test.tsx`, add a fixture agent:

```tsx
{
  id: "agent-1",
  project_id: "project-1",
  name: "hermes-agent",
  description: "",
  status: "active",
  version: 1,
  metadata: {
    contract_version: "agent-runtime-v1",
    agent_version: "1.2.3",
    framework: "hermes",
    runtime: "python",
    source: "adapter",
    environment: "dev"
  },
  created_at: "2026-07-22T00:00:00Z",
  updated_at: "2026-07-22T00:00:00Z"
}
```

Assert rendered text contains:

- `hermes`
- `python`
- `adapter`
- `1.2.3`

- [ ] **Step 3: Write capability display test**

In `web/src/features/agents/agents.test.tsx`, for Agent Detail fixture, add capability metadata:

```tsx
{
  id: "cap-1",
  agent_id: "agent-1",
  name: "query_order",
  description: "查询订单",
  metadata: {
    kind: "tool",
    version: "1.0.0",
    side_effect_risk: "medium"
  },
  created_at: "2026-07-22T00:00:00Z"
}
```

Assert rendered text contains:

- `tool`
- `1.0.0`
- `medium`

- [ ] **Step 4: Implement display helpers**

In `AgentsPage.tsx`, add safe metadata access:

```ts
function agentMeta(agent: AgentInfo): AgentRuntimeMetadata {
  return (agent.metadata ?? {}) as AgentRuntimeMetadata;
}
```

Display:

- `framework || "-"`
- `runtime || "-"`
- `source || "-"`
- `agent_version || String(agent.version)`
- `environment || "-"`

In `AgentDetailPage.tsx`, display the same metadata plus capabilities kind/version/risk.

- [ ] **Step 5: Active run metadata display**

If backend response does not expose run metadata yet, do not invent API fields in this task. Add a small placeholder-free layout section that displays current status and agent/source fields already available. Run metadata expansion can land with Workstream B.

- [ ] **Step 6: Verify frontend**

```bash
cd web
npm test -- --run web/src/features/agents web/src/features/runs
npm run build
```

- [ ] **Step 7: Commit**

```bash
git add web/src/api/types.ts web/src/features/agents/AgentsPage.tsx web/src/features/agents/AgentDetailPage.tsx web/src/features/runs/ActiveRunsPage.tsx web/src/features/agents/agents.test.tsx web/src/features/runs/active-runs.test.tsx
git commit -m "feat: 前端展示 Agent Runtime Contract"
```

---

## 7. Task 5: Demo And Database Verification

**Goal:** 确认 Phase 3.1 不只是类型和页面变化，真实 demo 运行后数据库里的 contract 数据正确。

**Files:**

- Modify: `python/examples/enterprise_agentops_demo.py`
- Modify: `python/examples/generic_observability_agent.py`
- Modify: `scripts/demo-enterprise-agentops.sh` only if env variables need to be surfaced.
- Test: no new test file required, but must run end-to-end verification.

**Interfaces:**

- Consumes: SDK helpers from Task 1.
- Produces: Real demo data with contract metadata.

### Steps

- [ ] **Step 1: Locate demo files**

Run:

```bash
rg -n "企业级 AgentOps|generic|enterprise-ops-agent|agent_init\\(" python/examples scripts
```

Use the actual file paths found by `rg`; do not guess file names.

- [ ] **Step 2: Update enterprise demo SDK init**

In the enterprise demo, update `agent_init(...)` call:

```python
rt = agent_init(
    server_url=base_url,
    project_token=project_token,
    agent_name="enterprise-ops-agent",
    description="企业级 AgentOps 闭环演示 Agent",
    agent_version="1.0.0",
    framework="custom-python",
    runtime="python",
    source="demo",
    environment="dev",
    model_provider=provider,
    model_name=model,
    harness_version="enterprise-demo-v1",
    prompt_version="support-policy-v1",
    tags=["demo", "enterprise", "agentops"],
    capabilities=[
        normalize_capability(
            kind="tool",
            name="query_order",
            version="1.0.0",
            description="查询订单信息",
            side_effect_risk="low",
        ),
        normalize_capability(
            kind="knowledge_base",
            name="enterprise-policies",
            version="1.0.0",
            description="企业政策知识库",
            side_effect_risk="low",
        ),
        normalize_capability(
            kind="guardrail",
            name="safety_check",
            version="1.0.0",
            description="安全风险检查",
            side_effect_risk="low",
        ),
    ],
)
```

Use the provider/model variables already defined in `python/examples/enterprise_agentops_demo.py`; if the file uses different names, normalize them locally to `provider` and `model` before calling `agent_init`.

- [ ] **Step 3: Update run start metadata**

Where demo starts a run, pass:

```python
runtime.start_run(
    input=case_input,
    trace_id=trace_id,
    session_id=f"enterprise-demo-session-{demo_id}",
    conversation_id=f"enterprise-demo-conversation-{case_index}",
    task_id=str(case_id),
    feature_flags={
        "use_rag": True,
        "use_safety_guard": True,
        "use_tools": True,
    },
    ablation_flags={
        "disable_rag": False,
        "disable_tools": False,
        "disable_safety_guard": False,
    },
    metadata={
        "dataset_case_id": str(case_id),
        "demo_name": "enterprise-agentops",
    },
)
```

Use the actual runtime variable name in the demo.

- [ ] **Step 4: Run full verification**

Run:

```bash
go test ./...
cd python && python -m pytest
cd ../web && npm test -- --run && npm run build
cd ..
make demo-enterprise-agentops
```

Expected:

- tests pass;
- demo completes;
- no stale running runs from this demo.

- [ ] **Step 5: Verify PostgreSQL metadata**

Use the project ID printed by demo:

```bash
docker compose exec -T postgres psql -U agentscope -d agentscope -P pager=off -c "
SELECT name, metadata->>'contract_version' AS contract_version,
       metadata->>'agent_version' AS agent_version,
       metadata->>'framework' AS framework,
       metadata->>'runtime' AS runtime,
       metadata->>'source' AS source
FROM agents
WHERE project_id = '<PROJECT_ID_PRINTED_BY_DEMO>'
ORDER BY created_at DESC
LIMIT 5;"
```

Expected:

- `contract_version = agent-runtime-v1`
- `agent_version = 1.0.0`
- `framework = custom-python`
- `runtime = python`
- `source = demo`

Query capabilities:

```bash
docker compose exec -T postgres psql -U agentscope -d agentscope -P pager=off -c "
SELECT name, metadata->>'kind' AS kind, metadata->>'version' AS version,
       metadata->>'side_effect_risk' AS side_effect_risk
FROM agent_capabilities
WHERE agent_id = '<AGENT_ID_FROM_PREVIOUS_QUERY>'
ORDER BY name;"
```

Expected:

- at least one `tool`
- one `knowledge_base`
- one `guardrail`

Query runs:

```bash
docker compose exec -T postgres psql -U agentscope -d agentscope -P pager=off -c "
SELECT agent_name, status, trace_id,
       metadata->>'contract_version' AS contract_version,
       metadata->>'session_id' AS session_id,
       metadata->>'task_id' AS task_id,
       metadata->'feature_flags' AS feature_flags
FROM runs
WHERE project_id = '<PROJECT_ID_PRINTED_BY_DEMO>'
ORDER BY created_at DESC
LIMIT 10;"
```

Expected:

- completed demo runs have `contract_version = agent-runtime-v1`;
- `session_id` and `task_id` are non-empty;
- `feature_flags` includes `use_rag`, `use_safety_guard`, `use_tools`;
- no current demo run remains `running`.

- [ ] **Step 6: Verify ClickHouse still links trace**

```bash
docker compose exec -T clickhouse clickhouse-client --database agentscope_traces --query "
SELECT run_id, count()
FROM trace_spans
WHERE run_id != '00000000-0000-0000-0000-000000000000'
GROUP BY run_id
ORDER BY count() DESC
LIMIT 10;"
```

Expected:

- demo run IDs appear with nonzero span counts.

- [ ] **Step 7: Commit**

```bash
git add python/examples scripts
git commit -m "test: 验证 Runtime Contract 端到端链路"
```

---

## 8. Final Verification Checklist

Before reporting completion, run:

```bash
git status --short
go test ./...
cd python && python -m pytest
cd ../web && npm test -- --run && npm run build
cd ..
make demo-enterprise-agentops
```

Then provide:

- worktree path;
- commit hashes;
- commands run and results;
- demo login email/password;
- project URL;
- agent URL;
- at least three PostgreSQL verification query outputs;
- at least one ClickHouse verification query output;
- known limitations.

Completion is not valid if:

- only tests pass but demo not run;
- demo runs but metadata not verified in DB;
- metadata exists only in Python payload but not persisted;
- frontend displays hardcoded values;
- new statuses exist in Python but not backend/frontend;
- any demo run from this phase remains stuck in `running`.

---

## 9. Handoff Prompt For Another Agent

Use this exact prompt when assigning implementation:

```text
你在 /home/luxinyu/GolandProjects/AgentScope 工作。请使用 using-git-worktrees 创建独立 worktree，不要直接在主工作区开发。任务是实现 docs/superpowers/plans/2026-07-22-phase3-1-runtime-contract.md。

必须使用 test-driven-development：每个 Task 先写失败测试，再实现，再运行验证。完成前必须使用 verification-before-completion。

要求：
1. 所有提交信息使用中文。
2. 每个 Task 至少一个 commit。
3. 不做破坏性数据库迁移，优先使用已有 metadata JSONB。
4. 保留 agent_instance_id 兼容 alias，但新代码使用 instance_id。
5. Runtime Contract 必须面向任意 Agent，不允许写死 enterprise/support demo。
6. 实现完成后必须跑：
   - go test ./...
   - cd python && python -m pytest
   - cd web && npm test -- --run && npm run build
   - make demo-enterprise-agentops
7. 跑完 demo 后必须查询 PostgreSQL 和 ClickHouse，验证 agent metadata、capability metadata、run metadata、trace span 关联都正确。
8. 最终回复必须包含 worktree 路径、commit hash、验证命令结果、数据库验证结果、是否有遗留问题。

从 Task 1 开始顺序执行，不要跳过测试和数据库验证。
```
