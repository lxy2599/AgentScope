# 在线运行状态中心 Phase 2.2 实施计划

> **给 agentic worker 的要求：** 必须使用 `using-git-worktrees` 在独立 worktree 开发；按任务执行时使用 `executing-plans`；每个行为变更使用 `test-driven-development`；遇到失败使用 `systematic-debugging`；完成前使用 `verification-before-completion`；收到审核意见后使用 `receiving-code-review`。

**目标：** 在 Agent 控制中心之上实现在线运行状态中心，让 AgentScope 能实时展示接入 SDK 的 Agent 当前正在运行什么、运行到哪个阶段、是否卡住、失败原因是什么，并能从运行状态跳到 Trace。

**架构：** PostgreSQL 保存 run 当前状态和关键状态事件，Agent 实例通过 `active_run_id` 关联正在执行的 run，ClickHouse 继续保存 trace 明细。Go 后端提供 SDK Runtime API 和 UI 查询 API，Python SDK 在真实 Agent 执行过程中上报 run 生命周期，React 前端新增在线运行页面和 Agent 详情联动。

**技术栈：** Go、chi、pgx、sqlc、PostgreSQL、ClickHouse、Python SDK、httpx、OpenTelemetry、React、TypeScript、TanStack Query、Ant Design。

## 全局约束

- 所有提交信息必须使用中文。
- 开发必须在独立 worktree 中执行，不要直接污染主工作区。
- 本阶段必须先确认 Phase 2.1 Agent 控制中心已经提交并合并；如果尚未合并，先收口 Phase 2.1。
- 不提交真实 API key、token、个人 `.env`。
- 不引入 Kafka、NATS、完整 OpenTelemetry Collector 或复杂告警系统；Phase 2.2 只做在线运行状态中心。
- 不破坏现有项目、数据集、实验、runner、真实 LLM demo、Trace 展示。
- SDK 的状态上报不能阻塞用户业务 Agent；网络错误只能记录或缓存，不能让业务流程崩溃。
- UI 文案使用中文。
- API 响应契约必须和后端真实返回保持一致，不要在前端臆造 `{data: ...}`、`{runs: ...}` 等包装。
- Run 状态枚举统一为：`queued`、`running`、`waiting_llm`、`waiting_tool`、`waiting_retrieval`、`retrying`、`completed`、`failed`、`timeout`、`cancelled`。

---

## 前置收口：合并 Phase 2.1

当前 Phase 2.1 worktree 路径：

```bash
/home/luxinyu/GolandProjects/AgentScope/.claude/worktrees/task15-agent-control-center
```

先进入该 worktree：

```bash
cd /home/luxinyu/GolandProjects/AgentScope/.claude/worktrees/task15-agent-control-center
git status --short
```

如果看到以下未提交文件：

```text
internal/adapter/agent_querier.go
internal/agents/service.go
internal/agents/service_test.go
internal/httpapi/agent_handlers.go
internal/store/agents.sql.go
queries/agents.sql
```

运行验证：

```bash
sqlc generate
go test ./... -count=1
go vet ./...
cd python && python -m pytest -q
cd ../web && npm test -- --run
npm run build
cd ..
git diff --check
```

验证通过后提交：

```bash
git add internal/adapter/agent_querier.go internal/agents/service.go internal/agents/service_test.go internal/httpapi/agent_handlers.go internal/store/agents.sql.go queries/agents.sql
git commit -m "fix: 修复 Agent 控制中心实例和版本一致性"
```

回到主工作区合并：

```bash
cd /home/luxinyu/GolandProjects/AgentScope
git status --short
git merge --no-ff worktree-task15-agent-control-center -m "merge: 合并 Agent 控制中心"
```

合并后再创建 Phase 2.2 worktree。建议路径：

```bash
git worktree add .claude/worktrees/task16-active-runs -b worktree-task16-active-runs
cd .claude/worktrees/task16-active-runs
```

---

## 文件边界

### 数据库与 sqlc

- 修改 `migrations/00001_init.sql`：新增 `run_state_events` 表和必要索引；如现有 `runs.status` 缺少更新时间字段或 trace 关联字段，只做兼容性增量，不做大迁移。
- 修改或新增 `queries/runs.sql`：补充 run 状态更新、active runs 查询、runtime detail 查询。
- 生成 `internal/store/runs.sql.go` 和 `internal/store/models.go`。
- 测试放在 `internal/store/store_test.go`。

### Go 领域层

- 修改 `internal/runs/service.go`：增加在线运行生命周期能力，保持现有实验 run 行为兼容。
- 修改 `internal/runs/service_test.go`：覆盖状态流转、权限、实例 active run 清理。
- 如果现有 `runs.Service` 已经过大，可新增 `internal/runs/runtime.go` 和 `internal/runs/runtime_test.go`，但不要把业务逻辑塞进 HTTP handler。

### Go adapter 与 HTTP

- 修改 `internal/adapter/runs.go` 或新增 `internal/adapter/runtime_runs.go`：把 sqlc 查询适配为领域层接口。
- 修改 `internal/httpapi/handlers.go`：补充 Runtime API 需要的接口和 DTO。
- 新增 `internal/httpapi/runtime_handlers.go`：UI 侧 active runs 和 runtime detail。
- 新增 `internal/httpapi/sdk_runtime_handlers.go`：SDK 侧 run start/state/finish/fail。
- 修改 `internal/httpapi/router.go`：挂载路由。
- 修改 `internal/app/app.go`：完成依赖注入。
- 测试放在 `internal/httpapi/runtime_handlers_test.go`。

### Python SDK

- 修改 `python/agentscope_sdk/runtime.py`：增加 run lifecycle API 和 context manager。
- 修改 `python/agentscope_sdk/tracing.py`：把 `run_id`、`agent_id`、`agent_instance_id`、`trace_id` 尽量写入 span attributes。
- 修改 `python/agentscope_sdk/__init__.py`：导出新增 API。
- 修改 `python/examples/real_agent_demo.py`：真实 demo 使用 run lifecycle。
- 测试放在 `python/tests/test_runtime.py`，必要时补充 `python/tests/test_tracing.py`。

### 前端

- 修改 `web/src/api/types.ts`：新增 active run、runtime detail、run state event 类型。
- 修改 `web/src/api/hooks.ts`：新增 active runs 和 runtime detail hooks。
- 新增 `web/src/features/runs/ActiveRunsPage.tsx`：在线运行列表。
- 新增 `web/src/features/runs/RunRuntimeDetailPage.tsx`：运行状态详情和时间线。
- 新增 `web/src/features/runs/active-runs.test.tsx`：页面测试。
- 修改 `web/src/features/agents/AgentDetailPage.tsx`：展示当前运行和最近运行入口。
- 修改 `web/src/app/App.tsx`：新增路由。
- 修改 `web/src/components/layout/AppShell.tsx`：新增导航或项目内入口。

### 文档

- 修改 `docs/real-agent-demo.md`：说明如何看到在线运行状态。
- 修改 `README.md`：补充在线运行状态中心能力。

---

## Task 1：运行状态事件存储

**Files:**

- 修改：`migrations/00001_init.sql`
- 修改：`queries/runs.sql`
- 生成：`internal/store/runs.sql.go`
- 生成：`internal/store/models.go`
- 测试：`internal/store/store_test.go`

**Interfaces:**

- 产出 `CreateRunStateEvent(ctx, CreateRunStateEventParams) (RunStateEvent, error)`
- 产出 `ListRunStateEventsByRun(ctx, pgtype.UUID) ([]RunStateEvent, error)`
- 产出 `UpdateRunStatus(ctx, UpdateRunStatusParams) (Run, error)`
- 产出 `ListActiveRunsByProject(ctx, ListActiveRunsByProjectParams) ([]ListActiveRunsByProjectRow, error)`
- 产出 `GetRunRuntimeDetail(ctx, GetRunRuntimeDetailParams) (GetRunRuntimeDetailRow, error)`

- [ ] **Step 1：写失败测试**

在 `internal/store/store_test.go` 增加测试，使用现有 `setupMinimal`、`dropAllTables`、`runMigration` 风格：

```go
func TestRunStateEventsAndActiveRuns(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	requireTestDatabase(t, dbURL)

	ctx := context.Background()
	pool, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()
	dropAllTables(t, pool)
	runMigration(t, pool)

	q := New(pool)
	f := setupMinimal(t, pool)

	run, err := q.CreateRun(ctx, CreateRunParams{
		ProjectID: f.projID,
		Status:    "queued",
		Input:     pgtype.Text{String: "hello", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	updated, err := q.UpdateRunStatus(ctx, UpdateRunStatusParams{
		ID:        run.ID,
		ProjectID: f.projID,
		Status:    "running",
		Error:     pgtype.Text{Valid: false},
	})
	if err != nil {
		t.Fatalf("UpdateRunStatus: %v", err)
	}
	if updated.Status != "running" {
		t.Fatalf("updated.Status = %q, want running", updated.Status)
	}

	if _, err := q.CreateRunStateEvent(ctx, CreateRunStateEventParams{
		ProjectID:  f.projID,
		RunID:      run.ID,
		FromStatus: pgtype.Text{String: "queued", Valid: true},
		ToStatus:   "running",
		Message:    pgtype.Text{String: "started", Valid: true},
		Attributes: []byte(`{"source":"test"}`),
	}); err != nil {
		t.Fatalf("CreateRunStateEvent: %v", err)
	}

	events, err := q.ListRunStateEventsByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListRunStateEventsByRun: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].ToStatus != "running" {
		t.Fatalf("ToStatus = %q, want running", events[0].ToStatus)
	}

	active, err := q.ListActiveRunsByProject(ctx, ListActiveRunsByProjectParams{
		ProjectID: f.projID,
		Limit:     50,
		Offset:    0,
	})
	if err != nil {
		t.Fatalf("ListActiveRunsByProject: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("got %d active runs, want 1", len(active))
	}
}
```

- [ ] **Step 2：确认测试失败**

```bash
go test ./internal/store -run TestRunStateEventsAndActiveRuns -count=1
```

预期：失败，原因是表和 sqlc 方法不存在。

- [ ] **Step 3：新增表和索引**

在 `migrations/00001_init.sql` 中追加：

```sql
CREATE TABLE run_state_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    agent_instance_id UUID REFERENCES agent_instances(id) ON DELETE SET NULL,
    from_status TEXT,
    to_status TEXT NOT NULL,
    message TEXT,
    attributes JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_run_state_events_project_created ON run_state_events(project_id, created_at DESC);
CREATE INDEX idx_run_state_events_run_created ON run_state_events(run_id, created_at ASC);
CREATE INDEX idx_run_state_events_instance_created ON run_state_events(agent_instance_id, created_at DESC);
CREATE INDEX idx_run_state_events_status_created ON run_state_events(to_status, created_at DESC);
```

- [ ] **Step 4：新增 sqlc 查询**

在 `queries/runs.sql` 增加：

```sql
-- name: CreateRunStateEvent :one
INSERT INTO run_state_events (
    project_id,
    run_id,
    agent_id,
    agent_instance_id,
    from_status,
    to_status,
    message,
    attributes
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: ListRunStateEventsByRun :many
SELECT *
FROM run_state_events
WHERE run_id = $1
ORDER BY created_at ASC;

-- name: UpdateRunStatus :one
UPDATE runs
SET
    status = $3,
    error = $4,
    updated_at = now()
WHERE id = $1 AND project_id = $2
RETURNING *;

-- name: ListActiveRunsByProject :many
SELECT
    r.id,
    r.project_id,
    r.experiment_id,
    r.status,
    r.input,
    r.output,
    r.error,
    r.trace_id,
    r.latency_ms,
    r.created_at,
    r.updated_at,
    a.id AS agent_id,
    a.name AS agent_name,
    ai.id AS agent_instance_id,
    ai.instance_name AS agent_instance_name,
    rse.created_at AS last_state_event_at,
    rse.to_status AS last_state
FROM runs r
LEFT JOIN agent_instances ai ON ai.active_run_id = r.id
LEFT JOIN agents a ON a.id = ai.agent_id
LEFT JOIN LATERAL (
    SELECT created_at, to_status
    FROM run_state_events
    WHERE run_id = r.id
    ORDER BY created_at DESC
    LIMIT 1
) rse ON true
WHERE r.project_id = $1
  AND r.status IN ('queued', 'running', 'waiting_llm', 'waiting_tool', 'waiting_retrieval', 'retrying')
ORDER BY r.updated_at DESC
LIMIT $2 OFFSET $3;

-- name: GetRunRuntimeDetail :one
SELECT
    r.id,
    r.project_id,
    r.experiment_id,
    r.status,
    r.input,
    r.output,
    r.error,
    r.trace_id,
    r.latency_ms,
    r.created_at,
    r.updated_at,
    a.id AS agent_id,
    a.name AS agent_name,
    ai.id AS agent_instance_id,
    ai.instance_name AS agent_instance_name
FROM runs r
LEFT JOIN agent_instances ai ON ai.active_run_id = r.id
LEFT JOIN agents a ON a.id = ai.agent_id
WHERE r.id = $1 AND r.project_id = $2;
```

- [ ] **Step 5：生成代码并验证**

```bash
sqlc generate
go test ./internal/store -run TestRunStateEventsAndActiveRuns -count=1
```

预期：通过。

- [ ] **Step 6：提交**

```bash
git add migrations/00001_init.sql queries/runs.sql internal/store/runs.sql.go internal/store/models.go internal/store/store_test.go
git commit -m "feat: 添加运行状态事件存储"
```

---

## Task 2：在线运行领域服务

**Files:**

- 修改：`internal/runs/service.go`
- 可选新增：`internal/runs/runtime.go`
- 测试：`internal/runs/service_test.go` 或 `internal/runs/runtime_test.go`

**Interfaces:**

- 产出 `StartRun(ctx, StartRunInput) (RuntimeRun, error)`
- 产出 `UpdateRunState(ctx, UpdateRunStateInput) (RuntimeRun, error)`
- 产出 `FinishRun(ctx, FinishRunInput) (RuntimeRun, error)`
- 产出 `FailRun(ctx, FailRunInput) (RuntimeRun, error)`
- 产出 `ListActiveRuns(ctx, ListActiveRunsInput) ([]ActiveRun, error)`
- 产出 `GetRuntimeRunDetail(ctx, GetRuntimeRunDetailInput) (RuntimeRunDetail, error)`

- [ ] **Step 1：定义领域类型**

在 `internal/runs/service.go` 或 `internal/runs/runtime.go` 中定义：

```go
type RunStatus string

const (
	RunStatusQueued           RunStatus = "queued"
	RunStatusRunning          RunStatus = "running"
	RunStatusWaitingLLM       RunStatus = "waiting_llm"
	RunStatusWaitingTool      RunStatus = "waiting_tool"
	RunStatusWaitingRetrieval RunStatus = "waiting_retrieval"
	RunStatusRetrying         RunStatus = "retrying"
	RunStatusCompleted        RunStatus = "completed"
	RunStatusFailed           RunStatus = "failed"
	RunStatusTimeout          RunStatus = "timeout"
	RunStatusCancelled        RunStatus = "cancelled"
)

func IsValidRunStatus(status string) bool {
	switch RunStatus(status) {
	case RunStatusQueued, RunStatusRunning, RunStatusWaitingLLM, RunStatusWaitingTool, RunStatusWaitingRetrieval, RunStatusRetrying, RunStatusCompleted, RunStatusFailed, RunStatusTimeout, RunStatusCancelled:
		return true
	default:
		return false
	}
}

func IsTerminalRunStatus(status string) bool {
	switch RunStatus(status) {
	case RunStatusCompleted, RunStatusFailed, RunStatusTimeout, RunStatusCancelled:
		return true
	default:
		return false
	}
}
```

- [ ] **Step 2：写失败测试**

覆盖：

- `UpdateRunState` 拒绝非法状态。
- `UpdateRunState` 写状态事件。
- `FinishRun` 把 run 改为 `completed` 并清空实例 `active_run_id`。
- `FailRun` 把 run 改为 `failed`、写 error 并清空实例 `active_run_id`。
- 跨项目 run 或 instance 操作返回权限错误。

测试名建议：

```go
func TestRuntimeServiceRejectsInvalidStatus(t *testing.T) {}
func TestRuntimeServiceWritesStateEvent(t *testing.T) {}
func TestRuntimeServiceFinishClearsActiveRun(t *testing.T) {}
func TestRuntimeServiceFailClearsActiveRun(t *testing.T) {}
func TestRuntimeServiceRejectsCrossProjectInstance(t *testing.T) {}
```

- [ ] **Step 3：实现领域服务**

实现时遵守：

- 所有输入都带 `ProjectID`。
- 更新 run 前先确认 run 属于该 project。
- 如果输入带 `AgentInstanceID`，确认 instance 属于同一 project。
- 状态变化顺序为：读取当前 run -> 校验状态 -> 更新 run -> 写 `run_state_events` -> 更新或清空 instance active run。
- 终态状态必须清空 instance active run。
- 非终态状态可以设置 instance active run。

- [ ] **Step 4：运行领域测试**

```bash
go test ./internal/runs -count=1
```

预期：通过。

- [ ] **Step 5：提交**

```bash
git add internal/runs/service.go internal/runs/runtime.go internal/runs/service_test.go internal/runs/runtime_test.go
git commit -m "feat: 添加在线运行领域服务"
```

如果没有创建某些可选文件，`git add` 时只添加实际存在的文件。

---

## Task 3：SDK Runtime HTTP API

**Files:**

- 修改：`internal/httpapi/handlers.go`
- 新增：`internal/httpapi/sdk_runtime_handlers.go`
- 修改：`internal/httpapi/router.go`
- 修改：`internal/app/app.go`
- 修改或新增：`internal/adapter/runtime_runs.go`
- 测试：`internal/httpapi/runtime_handlers_test.go`

**Interfaces:**

- `POST /api/sdk/runs/start`
- `POST /api/sdk/runs/{run_id}/state`
- `POST /api/sdk/runs/{run_id}/finish`
- `POST /api/sdk/runs/{run_id}/fail`

- [ ] **Step 1：写 HTTP 失败测试**

覆盖：

- project token 可 start run。
- state 更新后响应新状态。
- finish 后状态为 `completed`。
- fail 后状态为 `failed` 且带 error。
- 无 token 返回 401。
- token project 与 run project 不一致返回 403 或 404，按项目现有习惯选择一种并保持一致。

- [ ] **Step 2：定义请求和响应 DTO**

建议 DTO：

```go
type SDKStartRunRequest struct {
	AgentID         string         `json:"agent_id"`
	AgentInstanceID string        `json:"agent_instance_id"`
	Input           string        `json:"input"`
	Metadata        map[string]any `json:"metadata"`
	TraceID         string        `json:"trace_id"`
}

type SDKUpdateRunStateRequest struct {
	Status     string         `json:"status"`
	Message    string         `json:"message"`
	Attributes map[string]any `json:"attributes"`
	TraceID     string        `json:"trace_id"`
}

type SDKFinishRunRequest struct {
	Output     string         `json:"output"`
	Metrics    map[string]any `json:"metrics"`
	TraceID    string        `json:"trace_id"`
	LatencyMS  int64         `json:"latency_ms"`
}

type SDKFailRunRequest struct {
	Error      string         `json:"error"`
	Attributes map[string]any `json:"attributes"`
	TraceID     string        `json:"trace_id"`
	LatencyMS   int64         `json:"latency_ms"`
}
```

响应直接返回领域对象，不额外包装。

- [ ] **Step 3：实现 handler**

要求：

- SDK API 使用现有 project token 鉴权中间件。
- project id 必须来自 token，不从请求体信任。
- `AgentID` 和 `AgentInstanceID` 必须属于 token project。
- JSON 解析失败返回现有错误格式。

- [ ] **Step 4：挂路由并注入依赖**

在 `router.go` 中挂载 SDK runtime 路由。保持已有 `/api/sdk/agents/...` 风格一致。

- [ ] **Step 5：运行测试**

```bash
go test ./internal/httpapi -run 'TestSDK.*Run|TestRuntime' -count=1
go test ./internal/app -count=1
```

预期：通过。

- [ ] **Step 6：提交**

```bash
git add internal/httpapi/handlers.go internal/httpapi/sdk_runtime_handlers.go internal/httpapi/router.go internal/httpapi/runtime_handlers_test.go internal/app/app.go internal/adapter/runtime_runs.go
git commit -m "feat: 添加 SDK 在线运行 API"
```

---

## Task 4：UI 在线运行查询 API

**Files:**

- 修改：`internal/httpapi/handlers.go`
- 新增或修改：`internal/httpapi/runtime_handlers.go`
- 修改：`internal/httpapi/router.go`
- 修改：`internal/app/app.go`
- 修改或新增：`internal/adapter/runtime_runs.go`
- 测试：`internal/httpapi/runtime_handlers_test.go`

**Interfaces:**

- `GET /api/projects/{project_id}/active-runs`
- `GET /api/projects/{project_id}/runs/{run_id}/runtime`

- [ ] **Step 1：写失败测试**

覆盖：

- 项目成员可以查询 active runs。
- 非项目成员不能查询 active runs。
- runtime detail 返回 run 基本信息、agent 信息、instance 信息和状态事件。
- 查询不存在 run 返回 404。

- [ ] **Step 2：实现 UI handler**

要求：

- 使用 session 用户鉴权。
- 使用现有项目成员校验逻辑。
- active runs 支持 `agent_id`、`status`、`limit`、`offset` 查询参数。
- status 过滤只允许非终态 active 状态。

- [ ] **Step 3：运行测试**

```bash
go test ./internal/httpapi -run 'Test.*ActiveRuns|Test.*RuntimeDetail' -count=1
```

预期：通过。

- [ ] **Step 4：提交**

```bash
git add internal/httpapi/handlers.go internal/httpapi/runtime_handlers.go internal/httpapi/router.go internal/httpapi/runtime_handlers_test.go internal/app/app.go internal/adapter/runtime_runs.go
git commit -m "feat: 添加在线运行查询 API"
```

---

## Task 5：Python SDK 运行生命周期

**Files:**

- 修改：`python/agentscope_sdk/runtime.py`
- 修改：`python/agentscope_sdk/tracing.py`
- 修改：`python/agentscope_sdk/__init__.py`
- 测试：`python/tests/test_runtime.py`
- 测试：`python/tests/test_tracing.py`

**Interfaces:**

- `agentscope.start_run(input=None, metadata=None)`
- `agentscope.update_run_status(status, message=None, attributes=None)`
- `agentscope.finish_run(output=None, metrics=None)`
- `agentscope.fail_run(error, attributes=None)`
- `agentscope.run(input=None, metadata=None)` context manager

- [ ] **Step 1：写失败测试**

在 `python/tests/test_runtime.py` 覆盖：

```python
def test_start_run_posts_sdk_runtime_endpoint():
    runtime = Runtime(project_token="tok", server_url="http://server", agent_id="agent-1", agent_instance_id="inst-1")
    transport = RecordingTransport(json_response={"id": "run-1", "status": "running"})
    runtime._client = httpx.Client(transport=transport, base_url="http://server")

    run = runtime.start_run(input="hello", metadata={"case_id": "case-1"})

    assert run.id == "run-1"
    assert transport.requests[0].method == "POST"
    assert transport.requests[0].url.path == "/api/sdk/runs/start"

def test_update_run_status_posts_state_endpoint():
    runtime = Runtime(project_token="tok", server_url="http://server", agent_id="agent-1", agent_instance_id="inst-1")
    runtime._current_run = RuntimeRun(id="run-1", status="running")
    transport = RecordingTransport(json_response={"id": "run-1", "status": "waiting_llm"})
    runtime._client = httpx.Client(transport=transport, base_url="http://server")

    runtime.update_run_status("waiting_llm", message="calling model", attributes={"model": "glm"})

    assert transport.requests[0].method == "POST"
    assert transport.requests[0].url.path == "/api/sdk/runs/run-1/state"

def test_run_context_finishes_on_success():
    runtime = Runtime(project_token="tok", server_url="http://server", agent_id="agent-1", agent_instance_id="inst-1")
    transport = RecordingTransport(json_responses=[
        {"id": "run-1", "status": "running"},
        {"id": "run-1", "status": "completed"},
    ])
    runtime._client = httpx.Client(transport=transport, base_url="http://server")

    with runtime.run(input="hello") as run:
        run.set_output("world")

    assert [request.url.path for request in transport.requests] == [
        "/api/sdk/runs/start",
        "/api/sdk/runs/run-1/finish",
    ]

def test_run_context_fails_and_reraises_on_exception():
    runtime = Runtime(project_token="tok", server_url="http://server", agent_id="agent-1", agent_instance_id="inst-1")
    transport = RecordingTransport(json_responses=[
        {"id": "run-1", "status": "running"},
        {"id": "run-1", "status": "failed"},
    ])
    runtime._client = httpx.Client(transport=transport, base_url="http://server")

    with pytest.raises(ValueError):
        with runtime.run(input="hello"):
            raise ValueError("boom")

    assert transport.requests[-1].url.path == "/api/sdk/runs/run-1/fail"

def test_runtime_network_error_does_not_break_business_code():
    runtime = Runtime(project_token="tok", server_url="http://server", agent_id="agent-1", agent_instance_id="inst-1")
    runtime._client = httpx.Client(transport=FailingTransport(), base_url="http://server")

    run = runtime.start_run(input="hello")

    assert run is None
    assert runtime.last_error is not None
```

- [ ] **Step 2：实现 SDK API**

实现规则：

- `start_run` 调用 `POST /api/sdk/runs/start`，保存当前 run context。
- `update_run_status` 调用 `POST /api/sdk/runs/{run_id}/state`。
- `finish_run` 调用 `POST /api/sdk/runs/{run_id}/finish` 并清理当前 run context。
- `fail_run` 调用 `POST /api/sdk/runs/{run_id}/fail` 并清理当前 run context。
- context manager 正常退出调用 finish。
- context manager 异常退出调用 fail，并重抛原异常。
- 网络异常吞掉但记录到 SDK runtime 的 last error，便于测试。

- [ ] **Step 3：Trace attributes 联动**

在 tracing helper 中确保当前 span 尽量带上：

```text
agentscope.project_id
agentscope.agent_id
agentscope.agent_instance_id
agentscope.run_id
agentscope.trace_id
```

如果当前 runtime 或 run 不存在，不要报错。

- [ ] **Step 4：运行 Python 测试**

```bash
cd python
python -m pytest -q
```

预期：通过。

- [ ] **Step 5：提交**

```bash
git add python/agentscope_sdk/runtime.py python/agentscope_sdk/tracing.py python/agentscope_sdk/__init__.py python/tests/test_runtime.py python/tests/test_tracing.py
git commit -m "feat: 扩展 Python SDK 运行生命周期"
```

---

## Task 6：真实 Agent Demo 接入在线运行

**Files:**

- 修改：`python/examples/real_agent_demo.py`
- 修改：`scripts/demo-real-agent.sh`
- 修改：`Makefile`
- 测试：`python/tests/test_demo_helpers.py`

**Interfaces:**

- `make demo-real-agent` 运行期间会创建 Agent、实例、run、状态事件和 trace。

- [ ] **Step 1：写失败测试**

在 `python/tests/test_demo_helpers.py` 中增加文件级测试，确认 demo 使用：

```text
agentscope.run(
agentscope.update_run_status("waiting_llm"
```

如果 demo 中有工具或检索模拟，也要确认使用 `waiting_tool` 或 `waiting_retrieval`。

- [ ] **Step 2：修改 demo**

要求：

- 每个 case 执行时包在 `with agentscope.run(input=case_input)` 中。
- 调用 LLM 前上报 `waiting_llm`。
- 成功后 `finish_run(output=actual_output, metrics={...})`。
- 失败时由 context manager 调用 `fail_run`。
- 实验结果中的 `run_id` 和 trace 要能对应。

- [ ] **Step 3：运行 demo 相关测试**

```bash
cd python
python -m pytest -q tests/test_demo_helpers.py tests/test_runtime.py
```

预期：通过。

- [ ] **Step 4：提交**

```bash
git add python/examples/real_agent_demo.py scripts/demo-real-agent.sh Makefile python/tests/test_demo_helpers.py
git commit -m "feat: 让真实 Agent Demo 上报在线运行状态"
```

---

## Task 7：前端在线运行页面

**Files:**

- 修改：`web/src/api/types.ts`
- 修改：`web/src/api/hooks.ts`
- 新增：`web/src/features/runs/ActiveRunsPage.tsx`
- 新增：`web/src/features/runs/RunRuntimeDetailPage.tsx`
- 新增：`web/src/features/runs/active-runs.test.tsx`
- 修改：`web/src/app/App.tsx`
- 修改：`web/src/components/layout/AppShell.tsx`
- 修改：`web/src/features/agents/AgentDetailPage.tsx`

**Interfaces:**

- 产出 `/projects/:projectId/active-runs`
- 产出 `/projects/:projectId/runs/:runId/runtime`
- Agent 详情页展示当前运行入口。

- [ ] **Step 1：写失败测试**

在 `web/src/features/runs/active-runs.test.tsx` 覆盖：

- active runs 表格渲染中文状态。
- 可以按状态筛选。
- 点击 trace 链接跳转到已有 trace 页面。
- runtime detail 展示状态事件时间线。

- [ ] **Step 2：补充 API 类型和 hooks**

类型建议：

```ts
export type RunRuntimeStatus =
  | 'queued'
  | 'running'
  | 'waiting_llm'
  | 'waiting_tool'
  | 'waiting_retrieval'
  | 'retrying'
  | 'completed'
  | 'failed'
  | 'timeout'
  | 'cancelled';

export interface ActiveRun {
  id: string;
  project_id: string;
  experiment_id?: string | null;
  agent_id?: string | null;
  agent_name?: string | null;
  agent_instance_id?: string | null;
  agent_instance_name?: string | null;
  status: RunRuntimeStatus;
  input?: string | null;
  output?: string | null;
  error?: string | null;
  trace_id?: string | null;
  latency_ms?: number | null;
  created_at: string;
  updated_at: string;
  last_state_event_at?: string | null;
  last_state?: string | null;
}

export interface RunStateEvent {
  id: string;
  run_id: string;
  from_status?: string | null;
  to_status: RunRuntimeStatus;
  message?: string | null;
  attributes: Record<string, unknown>;
  created_at: string;
}

export interface RunRuntimeDetail extends ActiveRun {
  events: RunStateEvent[];
}
```

- [ ] **Step 3：实现页面**

页面要求：

- 文案全部中文。
- 状态标签颜色区分：运行中、等待 LLM、等待工具、等待检索、重试、失败、完成。
- 列表展示 Agent、实例、状态、已运行时长、输入摘要、更新时间、Trace。
- 筛选项使用 Ant Design `Select`。
- 刷新使用按钮或 TanStack Query refetch。
- detail 页面使用 `Timeline` 展示状态事件。

- [ ] **Step 4：Agent 详情页联动**

在 `AgentDetailPage.tsx` 展示：

- 当前运行。
- 最近运行入口。
- “查看在线运行”按钮跳转到 active runs 页面并带 agent 过滤。

- [ ] **Step 5：运行前端测试和构建**

```bash
cd web
npm test -- --run
npm run build
```

预期：测试通过，构建通过。Vite chunk size warning 可以接受。

- [ ] **Step 6：提交**

```bash
git add web/src/api/types.ts web/src/api/hooks.ts web/src/features/runs/ActiveRunsPage.tsx web/src/features/runs/RunRuntimeDetailPage.tsx web/src/features/runs/active-runs.test.tsx web/src/app/App.tsx web/src/components/layout/AppShell.tsx web/src/features/agents/AgentDetailPage.tsx
git commit -m "feat: 添加在线运行前端页面"
```

---

## Task 8：文档和最终验证

**Files:**

- 修改：`README.md`
- 修改：`docs/real-agent-demo.md`

- [ ] **Step 1：更新文档**

`docs/real-agent-demo.md` 需要说明：

- 如何配置 DeepSeek / GLM key。
- 如何启动依赖、后端和前端。
- 如何运行 `make demo-real-agent`。
- 在前端哪里查看 Agent 在线状态。
- 在前端哪里查看 Active Runs。
- 如何从 Active Runs 跳到 Trace。

`README.md` 需要补充：

- Agent 控制中心。
- 在线运行状态中心。
- Python SDK run lifecycle。

- [ ] **Step 2：完整验证**

运行：

```bash
sqlc generate
go test ./... -count=1
go vet ./...
cd python && python -m pytest -q
cd ../web && npm test -- --run
npm run build
cd ..
git diff --check
```

如果本地 PostgreSQL、ClickHouse、LLM key 已可用，再运行：

```bash
make demo-real-agent
```

预期：

- Agent 控制中心能看到 Agent 在线。
- demo 执行中 Active Runs 能看到正在运行的 run。
- LLM 调用阶段显示 `waiting_llm` 或中文“等待 LLM”。
- 执行完成后 run 进入 `completed`。
- 实验报告仍正常。
- Trace 链接可用。

- [ ] **Step 3：提交文档**

```bash
git add README.md docs/real-agent-demo.md
git commit -m "docs: 更新在线运行演示说明"
```

- [ ] **Step 4：输出交付报告**

交付报告必须包含：

- worktree 路径
- 分支名
- commit 列表
- 修改文件列表
- 验证命令和结果
- demo 查看方式
- 是否有未提交改动

---

## 自检清单

- Phase 2.1 是否已经提交并合并到主分支。
- `run_state_events` 是否有 project 维度，避免跨项目查询。
- SDK API 是否只信任 token project，不信任请求体 project id。
- terminal 状态是否会清空 `agent_instances.active_run_id`。
- Active Runs 是否只展示非终态 run。
- 前端是否没有臆造 API 包装。
- demo 是否仍能生成实验报告和 Trace。
- 所有页面文案是否为中文。
- 最终是否运行完整验证。
