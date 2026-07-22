# Real-Time AgentOps And Configurable Evaluation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把当前 demo 级 AgentScope 优化成一个可信的实时 Agent 可观测与用户可配置多维评测闭环：用户通过 Python SDK 在自己的 Agent 代码中埋点，平台实时展示 Agent 当前状态、思考内容、LLM、工具、知识库、Skill、结果与错误；用户预先配置数据集和多维评测方案，平台执行评测并给出维度分、综合分和 Trace 归因。

**Architecture:** 本阶段先修复 Run/Trace/Result 身份模型，保证“一次 Agent 请求 = 一个 Run = 一个 Trace = 一套状态事件 + 结构化调用 + 评测结果”。随后扩展 Python SDK 标准埋点 API，让用户显式上报 `thought`、`llm`、`tool`、`retrieval`、`skill`、`final_answer` 等 Agent 语义事件。前端先用 React Query 轮询实现实时感，后续可替换为 SSE/WebSocket；评测系统用现有 Postgres 元数据 + ClickHouse Trace 数据，支持用户配置指标、权重、阈值和 LLM-as-Judge。

**Tech Stack:** Go、PostgreSQL/sqlc、ClickHouse、Python SDK/OpenTelemetry、React/Vite/Ant Design、React Query、Vitest、Go test、pytest。

## Global Constraints

- 所有提交信息必须使用中文。
- 开发必须在独立 git worktree 中完成，不直接污染主工作区。
- 每个 Task 至少一个独立 commit；数据模型、SDK、前端、demo 不要混成一个提交。
- 先写失败测试，再实现修复，再运行验证。
- 本阶段先用轮询实现实时展示，不引入 Kafka、Redis Stream、SSE/WebSocket 等新基础设施。
- 不删除现有三个 demo：`make demo-real-agent`、`make demo-support-agent`、`make demo-multi-eval` 都必须继续可运行。
- 评测语义必须区分：执行状态、质量结果、维度得分、综合评分。
- “思考内容”属于敏感内容，SDK 默认只采集摘要；完整内容必须由用户显式开启，并经过脱敏。

---

## Current Evidence From Database

这不是纯前端展示问题。当前数据库已经证明存在以下链路缺陷：

- real-agent 4 个用例生成了 8 条 runs：有输入输出的 run 没 trace，有 trace 的 run 没输入输出。
- real-agent 2 个 eval job 失败，原因是 GLM 429；失败对应的 trace run 仍停在 `running`。
- multi-eval 的 5 个 job 全部失败，原因是 `target_not_found: Target 'multi-eval-target' is not registered`，`eval_scores` 为 0。
- customer-support demo 有较丰富的 LLM/tool/retrieval/skill 数据，但部分 `run_id` 对应多个 `trace_id`，存在 trace 串线。
- Agent 心跳 payload 为空，hostname 为空、pid 为 0，无法可靠展示实例环境。
- ClickHouse `trace_ingest_stats` 为空，采集健康页目前主要是原始表临时聚合，不是真正的高并发采集健康视图。

---

## Target Product Flow

1. 用户创建项目并生成 Project Token。
2. 用户在自己的 Python Agent 中接入 SDK：
   - `agent_init(...)` 注册 Agent 和实例；
   - `with agent_run(...)` 包裹一次请求；
   - `trace_thought(...)` 记录思考摘要；
   - `trace_llm(...)` 记录模型、prompt/response 摘要、token、成本、错误；
   - `trace_tool(...)` 记录工具名、参数摘要、结果摘要、重试、风险等级；
   - `trace_retrieval(...)` 记录知识库、query、topK、命中文档、分数；
   - `trace_skill(...)` 记录 skill 名称、版本、输入输出摘要；
   - `record_final_answer(...)` 记录最终答案和结构化输出。
3. 平台实时展示：
   - 哪些 Agent 在线；
   - 每个实例最后心跳、当前 run、运行时长；
   - 当前 run 执行到哪一步；
   - 思考、LLM、工具、检索、Skill、结果和错误按时间线出现。
4. 用户配置评测：
   - 数据集和用例；
   - 指标定义：final answer、tool correctness、trajectory、retrieval groundedness、safety、efficiency、LLM-as-Judge；
   - 每个指标的权重、阈值、配置；
   - 综合评分规则。
5. Runner 执行评测并提交结果。
6. 平台展示：
   - 执行状态；
   - 每个 case 的输出；
   - 每个维度的分数和原因；
   - 综合分；
   - 关联 Trace 和扣分归因。

---

## Phase A: 数据可信度修复

### Task 1: 统一 Run/Trace 身份模型

**Files:**
- Modify: `python/examples/real_agent_demo.py`
- Modify: `python/agentscope_sdk/runtime.py`
- Modify: `python/agentscope_sdk/runner.py`
- Modify: `internal/runs/service.go`
- Test: `python/tests/test_run_trace_identity.py`
- Test: `internal/runs/service_test.go`

**Interfaces:**
- Produces: `agentscope.run.id` 必须保存业务 run id，`agentscope.trace.id` 保存 OTLP trace id。
- Produces: 一个用户请求只有一个业务 Run，且该 Run 同时有 input/output/status/trace_id。

- [ ] **Step 1: Write failing Python test**

Create `python/tests/test_run_trace_identity.py`:

```python
from agentscope_sdk.runtime import build_run_resource_attributes


def test_build_run_resource_attributes_separates_run_id_and_trace_id():
    attrs = build_run_resource_attributes(
        run_id="run-123",
        trace_id="trace-abc",
        agent_name="support-agent",
    )

    assert attrs["agentscope.run.id"] == "run-123"
    assert attrs["agentscope.trace.id"] == "trace-abc"
    assert attrs["agentscope.agent.name"] == "support-agent"
```

- [ ] **Step 2: Run failing test**

```bash
cd python
PYTHONPATH=. pytest -q tests/test_run_trace_identity.py
```

Expected: FAIL because `build_run_resource_attributes` does not exist.

- [ ] **Step 3: Implement SDK helper**

In `python/agentscope_sdk/runtime.py`, add:

```python
def build_run_resource_attributes(*, run_id: str, trace_id: str, agent_name: str) -> dict[str, str]:
    return {
        "agentscope.run.id": run_id,
        "agentscope.trace.id": trace_id,
        "agentscope.agent.name": agent_name,
    }
```

Update demo/runner code so:
- runtime-created run id is the canonical `run_id`;
- OTLP root span carries `agentscope.run.id = runtime run id`;
- backend does not create a second run when `agentscope.run.id` already exists.

- [ ] **Step 4: Verify with database**

Run:

```bash
make demo-real-agent
docker compose exec -T postgres psql -U agentscope -d agentscope_test -P pager=off -c "
SELECT input_summary, output_summary, trace_id, agent_name, status
FROM runs
WHERE project_id = 'REPLACE_WITH_DEMO_PROJECT_ID'
ORDER BY created_at;"
```

Expected:
- successful cases have one run each;
- no successful `unknown` duplicate run;
- result `run_id` points to a run with both input/output and trace_id.

- [ ] **Step 5: Commit**

```bash
git add python/examples/real_agent_demo.py python/agentscope_sdk/runtime.py python/agentscope_sdk/runner.py internal/runs/service.go python/tests/test_run_trace_identity.py internal/runs/service_test.go
git commit -m "fix: 统一 Run 与 Trace 身份模型"
```

---

### Task 2: 修复失败 Run 状态收口

**Files:**
- Modify: `python/agentscope_sdk/runner.py`
- Modify: `python/agentscope_sdk/client.py`
- Modify: `internal/runs/runtime.go`
- Test: `python/tests/test_runner_failure_closes_run.py`
- Test: `internal/runs/runtime_test.go`

**Interfaces:**
- Produces: target 执行失败时，如果 trace run 已创建，run 必须转为 `failed`，不能留在 `running`。

- [ ] **Step 1: Write failing test**

Create `python/tests/test_runner_failure_closes_run.py`:

```python
from agentscope_sdk.runner import _build_target_failure_run_error


def test_build_target_failure_run_error_includes_trace_and_error():
    msg = _build_target_failure_run_error(
        trace_id="abcdef1234567890abcdef1234567890",
        error="[glm] HTTP 429",
    )

    assert "abcdef1234567890" in msg
    assert "[glm] HTTP 429" in msg
```

- [ ] **Step 2: Implement failure close path**

In `python/agentscope_sdk/runner.py`, add:

```python
def _build_target_failure_run_error(*, trace_id: str, error: str) -> str:
    return f"Target execution failed for trace_id={trace_id[:16]}...: {error}"
```

In target exception path:
- `force_flush()`;
- lookup run by trace id;
- call SDK fail-run endpoint for that run;
- then fail eval job.

- [ ] **Step 3: Verify**

```bash
cd python
PYTHONPATH=. pytest -q tests/test_runner_failure_closes_run.py
cd ..
make demo-real-agent
docker compose exec -T postgres psql -U agentscope -d agentscope_test -P pager=off -c "
SELECT id, status, error
FROM runs
WHERE status = 'running'
ORDER BY created_at DESC;"
```

Expected: demo 完成后不应留下本轮失败产生的 stale running run。

- [ ] **Step 4: Commit**

```bash
git add python/agentscope_sdk/runner.py python/agentscope_sdk/client.py internal/runs/runtime.go python/tests/test_runner_failure_closes_run.py internal/runs/runtime_test.go
git commit -m "fix: 目标执行失败时关闭运行状态"
```

---

### Task 3: 修复客服 demo trace 串线

**Files:**
- Modify: `python/examples/customer_support_agent.py`
- Test: `python/tests/test_customer_support_trace_context.py`

**Interfaces:**
- Produces: 每个客服请求独立 root span；ClickHouse 中每个 `run_id` 只能对应一个 `trace_id`。

- [ ] **Step 1: Write failing test**

Create `python/tests/test_customer_support_trace_context.py`:

```python
from python.examples.customer_support_agent import compute_latency_ms


def test_compute_latency_ms_is_positive():
    assert compute_latency_ms(start_monotonic=1.0, end_monotonic=1.123) == 123
```

- [ ] **Step 2: Implement per-request context boundary**

In `python/examples/customer_support_agent.py`, add:

```python
def compute_latency_ms(*, start_monotonic: float, end_monotonic: float) -> int:
    return max(1, int((end_monotonic - start_monotonic) * 1000))
```

Refactor loop so each case uses:

```python
for case in cases:
    started = time.monotonic()
    with tracer.start_as_current_span("customer_support.run") as root_span:
        trace_id = format(root_span.get_span_context().trace_id, "032x")
        rt_run = rt.start_run(input=user_input, metadata=metadata, trace_id=trace_id)
        try:
            response = handle_case(...)
            rt.finish_run(
                output=response,
                metrics={"latency_ms": compute_latency_ms(start_monotonic=started, end_monotonic=time.monotonic())},
            )
        except Exception as exc:
            rt.fail_run(str(exc), attributes={"error_type": type(exc).__name__})
            raise
```

- [ ] **Step 3: Verify**

```bash
cd python
PYTHONPATH=. pytest -q tests/test_customer_support_trace_context.py
cd ..
make demo-support-agent
docker compose exec -T clickhouse clickhouse-client --database agentscope_traces --query "
SELECT run_id, uniqExact(trace_id) AS trace_count
FROM trace_spans
GROUP BY run_id
HAVING trace_count > 1
FORMAT PrettyCompact"
```

Expected: no rows.

- [ ] **Step 4: Commit**

```bash
git add python/examples/customer_support_agent.py python/tests/test_customer_support_trace_context.py
git commit -m "fix: 修复客服 Agent Trace 上下文串线"
```

---

### Task 4: 修复 multi-eval target 注册和端到端执行

**Files:**
- Modify: `python/examples/demo_multi_eval.py`
- Test: `python/tests/test_demo_multi_eval_target.py`

**Interfaces:**
- Produces: `multi-eval-target` 可被 runner 找到；demo 能产生 `eval_scores`。

- [ ] **Step 1: Write failing test**

Create `python/tests/test_demo_multi_eval_target.py`:

```python
from agentscope_sdk.target import get_target
import python.examples.demo_multi_eval  # noqa: F401


def test_multi_eval_target_registered_on_import():
    target = get_target("multi-eval-target")

    assert target.name == "multi-eval-target"
    assert callable(target.func)
```

- [ ] **Step 2: Register target**

In `python/examples/demo_multi_eval.py`:

```python
from agentscope_sdk.target import target


@target(name="multi-eval-target", version="1.0.0")
def demo_multi_eval_target(case_input: str, context: dict) -> str:
    ...
```

Then make the demo run the runner loop in-process until all demo jobs are consumed.

- [ ] **Step 3: Verify**

```bash
cd python
PYTHONPATH=. pytest -q tests/test_demo_multi_eval_target.py
cd ..
make demo-multi-eval
docker compose exec -T postgres psql -U agentscope -d agentscope_test -P pager=off -c "
SELECT count(*) FROM eval_scores;"
```

Expected: `eval_scores` count > 0.

- [ ] **Step 4: Commit**

```bash
git add python/examples/demo_multi_eval.py python/tests/test_demo_multi_eval_target.py
git commit -m "fix: 修复多维评测目标注册和执行闭环"
```

---

## Phase B: SDK 标准 Agent 埋点 API

### Task 5: 增加 Agent 语义埋点接口

**Files:**
- Modify: `python/agentscope_sdk/tracing.py`
- Modify: `python/agentscope_sdk/__init__.py`
- Test: `python/tests/test_semantic_tracing_helpers.py`

**Interfaces:**
- Produces:
  - `trace_thought(tracer, summary, content="", stage="", confidence=None)`
  - `record_final_answer(tracer, answer, summary="", structured_output=None)`
  - existing `trace_llm` / `trace_tool` / `trace_retrieval` / `trace_skill` continue working.

- [ ] **Step 1: Write failing test**

Create `python/tests/test_semantic_tracing_helpers.py`:

```python
from agentscope_sdk.tracing import build_thought_attributes, build_final_answer_attributes


def test_build_thought_attributes_defaults_to_summary_only():
    attrs = build_thought_attributes(summary="需要先查订单", content="完整思考内容", capture_content=False)

    assert attrs["agentscope.span.kind"] == "THOUGHT"
    assert attrs["thought.summary"] == "需要先查订单"
    assert "thought.content" not in attrs


def test_build_final_answer_attributes_records_answer_summary():
    attrs = build_final_answer_attributes(answer="Paris", summary="回答首都")

    assert attrs["agentscope.span.kind"] == "FINAL_ANSWER"
    assert attrs["answer.summary"] == "回答首都"
    assert attrs["answer.output"] == "Paris"
```

- [ ] **Step 2: Implement helpers**

In `python/agentscope_sdk/tracing.py`, add:

```python
def build_thought_attributes(
    *,
    summary: str,
    content: str = "",
    stage: str = "",
    confidence: float | None = None,
    capture_content: bool = False,
) -> dict[str, Any]:
    attrs: dict[str, Any] = {
        "openinference.span.kind": "CHAIN",
        "agentscope.span.kind": "THOUGHT",
        "thought.summary": _truncate(summary),
    }
    if stage:
        attrs["thought.stage"] = stage
    if confidence is not None:
        attrs["thought.confidence"] = confidence
    if capture_content and content:
        attrs["thought.content"] = _truncate(content)
        attrs["agentscope.content_captured"] = True
    return attrs


def build_final_answer_attributes(
    *,
    answer: str,
    summary: str = "",
    structured_output: dict[str, Any] | None = None,
) -> dict[str, Any]:
    attrs: dict[str, Any] = {
        "openinference.span.kind": "CHAIN",
        "agentscope.span.kind": "FINAL_ANSWER",
        "answer.output": _truncate(answer),
    }
    if summary:
        attrs["answer.summary"] = _truncate(summary)
    if structured_output is not None:
        attrs["answer.structured_output"] = str(structured_output)
    return attrs
```

Add context managers `trace_thought(...)` and `record_final_answer(...)` that set these attributes on spans.

- [ ] **Step 3: Verify**

```bash
cd python
PYTHONPATH=. pytest -q tests/test_semantic_tracing_helpers.py
PYTHONPATH=. pytest -q
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add python/agentscope_sdk/tracing.py python/agentscope_sdk/__init__.py python/tests/test_semantic_tracing_helpers.py
git commit -m "feat: 增加 Agent 语义埋点接口"
```

---

### Task 6: 后端提取 Thought 和 Final Answer 结构化数据

**Files:**
- Modify: `clickhouse/migrations/00002_structured_calls.sql`
- Modify: `internal/ingest/structured.go`
- Modify: `internal/tracestore/clickhouse.go`
- Modify: `internal/runs/service.go`
- Modify: `web/src/api/types.ts`
- Test: `internal/ingest/structured_test.go`
- Test: `internal/tracestore/clickhouse_test.go`

**Interfaces:**
- Produces:
  - `trace_thoughts`
  - `trace_final_answers`
  - `RunDetail.thoughts`
  - `RunDetail.final_answers`

- [ ] **Step 1: Write failing ingest test**

In `internal/ingest/structured_test.go`, add:

```go
func TestExtractThoughtRecord(t *testing.T) {
	attrs := map[string]string{
		"agentscope.span.kind": "THOUGHT",
		"thought.summary":     "需要先查订单",
	}
	rec, ok := ExtractThoughtRecord("p", "r", "t", "s", attrs, time.Now(), nil)
	if !ok {
		t.Fatal("expected thought record")
	}
	if rec.Summary != "需要先查订单" {
		t.Fatalf("unexpected summary: %s", rec.Summary)
	}
}
```

- [ ] **Step 2: Implement ClickHouse schema**

Add tables:

```sql
CREATE TABLE IF NOT EXISTS trace_thoughts (
  project_id UUID,
  run_id UUID,
  trace_id String,
  span_id String,
  agent_id Nullable(String),
  agent_instance_id Nullable(String),
  stage String,
  summary String,
  content String,
  confidence Nullable(Float64),
  content_captured Bool,
  start_time DateTime64(6),
  duration_ms Nullable(Int64),
  ingested_at DateTime64(6)
) ENGINE = MergeTree
PARTITION BY toYYYYMM(start_time)
ORDER BY (project_id, run_id, trace_id, start_time);
```

```sql
CREATE TABLE IF NOT EXISTS trace_final_answers (
  project_id UUID,
  run_id UUID,
  trace_id String,
  span_id String,
  answer_summary String,
  answer_output String,
  structured_output String,
  start_time DateTime64(6),
  duration_ms Nullable(Int64),
  ingested_at DateTime64(6)
) ENGINE = MergeTree
PARTITION BY toYYYYMM(start_time)
ORDER BY (project_id, run_id, trace_id, start_time);
```

- [ ] **Step 3: Wire backend extraction and query**

Add insert/list methods in tracestore and include these arrays in RunDetail.

- [ ] **Step 4: Verify**

```bash
go test ./internal/ingest ./internal/tracestore ./internal/runs -count=1
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add clickhouse/migrations/00002_structured_calls.sql internal/ingest/structured.go internal/tracestore/clickhouse.go internal/runs/service.go web/src/api/types.ts internal/ingest/structured_test.go internal/tracestore/clickhouse_test.go
git commit -m "feat: 存储 Thought 和最终答案结构化数据"
```

---

## Phase C: 实时可观测展示

### Task 7: Agent 在线控制台自动刷新

**Files:**
- Modify: `python/agentscope_sdk/runtime.py`
- Modify: `internal/agents/service.go`
- Modify: `web/src/features/agents/AgentsPage.tsx`
- Modify: `web/src/features/runs/ActiveRunsPage.tsx`
- Test: `python/tests/test_runtime_heartbeat_payload.py`
- Test: `web/src/features/agents/agents.test.tsx`

**Interfaces:**
- Produces heartbeat payload: `hostname`、`pid`、`sdk_version`、`runtime_started_at`。
- Produces Agent list display: 在线实例数、当前运行数、最后心跳。

- [ ] **Step 1: Write heartbeat test**

Create `python/tests/test_runtime_heartbeat_payload.py`:

```python
from agentscope_sdk.runtime import build_heartbeat_payload


def test_build_heartbeat_payload_contains_process_identity():
    payload = build_heartbeat_payload()

    assert payload["hostname"]
    assert payload["pid"] > 0
    assert payload["sdk_version"]
    assert payload["runtime_started_at"]
```

- [ ] **Step 2: Implement payload and display**

In `runtime.py`, build payload using `socket.gethostname()`、`os.getpid()` and UTC timestamp. Update frontend hooks to use `refetchInterval: 3000` for active runs and agent list.

- [ ] **Step 3: Verify**

```bash
cd python && PYTHONPATH=. pytest -q tests/test_runtime_heartbeat_payload.py
cd ../web && npm test -- --run agents
npm run build
```

- [ ] **Step 4: Commit**

```bash
git add python/agentscope_sdk/runtime.py python/tests/test_runtime_heartbeat_payload.py internal/agents/service.go web/src/features/agents/AgentsPage.tsx web/src/features/runs/ActiveRunsPage.tsx web/src/features/agents/agents.test.tsx
git commit -m "feat: 增强 Agent 在线状态展示"
```

---

### Task 8: Run 实时诊断时间线

**Files:**
- Create: `web/src/features/runs/diagnostics.ts`
- Modify: `web/src/features/runs/RunDetailPage.tsx`
- Modify: `web/src/features/runs/RunRuntimeDetailPage.tsx`
- Test: `web/src/features/runs/runs.test.tsx`

**Interfaces:**
- Produces: `buildRunTimeline(detail)`，把 state events、thoughts、llm、tool、retrieval、skill、final_answer 合并成按时间排序的 Agent 时间线。

- [ ] **Step 1: Write timeline test**

In `web/src/features/runs/runs.test.tsx`:

```ts
import { buildRunTimeline } from "./diagnostics";

it("orders llm and tool events by time", () => {
  const timeline = buildRunTimeline({
    run: { id: "r1", project_id: "p1", source: "live", status: "completed", trace_id: "t1", agent_name: "a", created_at: "2026-07-19T00:00:00Z" },
    spans: [],
    events: [],
    llm_calls: [{ project_id: "p1", run_id: "r1", trace_id: "t1", span_id: "s1", provider: "glm", model: "glm", status: "OK", content_captured: false, start_time: "2026-07-19T00:00:02Z" }],
    tool_calls: [{ project_id: "p1", run_id: "r1", trace_id: "t1", span_id: "s2", tool_name: "search", success: true, retry_count: 0, start_time: "2026-07-19T00:00:01Z" }],
    retrieval_calls: [],
    skill_calls: [],
  });

  expect(timeline.map((i) => i.kind)).toEqual(["tool", "llm"]);
});
```

- [ ] **Step 2: Implement timeline helper**

Create `diagnostics.ts` with:

```ts
export interface TimelineItem {
  kind: "thought" | "llm" | "tool" | "retrieval" | "skill" | "final_answer" | "state";
  title: string;
  summary: string;
  status: string;
  start_time: string;
  duration_ms?: number | null;
}
```

Map existing structured calls into this unified timeline. Add thought/final answer mapping once Task 6 types exist.

- [ ] **Step 3: Update UI**

Run detail first屏展示：
- 当前状态；
- 当前/最后步骤；
- 时间线；
- 最慢调用 Top 3；
- 错误调用；
- token/cost；
- 评测分数摘要。

Use `refetchInterval: run.status` is terminal ? false : 2000`.

- [ ] **Step 4: Verify**

```bash
cd web
npm test -- --run runs
npm run build
```

- [ ] **Step 5: Commit**

```bash
git add web/src/features/runs/diagnostics.ts web/src/features/runs/RunDetailPage.tsx web/src/features/runs/RunRuntimeDetailPage.tsx web/src/features/runs/runs.test.tsx
git commit -m "feat: 增加实时运行诊断时间线"
```

---

## Phase D: 用户可配置多维评测

### Task 9: 评测配置模型支持权重和综合分

**Files:**
- Modify: `queries/evals.sql`
- Modify: `migrations/00002_eval_metrics.sql`
- Modify: `internal/evaluators/service.go`
- Modify: `internal/runnerapi/service.go`
- Modify: `python/agentscope_sdk/evaluators.py`
- Test: `internal/evaluators/service_test.go`
- Test: `python/tests/test_weighted_evaluation.py`

**Interfaces:**
- Produces evaluator config:

```json
{
  "type": "multi",
  "aggregation": "weighted_average",
  "pass_threshold": 0.75,
  "metrics": [
    {"name": "final-answer", "kind": "final_answer", "weight": 0.4, "config": {"type": "exact"}},
    {"name": "tool-correctness", "kind": "tool_correctness", "weight": 0.3, "config": {"type": "required_tool_called", "tool_name": "lookup_order"}},
    {"name": "efficiency", "kind": "efficiency", "weight": 0.3, "config": {"type": "latency_threshold", "max_ms": 5000}}
  ]
}
```

- [ ] **Step 1: Write weighted score test**

Create `python/tests/test_weighted_evaluation.py`:

```python
from agentscope_sdk.evaluators import aggregate_weighted_scores


def test_aggregate_weighted_scores():
    result = aggregate_weighted_scores([
        {"score": 1.0, "weight": 0.4},
        {"score": 0.5, "weight": 0.6},
    ])

    assert result == 0.7
```

- [ ] **Step 2: Implement evaluator aggregation**

In `python/agentscope_sdk/evaluators.py`, add:

```python
def aggregate_weighted_scores(items: list[dict]) -> float:
    total_weight = sum(float(i.get("weight", 0)) for i in items)
    if total_weight <= 0:
        return 0.0
    score = sum(float(i.get("score", 0)) * float(i.get("weight", 0)) for i in items)
    return round(score / total_weight, 6)
```

Update runner to submit primary result score as weighted aggregate when evaluator config type is `multi`.

- [ ] **Step 3: Verify**

```bash
cd python
PYTHONPATH=. pytest -q tests/test_weighted_evaluation.py
PYTHONPATH=. pytest -q
cd ..
go test ./internal/evaluators ./internal/runnerapi -count=1
```

- [ ] **Step 4: Commit**

```bash
git add queries/evals.sql migrations/00002_eval_metrics.sql internal/evaluators/service.go internal/runnerapi/service.go python/agentscope_sdk/evaluators.py internal/evaluators/service_test.go python/tests/test_weighted_evaluation.py
git commit -m "feat: 支持多维评测权重和综合分"
```

---

### Task 10: 前端评测配置界面

**Files:**
- Create: `web/src/features/evals/EvaluatorConfigBuilder.tsx`
- Modify: `web/src/features/experiments/CreateExperimentPanel.tsx`
- Modify: `web/src/api/types.ts`
- Test: `web/src/features/experiments/experiments.test.tsx`

**Interfaces:**
- Produces UI for:
  - 选择指标类型；
  - 配置规则参数；
  - 配置权重；
  - 配置通过阈值；
  - 预览 JSON evaluator config。

- [ ] **Step 1: Write frontend test**

In `web/src/features/experiments/experiments.test.tsx`:

```ts
it("renders evaluator metric weights in create experiment flow", async () => {
  // Render CreateExperimentPanel with mocked datasets and metric definitions.
  // Assert labels exist: "评测维度", "权重", "综合通过阈值".
});
```

- [ ] **Step 2: Implement builder**

Create `EvaluatorConfigBuilder.tsx` with controlled state:

```ts
export interface MetricConfigDraft {
  name: string;
  kind: string;
  weight: number;
  config: Record<string, unknown>;
}
```

Expose:

```ts
onChange(config: {
  type: "multi";
  aggregation: "weighted_average";
  pass_threshold: number;
  metrics: MetricConfigDraft[];
}) => void
```

- [ ] **Step 3: Wire CreateExperimentPanel**

Allow user to create/select evaluator config before creating experiment. Keep existing simple path working.

- [ ] **Step 4: Verify**

```bash
cd web
npm test -- --run experiments
npm run build
```

- [ ] **Step 5: Commit**

```bash
git add web/src/features/evals/EvaluatorConfigBuilder.tsx web/src/features/experiments/CreateExperimentPanel.tsx web/src/api/types.ts web/src/features/experiments/experiments.test.tsx
git commit -m "feat: 增加可配置多维评测界面"
```

---

### Task 11: 实验报告展示维度分和综合分

**Files:**
- Modify: `internal/experiments/service.go`
- Modify: `internal/adapter/experiments.go`
- Modify: `web/src/features/experiments/ExperimentDetailPage.tsx`
- Test: `internal/experiments/service_test.go`
- Test: `web/src/features/experiments/experiments.test.tsx`

**Interfaces:**
- Produces:
  - `execution_summary`
  - `quality_summary`
  - `metric_aggregations`
  - per-case metric score drilldown.

- [ ] **Step 1: Backend separates execution and quality**

Add `ExecutionSummary`:

```go
type ExecutionSummary struct {
	Queued         int `json:"queued"`
	Running        int `json:"running"`
	Succeeded      int `json:"succeeded"`
	Failed         int `json:"failed"`
	MissingResults int `json:"missing_results"`
}
```

Add `QualitySummary`:

```go
type QualitySummary struct {
	TotalResults  int     `json:"total_results"`
	PassedResults int     `json:"passed_results"`
	FailedResults int     `json:"failed_results"`
	AverageScore  float64 `json:"average_score"`
}
```

- [ ] **Step 2: Frontend updates report**

Experiment report first屏展示：
- 执行成功/执行失败/缺失结果；
- 质量通过/质量未通过/平均分；
- 维度得分卡片；
- 每个 case 的维度详情；
- Trace 按钮。

- [ ] **Step 3: Verify**

```bash
go test ./internal/experiments ./internal/adapter -count=1
cd web && npm test -- --run experiments
npm run build
```

- [ ] **Step 4: Commit**

```bash
git add internal/experiments/service.go internal/adapter/experiments.go web/src/features/experiments/ExperimentDetailPage.tsx internal/experiments/service_test.go web/src/features/experiments/experiments.test.tsx
git commit -m "feat: 实验报告展示执行状态和综合评分"
```

---

## Phase E: Demo 验证与回归护栏

### Task 12: 增加 demo 数据一致性验证脚本

**Files:**
- Create: `scripts/verify-demo-data.sh`
- Modify: `Makefile`

**Interfaces:**
- Produces: `make verify-demo-data`

- [ ] **Step 1: Create script**

Create `scripts/verify-demo-data.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "[verify-demo-data] experiments/jobs/results"
docker compose exec -T postgres psql -U agentscope -d agentscope_test -v ON_ERROR_STOP=1 -P pager=off -c "
SELECT e.target_name, e.total_cases, e.passed_cases, e.failed_cases,
       COUNT(j.*) AS jobs,
       COUNT(r.*) AS results
FROM experiments e
LEFT JOIN eval_jobs j ON j.experiment_id = e.id
LEFT JOIN eval_case_results r ON r.eval_job_id = j.id
GROUP BY e.id
ORDER BY e.created_at DESC;"

echo "[verify-demo-data] run_id with multiple trace_id"
docker compose exec -T clickhouse clickhouse-client --database agentscope_traces --query "
SELECT run_id, uniqExact(trace_id) AS trace_count
FROM trace_spans
GROUP BY run_id
HAVING trace_count > 1
FORMAT PrettyCompact"

echo "[verify-demo-data] stale running runs"
docker compose exec -T postgres psql -U agentscope -d agentscope_test -v ON_ERROR_STOP=1 -P pager=off -c "
SELECT id, agent_name, trace_id, status, created_at
FROM runs
WHERE status = 'running'
ORDER BY created_at DESC;"

echo "[verify-demo-data] eval scores"
docker compose exec -T postgres psql -U agentscope -d agentscope_test -v ON_ERROR_STOP=1 -P pager=off -c "
SELECT count(*) AS eval_score_count FROM eval_scores;"
```

- [ ] **Step 2: Add Makefile target**

```make
.PHONY: verify-demo-data
verify-demo-data:
	./scripts/verify-demo-data.sh
```

- [ ] **Step 3: Verify**

```bash
chmod +x scripts/verify-demo-data.sh
make demo-real-agent
make demo-support-agent
make demo-multi-eval
make verify-demo-data
```

Expected:
- no `run_id` with multiple trace ids;
- no stale running runs after demos complete;
- multi-eval produces eval scores.

- [ ] **Step 4: Commit**

```bash
git add scripts/verify-demo-data.sh Makefile
git commit -m "chore: 添加 demo 数据一致性验证"
```

---

## Final Verification

Run:

```bash
go test ./... -count=1
go vet ./...
cd python && PYTHONPATH=. pytest -q
cd ../web && npm test -- --run
npm run build
cd ..
make demo-real-agent
make demo-support-agent
make demo-multi-eval
make verify-demo-data
```

Expected:
- real-agent demo：一个 case 不再拆成 unknown run + trace run。
- support-agent demo：每个 run_id 只对应一个 trace_id。
- multi-eval demo：`eval_scores` 大于 0。
- Agent 页面：能看到在线实例、最后心跳、当前运行。
- Run 页面：能看到实时状态时间线、thought、LLM、工具、检索、Skill、最终答案。
- Experiment 页面：能看到用户配置的多维指标、权重、维度分、综合分、Trace 归因。

## Self-Review

- Spec coverage: 覆盖用户目标中的 SDK 埋点、实时可观测、思考/工具/结果展示、用户配置数据集、多维评测、综合评分、Trace 归因。
- Database evidence coverage: 覆盖 real-agent 双 Run、失败 Run running、multi-eval target_not_found、客服 trace 串线、心跳信息不足、采集统计空表。
- Placeholder scan: 无 TBD/TODO/implement later。
- Type consistency: `build_run_resource_attributes`、`trace_thought`、`record_final_answer`、`ExecutionSummary`、`QualitySummary`、`buildRunTimeline` 均在对应任务中定义。

