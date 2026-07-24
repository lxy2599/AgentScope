# Phase 4 实时可观测、可信评测与生产治理实施计划

> **执行要求：** 必须使用 `executing-plans`、`test-driven-development` 和
> `verification-before-completion`。全部代码在一个独立 Worktree 中完成，每个任务
> 单独提交，提交信息使用中文。

**目标：** 完成 Phase 4.1–4.5：OpenHands 真实接入、Redis Streams + SSE 实时
事件平面、不可变 Run Snapshot、多 Judge 与人工校准、分布式配额、SDK 背压、
成本性能分析、异常检测和可重复高并发验收。

**架构：** PostgreSQL 保存业务事实、快照、Outbox、评测与配额；ClickHouse 保存
Trace 明细和分析聚合；Redis Streams 负责实时扇出和短窗口分布式限流。所有实时事件
在权威写入成功后发布，Redis 故障时降级而不破坏业务写入。

**技术栈：** Go 1.26、chi、pgx/sqlc、go-redis v9、PostgreSQL 16、ClickHouse、
Python 3.11、OpenTelemetry、OpenHands SDK、React 18、TanStack Query、Ant Design、
Vitest、Playwright。

**设计依据：**

- `docs/superpowers/specs/2026-07-24-phase4-realtime-evaluation-governance-design.md`
- `docs/可观测.md`
- `docs/superpowers/plans/2026-07-22-phase3-observability-platform-upgrade.md`

---

## Task 0：建立隔离 Worktree 和基线

**文件：**

- 不修改业务文件

**步骤 1：检查仓库和 Worktree 约定**

```bash
git branch --show-current
git status --short
git worktree list
git check-ignore -q .claude/worktrees
```

预期：主分支为 `master`；仅存在用户已有的 `docker-compose.yml` 修改；
`.claude/worktrees` 已忽略。

**步骤 2：创建 Worktree**

```bash
git worktree add .claude/worktrees/phase4-agentops -b worktree-phase4-agentops
```

不要复制主工作区未提交文件。Phase 4 的 Redis 使用独立 Compose overlay，避免覆盖
用户对 `docker-compose.yml` 的本地修改。

**步骤 3：运行基线**

```bash
go test ./...
cd python && python -m pytest
cd web && npm test
cd web && npm run lint
cd web && npm run build
```

记录既有失败。不得把既有失败误判为 Phase 4 引入的回归。

---

## Task 1：统一实时事件、PostgreSQL Outbox 与 Redis Relay

**文件：**

- 新建：`migrations/00008_phase4_realtime.sql`
- 新建：`queries/realtime.sql`
- 生成：`internal/store/realtime.sql.go`
- 修改：`internal/store/models.go`
- 新建：`internal/realtime/event.go`
- 新建：`internal/realtime/outbox.go`
- 新建：`internal/realtime/redis_stream.go`
- 新建：`internal/realtime/relay.go`
- 新建：`internal/realtime/event_test.go`
- 新建：`internal/realtime/redis_stream_test.go`
- 新建：`internal/realtime/relay_test.go`
- 修改：`internal/config/config.go`
- 修改：`internal/config/config_test.go`
- 修改：`go.mod`
- 修改：`go.sum`
- 新建：`docker-compose.phase4.yml`
- 修改：`Makefile`
- 修改：`scripts/dev.sh`

**步骤 1：写失败测试**

覆盖：

- `realtime-event-v1` 必填字段和低敏 Payload 上限；
- Outbox ID 映射为 Redis Stream ID；
- Lua 发布脚本在重复 Relay 时不重复添加事件；
- Relay 失败后保留未发布记录并递增 attempts；
- 多 Relay 使用 `SKIP LOCKED` 不重复领取；
- Redis 未配置时返回 disabled publisher，而不是阻止服务启动；
- 配置解析 Redis 地址、密码、DB、Stream 保留量、Relay 批量大小。

运行：

```bash
go test ./internal/realtime ./internal/config
```

预期：因包、类型和配置不存在而失败。

**步骤 2：增加 PostgreSQL Outbox**

`realtime_outbox` 至少包含：

- `id BIGSERIAL PRIMARY KEY`
- `schema_version`
- `project_id`
- `run_id`
- `agent_id`
- `agent_instance_id`
- `event_type`
- `payload JSONB`
- `occurred_at`
- `published_at`
- `attempts`
- `last_error`

增加领取、标记成功、标记失败、按游标查询和清理查询。对 `run_state_events`、
`agent_heartbeats` 和 Agent 注册建立事务内触发器，生成低敏实时事件。

**步骤 3：实现 Redis Stream**

使用 `github.com/redis/go-redis/v9`。Lua 脚本原子执行：

1. 检查项目级去重 Hash 是否存在 Outbox ID；
2. 使用显式 `{outboxID}-0` 执行 `XADD`；
3. 写入去重 Hash；
4. 近似裁剪 Stream；
5. 设置去重 Hash TTL。

Stream Key：`agentscope:events:{projectID}`。

**步骤 4：实现 Relay 生命周期**

- 后台循环受应用 Context 控制；
- 指数退避有上限；
- 每次批量领取；
- Redis 恢复后继续处理积压；
- Shutdown 等待当前批次或超时；
- 日志只包含 Outbox ID、项目 ID 和错误码。

**步骤 5：增加 Compose overlay**

`docker-compose.phase4.yml` 只增加内部 Redis 服务和健康检查，不向宿主机暴露默认
6379。`make db-up`、`make db-down`、`scripts/dev.sh` 同时加载基础 Compose 和
Phase 4 overlay。

**步骤 6：生成并验证**

```bash
sqlc generate
go test ./internal/realtime ./internal/config ./internal/store
docker compose -f docker-compose.yml -f docker-compose.phase4.yml config
git diff --check
```

**步骤 7：提交**

```bash
git add migrations/00008_phase4_realtime.sql queries/realtime.sql \
  internal/store internal/realtime internal/config go.mod go.sum \
  docker-compose.phase4.yml Makefile scripts/dev.sh
git commit -m "feat: 建立实时事件 Outbox 与 Redis Streams"
```

---

## Task 2：SSE API、权限隔离与前端实时订阅

**文件：**

- 新建：`internal/realtime/subscriber.go`
- 新建：`internal/realtime/subscriber_test.go`
- 新建：`internal/httpapi/realtime_handlers.go`
- 新建：`internal/httpapi/realtime_handlers_test.go`
- 修改：`internal/httpapi/handlers.go`
- 修改：`internal/httpapi/router.go`
- 修改：`internal/app/app.go`
- 新建：`web/src/api/realtime.ts`
- 新建：`web/src/api/__tests__/realtime.test.ts`
- 修改：`web/src/api/hooks.ts`
- 修改：`web/src/api/types.ts`
- 修改：`web/src/features/agents/AgentsPage.tsx`
- 修改：`web/src/features/agents/AgentDetailPage.tsx`
- 修改：`web/src/features/runs/ActiveRunsPage.tsx`
- 修改：`web/src/features/runs/RunDetailPage.tsx`
- 修改：`web/src/features/runs/RunRuntimeDetailPage.tsx`
- 修改：相关前端测试

**步骤 1：写 SSE 失败测试**

覆盖：

- 未登录返回 401；
- 非项目成员返回 403；
- `Last-Event-ID` 从下一条开始；
- project/run/agent/types 过滤；
- SSE `id/event/data` 格式；
- 心跳；
- Cursor 早于 Stream 最小 ID 时发送 `reset`；
- Redis 不可用返回 503 和稳定错误码；
- 慢消费者取消 Context 并释放连接计数；
- 绝不跨项目推送。

运行：

```bash
go test ./internal/realtime ./internal/httpapi
```

预期：路由和订阅器不存在。

**步骤 2：实现 SSE Endpoint**

新增：

```text
GET /api/projects/{projectID}/events/stream
```

响应头：

- `Content-Type: text/event-stream`
- `Cache-Control: no-cache`
- `Connection: keep-alive`
- `X-Accel-Buffering: no`

使用 Session Cookie 和 `ProjectAuth`。Event ID 使用 Outbox ID。对每条事件再次检查
`project_id`。

**步骤 3：实现前端 EventSource 管理器**

`createProjectEventStream` 负责：

- 单项目单连接复用；
- 指数退避；
- Event ID 去重；
- `reset` 时失效相关 React Query；
- 503 后进入轮询降级；
- 页面卸载关闭连接。

由于原生 EventSource 自动携带同源 Cookie，不在 URL 中放 Token。

**步骤 4：接入页面**

- Agent 列表和详情：心跳、在线状态、当前 Run；
- Active Runs：状态增量更新；
- Run Detail：语义事件到达时刷新 Timeline；
- 页面明确显示“实时 / 重连中 / 轮询降级”。

保留现有轮询作为首次加载和降级路径。

**步骤 5：验证并提交**

```bash
go test ./internal/realtime ./internal/httpapi ./internal/app
cd web && npm test -- --run
cd web && npm run lint
cd web && npm run build
git diff --check
git status --short
git add -A
git commit -m "feat: 提供 SSE 实时运行视图"
```

---

## Task 3：不可变 Run Snapshot

**文件：**

- 新建：`migrations/00009_phase4_run_snapshots.sql`
- 新建：`queries/snapshots.sql`
- 生成：`internal/store/snapshots.sql.go`
- 新建：`internal/snapshots/service.go`
- 新建：`internal/snapshots/service_test.go`
- 新建：`internal/adapter/snapshot.go`
- 修改：`internal/runs/runtime.go`
- 修改：`internal/adapter/runtime_runs.go`
- 修改：`internal/httpapi/sdk_runtime_handlers.go`
- 修改：`internal/httpapi/run_handlers.go`
- 修改：`internal/httpapi/router.go`
- 修改：`internal/app/app.go`
- 修改：`python/agentscope_sdk/runtime.py`
- 修改：`python/agentscope_sdk/client.py`
- 新建：`python/agentscope_sdk/snapshots.py`
- 新建：`python/tests/test_snapshots.py`
- 修改：`python/tests/test_runtime.py`
- 修改：`web/src/api/types.ts`
- 修改：`web/src/api/hooks.ts`
- 新建：`web/src/features/runs/RunSnapshotPanel.tsx`
- 修改：`web/src/features/runs/RunDetailPage.tsx`
- 修改：`web/src/features/experiments/ExperimentComparePage.tsx`
- 修改：相关测试

**步骤 1：写失败测试**

覆盖：

- Canonical JSON 键顺序不影响 SHA-256；
- Secret、Token、Cookie、Authorization、API Key 被脱敏；
- 同 Run 同 Hash 幂等；
- 同 Run 不同 Hash 返回 409；
- 数据库拒绝 UPDATE/DELETE；
- Snapshot 与 Run 项目边界一致；
- 旧 Run 返回 `snapshot_unavailable`；
- SDK start_run 自动生成 Runtime Contract v2 Snapshot；
- 前端正确显示版本、哈希和采集策略。

**步骤 2：实现数据模型和不可变约束**

`run_snapshots` 使用 `run_id UNIQUE`。增加数据库触发器拒绝更新和删除。Payload 大小
设上限，Hash 在服务端对脱敏后的 Canonical JSON 重新计算，不能信任客户端 Hash。

**步骤 3：扩展 Runtime Start**

`POST /api/sdk/runs/start` 接收可选 `snapshot`。Runtime Adapter 使用一个 PostgreSQL
事务完成：

1. 创建 Run；
2. 创建初始 State Event；
3. 创建 Snapshot；
4. 绑定 Agent Instance。

任何步骤失败都回滚，防止出现无 Snapshot 的新 SDK Run。

**步骤 4：SDK 构建器**

提供：

- `RunSnapshotBuilder`
- 模型参数；
- Prompt/Tool/Skill/MCP/RAG 版本和哈希；
- Framework/SDK/上游 Commit；
- Feature/Ablation Flags；
- 容器和依赖摘要；
- Case/Input 哈希；
- Capture Policy。

不读取整个环境变量；只接受调用方显式值。

**步骤 5：前端快照与对比**

Run Detail 增加“运行配置”标签。Experiment Compare 根据快照递归生成受控 Diff，
敏感字段只显示脱敏值和 Hash。

**步骤 6：验证并提交**

```bash
sqlc generate
go test ./internal/snapshots ./internal/runs ./internal/httpapi ./internal/adapter
cd python && python -m pytest tests/test_snapshots.py tests/test_runtime.py
cd web && npm test -- --run
cd web && npm run lint
git diff --check
git status --short
git add -A
git commit -m "feat: 保存不可变运行配置快照"
```

---

## Task 4：OpenHands 无侵入 Adapter 与真实源码接入

**文件：**

- 新建：`python/agentscope_sdk/adapters/openhands.py`
- 修改：`python/agentscope_sdk/adapters/__init__.py`
- 新建：`python/tests/test_openhands_adapter.py`
- 新建：`python/tests/test_openhands_demo_structure.py`
- 新建：`python/examples/openhands_agentops_demo.py`
- 新建：`scripts/setup-openhands-agent.sh`
- 新建：`scripts/demo-openhands-agentops.sh`
- 新建：`scripts/verify-openhands-data.sh`
- 新建：`docs/integrations/openhands.md`
- 修改：`python/pyproject.toml`
- 修改：`Makefile`
- 修改：`.gitignore`

**步骤 1：固定官方版本**

通过官方仓库获取当前可用 Commit，并在 Adapter 常量、安装脚本和文档中使用同一
Commit。源码 clone 到被忽略的 `.external/openhands`，不得把上游仓库提交进本项目。

```bash
git ls-remote https://github.com/OpenHands/software-agent-sdk.git HEAD
```

**步骤 2：写事件映射失败测试**

使用最小 Fake Event 验证：

- Message user/agent；
- Action 与并行 tool call ID；
- Observation 成功/失败；
- AgentError 与 ConversationError；
- State Update/Pause；
- System Prompt Snapshot；
- Token/TTFT；
- Condensation；
- 未知事件降级；
- 回调异常不影响 OpenHands。

**步骤 3：实现 Adapter**

只使用公开 Callback 和 Token Callback。通过事件 `kind` 与公开字段做兼容提取；
版本不支持的字段保留默认值并记录兼容性属性。不得修改 OpenHands 类或源码。

**步骤 4：真实 Demo**

真实任务要求 OpenHands：

1. 检查工作区；
2. 创建一个小文件；
3. 运行命令验证文件；
4. 故意执行一次可恢复失败；
5. 修正并给出最终回答。

Demo 注册 Agent、创建 Run/Snapshot、配置 Tracing，并在执行中打印 SSE 事件到达
时间。支持 GLM/DeepSeek 的 OpenAI-compatible 配置。

**步骤 5：数据库验证**

脚本必须验证：

- PostgreSQL Run/State/Snapshot；
- ClickHouse Thought/LLM/Tool/Final Answer；
- Terminal 和 File 工具均出现；
- Redis Stream 有相同 Run ID；
- SSE 至少在 Run 结束前收到一条非终态事件；
- 上游源码 `git status --porcelain` 为空。

**步骤 6：验证并提交**

```bash
cd python && python -m pytest tests/test_openhands_adapter.py \
  tests/test_openhands_demo_structure.py
make setup-openhands-agent
make demo-openhands-agentops
git diff --check
git status --short
git add -A
git commit -m "feat: 接入 OpenHands 真实运行可观测"
```

---

## Task 5：版本化 Rubric 模板与多 Judge 投票

**文件：**

- 新建：`migrations/00010_phase4_eval_calibration.sql`
- 修改：`queries/evals.sql`
- 生成：`internal/store/evals.sql.go`
- 新建：`internal/evaluators/rubric.go`
- 新建：`internal/evaluators/rubric_test.go`
- 新建：`internal/evals/judge_aggregation.go`
- 新建：`internal/evals/judge_aggregation_test.go`
- 修改：`internal/evals/service.go`
- 修改：`internal/adapter/evals.go`
- 修改：`internal/httpapi/evals_handlers.go`
- 修改：`internal/httpapi/router.go`
- 修改：`python/agentscope_sdk/multi_judges.py`
- 修改：`python/agentscope_sdk/runner.py`
- 修改：`python/agentscope_sdk/client.py`
- 修改：`python/tests/test_multi_judges.py`
- 修改：`python/tests/test_runner.py`
- 修改：`web/src/api/types.ts`
- 修改：`web/src/api/hooks.ts`
- 修改：`web/src/features/evals/MetricConfigPage.tsx`
- 新建：`web/src/features/evals/RubricTemplatesPage.tsx`
- 修改：`web/src/features/experiments/ExperimentDetailPage.tsx`
- 修改：相关测试

**步骤 1：写失败测试**

覆盖：

- 发布后的 Rubric 不可修改，只能升版本；
- 维度权重总和校验；
- essential/veto 项校验；
- 每个 Judge Vote 独立保存；
- mean/median/trimmed mean/majority；
- 标准差、极差、分歧度和置信度；
- Judge 法定数量不足时低置信或失败；
- 安全 veto 覆盖聚合分数；
- 单 Judge 旧配置保持兼容；
- Judge 错误不会被当成 0 分投票。

**步骤 2：实现 Schema**

新增：

- `rubric_templates`
- `rubric_template_versions`
- `eval_judge_votes`
- `eval_score_aggregations`
- `eval_calibration_runs`
- Annotation Item 的 `gold_scores`、`is_gold`、`annotator_id`、`completed_at`
- Sampling Rule 的策略、预算和分层字段

Vote 关联 project/run/metric/evaluator，保存 provider/model/prompt version、score、
passed、reason、raw response hash、latency、tokens、cost 和 error。

**步骤 3：服务端聚合**

服务端根据已落库 Vote 重新计算最终结果。Runner 不能直接指定 confidence 或
disagreement。聚合完成后写现有 `eval_scores`，保持报表兼容。

**步骤 4：Runner 多 Judge**

支持一维多个 Judge 配置，逐个调用并批量提交 Vote。API Key 仍只从环境变量读取。
失败 Judge 作为 error Vote 保存，但不参与分数计算。

**步骤 5：前端**

- Rubric 模板列表、版本和发布；
- 指标绑定模板版本和多个 Judge；
- 实验结果展示每票、聚合方法、分歧、置信度和 veto。

**步骤 6：验证并提交**

```bash
sqlc generate
go test ./internal/evaluators ./internal/evals ./internal/httpapi
cd python && python -m pytest tests/test_multi_judges.py tests/test_runner.py
cd web && npm test -- --run
cd web && npm run lint
git diff --check
git status --short
git add -A
git commit -m "feat: 支持版本化 Rubric 与多 Judge 聚合"
```

---

## Task 6：人工 Gold 校准、在线采样与统计显著性

**文件：**

- 使用：`migrations/00010_phase4_eval_calibration.sql` 中 Task 5 已建立的校准字段
- 修改：`queries/annotation_queues.sql`
- 修改：`queries/sampling_rules.sql`
- 修改：`queries/evals.sql`
- 新建：`internal/evals/calibration.go`
- 新建：`internal/evals/calibration_test.go`
- 修改：`internal/onlinerval/dispatcher.go`
- 修改：`internal/onlinerval/dispatcher_test.go`
- 修改：`internal/sampling/service.go`
- 修改：`internal/sampling/service_test.go`
- 修改：`internal/annotation/service.go`
- 修改：`internal/annotation/service_test.go`
- 修改：`internal/experiments/compare.go`
- 修改：`internal/experiments/compare_test.go`
- 修改：`internal/httpapi/annotation_handlers.go`
- 修改：`internal/httpapi/sampling_handlers.go`
- 修改：`internal/httpapi/evals_handlers.go`
- 修改：`internal/httpapi/compare_handlers.go`
- 修改：`web/src/features/evals/AnnotationQueuesPage.tsx`
- 修改：`web/src/features/evals/SamplingRulesPage.tsx`
- 新建：`web/src/features/evals/EvalCalibrationPage.tsx`
- 修改：`web/src/features/experiments/ExperimentComparePage.tsx`
- 修改：相关 API 类型、Hook 和测试

**步骤 1：写失败测试**

覆盖：

- 人工 Gold 分数与 Annotator 可追踪；
- MAE、一致率、Cohen's Kappa；
- 空样本和单类别样本不伪造 Kappa；
- 分维度偏差；
- Bootstrap 置信区间固定随机种子可重复；
- 显著回归、无显著变化和样本不足三种结论；
- 在线采样组合随机、失败、异常成本、低置信、高分歧和分层预算；
- 高分歧/低置信样本自动进标注队列；
- 同一 Run 不重复创建待标注项。

**步骤 2：扩展 Annotation**

增加 `gold_scores`、`is_gold`、`annotator_id`、`completed_at`。只有项目成员可以
提交人工 Gold；接口从 Session 获取用户 ID，不能由请求体伪造。

**步骤 3：校准服务**

创建校准运行和报告，固定 Judge/Rubric 版本与样本集合。报告保存指标和样本量，
前端展示分维度偏差和可信性警告。

**步骤 4：在线采样**

Sampling Rule 增加策略、预算和分层字段。Dispatcher 在 Run 完成后读取 Run、
Snapshot、Trace 统计和最近用量决定是否评测。

**步骤 5：统计显著性**

Experiment Compare 对可配对 Case 做 Bootstrap。输出 score/pass/latency/cost 的
区间、p 值近似和样本量。Safety veto 仍优先于统计结论。

**步骤 6：验证并提交**

```bash
sqlc generate
go test ./internal/evals ./internal/onlinerval ./internal/sampling \
  ./internal/annotation ./internal/experiments ./internal/httpapi
cd web && npm test -- --run
cd web && npm run lint
git diff --check
git status --short
git add -A
git commit -m "feat: 增加 Judge 人工校准与显著性分析"
```

---

## Task 7：分布式项目配额

**文件：**

- 新建：`migrations/00011_phase4_quotas.sql`
- 新建：`queries/quotas.sql`
- 生成：`internal/store/quotas.sql.go`
- 新建：`internal/quotas/model.go`
- 新建：`internal/quotas/service.go`
- 新建：`internal/quotas/redis_limiter.go`
- 新建：`internal/quotas/service_test.go`
- 新建：`internal/quotas/redis_limiter_test.go`
- 修改：`internal/ingest/ratelimit.go`
- 修改：`internal/httpapi/ingest_handlers.go`
- 修改：`internal/httpapi/realtime_handlers.go`
- 新建：`internal/httpapi/quota_handlers.go`
- 新建：`internal/httpapi/quota_handlers_test.go`
- 修改：`internal/httpapi/router.go`
- 修改：`internal/app/app.go`
- 修改：`internal/config/config.go`
- 修改：`internal/config/config_test.go`
- 修改：前端 API 类型和 Hook
- 新建：`web/src/features/ingest/QuotaPanel.tsx`
- 修改：`web/src/features/ingest/IngestDashboard.tsx`
- 修改：相关测试

**步骤 1：写失败测试**

覆盖：

- Redis Lua Token Bucket 在并发下不超发；
- 两个 Service 实例共享限额；
- request/span/byte/SSE 限额；
- token/day 和 cost/day 硬预算；
- ingest Redis 故障 fail-open 并产生事件；
- SSE Redis 故障 fail-closed；
- 硬成本预算故障 fail-closed；
- 限流头、Retry-After 和配额剩余；
- 项目管理员可更新配额，普通成员只读；
- 默认配额向后兼容。

**步骤 2：实现定义与用量**

PostgreSQL 保存 `project_quotas` 和版本。短窗口 Redis Key 包含项目、维度和窗口。
Token/成本日用量从 ClickHouse 聚合读取并短期缓存。

**步骤 3：接入 Ingest 与 SSE**

鉴权后、解析大 Body 前先检查 request/byte 预算；解码后检查 span 预算。SSE 建连
和断开原子维护并发计数。所有拒绝写入采集健康或配额事件。

**步骤 4：治理 UI**

在采集治理页显示限额、当前用量、剩余、重置时间和最近超限事件。提供管理员编辑。

**步骤 5：验证并提交**

```bash
sqlc generate
go test -race ./internal/quotas ./internal/ingest ./internal/httpapi
cd web && npm test -- --run
cd web && npm run lint
git diff --check
git status --short
git add -A
git commit -m "feat: 提供跨实例项目配额治理"
```

---

## Task 8：SDK 背压、TTFT、Token 与成本观测

**文件：**

- 修改：`python/agentscope_sdk/tracing.py`
- 修改：`python/agentscope_sdk/runtime.py`
- 修改：`python/agentscope_sdk/adapters/base.py`
- 修改：`python/agentscope_sdk/adapters/openhands.py`
- 新建：`python/agentscope_sdk/export_metrics.py`
- 修改：`python/tests/test_tracing_batch.py`
- 新建：`python/tests/test_export_metrics.py`
- 修改：`python/tests/test_openhands_adapter.py`
- 修改：`internal/ingest/structured.go`
- 修改：`internal/ingest/structured_test.go`
- 修改：`internal/ingest/health.go`
- 修改：`internal/tracestore/clickhouse.go`
- 修改：`internal/tracestore/clickhouse_test.go`
- 新建：`clickhouse/migrations/00005_phase4_performance_governance.sql`
- 修改：`web/src/features/runs/TraceDiagnostics.tsx`
- 修改：`web/src/features/ingest/IngestDashboard.tsx`
- 修改：相关类型和测试

**步骤 1：写失败测试**

覆盖：

- 首 Token 只记录一次；
- input/output/cache/total token 一致性；
- actual cost 优先于 estimated cost；
- queue depth/capacity、drop、retry、batch 和 export latency；
- queue 满时不阻塞业务线程；
- force_flush/shutdown 结果可查询；
- 工具时延、重试和错误；
- 缺失指标显示 unknown，不显示 0ms；
- ClickHouse 新旧数据兼容。

**步骤 2：SDK 导出指标**

包装 BatchSpanProcessor 的可观测状态，提供线程安全快照和周期性低敏指标 Span。
不能访问 OTel 私有字段；自建有界队列时必须保持 OpenTelemetry Exporter 接口兼容。

**步骤 3：流式 LLM 指标**

AdapterSession 增加首 Token 回调、cache token、估算成本和实际成本。OpenHands Token
Callback 传入首 Token 时间。

**步骤 4：ClickHouse Schema 和查询**

为 LLM 表增加 TTFT/cache tokens/cost source，为采集健康增加 SDK queue/drop/retry
指标，为 Tool 保留 duration/retry。更新 Run Detail 和治理统计。

**步骤 5：验证并提交**

```bash
cd python && python -m pytest tests/test_tracing_batch.py \
  tests/test_export_metrics.py tests/test_openhands_adapter.py
go test ./internal/ingest ./internal/tracestore ./internal/httpapi
cd web && npm test -- --run
git diff --check
git status --short
git add -A
git commit -m "feat: 完善 SDK 背压与成本性能观测"
```

---

## Task 9：ClickHouse Rollup、TTL 与异常检测

**文件：**

- 修改：`clickhouse/migrations/00005_phase4_performance_governance.sql`
- 新建：`internal/tracestore/governance.go`
- 新建：`internal/tracestore/governance_test.go`
- 新建：`internal/anomalies/service.go`
- 新建：`internal/anomalies/service_test.go`
- 新建：`internal/adapter/anomalies.go`
- 新建：`internal/httpapi/anomaly_handlers.go`
- 新建：`internal/httpapi/anomaly_handlers_test.go`
- 修改：`internal/httpapi/router.go`
- 修改：`internal/app/app.go`
- 修改：`internal/config/config.go`
- 修改：`web/src/api/types.ts`
- 修改：`web/src/api/hooks.ts`
- 新建：`web/src/features/ingest/AnomalyPanel.tsx`
- 修改：`web/src/features/ingest/IngestDashboard.tsx`
- 修改：相关测试

**步骤 1：写失败测试**

覆盖：

- 小时/天 Rollup；
- raw/rollup TTL 配置与 schema；
- latency/token/cost/tool error/ingest reject 指标；
- rolling median 和 MAD；
- MAD 为零的边界；
- 样本不足返回 `insufficient_baseline`；
- project/agent/version/environment 分组；
- 异常事件幂等；
- API 项目隔离。

**步骤 2：实现 Rollup**

使用 ClickHouse Materialized View 生成小时和天聚合。原始表保留期、小时聚合保留期、
天聚合保留期由明确配置和迁移模板管理；运行中配置变化通过受控管理命令执行，不在
每次请求动态 ALTER。

**步骤 3：实现异常检测**

查询最近窗口和历史基线，在 Go 服务中计算中位数/MAD。异常写入 PostgreSQL
`operational_anomalies` 并通过 Outbox 实时通知。

**步骤 4：前端**

显示当前异常、基线、偏差倍数、样本量和状态。不得把样本不足显示为正常或异常。

**步骤 5：验证并提交**

```bash
go test ./internal/tracestore ./internal/anomalies ./internal/httpapi
cd web && npm test -- --run
cd web && npm run lint
git diff --check
git status --short
git add -A
git commit -m "feat: 增加成本性能聚合与异常检测"
```

---

## Task 10：高并发与故障恢复测试工具

**文件：**

- 新建：`cmd/agentscope-loadtest/main.go`
- 新建：`internal/loadtest/config.go`
- 新建：`internal/loadtest/runner.go`
- 新建：`internal/loadtest/report.go`
- 新建：`internal/loadtest/runner_test.go`
- 新建：`scripts/test-phase4-load.sh`
- 新建：`scripts/test-phase4-recovery.sh`
- 新建：`docs/phase4-capacity-testing.md`
- 修改：`Makefile`

**步骤 1：写失败测试**

覆盖：

- 固定种子生成多项目、多 Run OTLP；
- 并发度和批量大小；
- P50/P95/P99；
- 错误率和丢弃率；
- SSE 首事件/事件延迟；
- 报告 JSON Schema；
- 阈值失败退出非零；
- 测试日志不泄露 Token。

**步骤 2：实现负载工具**

支持：

- OTLP ingest；
- Runtime 状态；
- SSE 客户端；
- 多项目 Token；
- 持续时间/并发/批量；
- JSON 和终端报告。

工具只压测已有服务，不在进程内绕过 HTTP。

**步骤 3：故障恢复脚本**

顺序验证：

1. 产生持续运行和事件；
2. 暂停 Redis；
3. 确认 Run/Trace 继续写入；
4. 恢复 Redis；
5. 确认 Outbox 清空和 SSE reset/reconnect；
6. 模拟 ClickHouse 慢写或不可用；
7. 确认错误可诊断；
8. 让 SDK 队列饱和并确认 drop 计数。

**步骤 4：验证并提交**

```bash
go test ./internal/loadtest ./cmd/agentscope-loadtest
make phase4-load-smoke
make phase4-recovery-test
git diff --check
git status --short
git add -A
git commit -m "test: 增加第四阶段容量与恢复验收"
```

---

## Task 11：Phase 4 统一演示、Playwright 和数据库验收

**文件：**

- 新建：`python/examples/phase4_agentops_demo.py`
- 新建：`scripts/demo-phase4-agentops.sh`
- 新建：`scripts/verify-phase4-data.sh`
- 新建：`web/playwright.config.ts`
- 新建：`web/e2e/phase4-agentops.spec.ts`
- 修改：`web/package.json`
- 修改：`web/package-lock.json`
- 修改：`Makefile`
- 新建：`docs/phase4-enterprise-demo.md`

**步骤 1：定义一个账号、一个项目的完整闭环**

统一 Demo 不创建多个互不关联账号。流程：

1. 注册/登录；
2. 创建项目、Token 和配额；
3. 注册 OpenHands Agent；
4. 建立 SSE 监听；
5. 运行真实 OpenHands 任务；
6. 查看不可变 Snapshot；
7. 创建 Dataset、Rubric Template 和多 Judge Experiment；
8. 执行评测；
9. 标注 Gold；
10. 生成校准报告和实验显著性；
11. 触发小规模并发流量；
12. 查看配额、SDK 背压、成本和异常；
13. 输出所有前端链接和单一登录账号。

**步骤 2：数据库强验证**

脚本使用精确 ID 验证：

- PostgreSQL：Run、State、Snapshot、Outbox、Rubric、Votes、Aggregation、Gold、
  Calibration、Quota、Anomaly；
- ClickHouse：Span、Thought、LLM、Tool、Final Answer、TTFT、Cost、Rollup；
- Redis：Stream 包含目标 Run，未发布 Outbox 为 0；
- API：SSE 事件到达顺序、Snapshot Hash、Judge 置信度、配额用量；
- OpenHands 上游工作区干净。

任意缺失都必须退出非零。

**步骤 3：Playwright**

验证：

- Agent 在线和当前 Run 实时变化；
- Run Timeline 在任务结束前出现工具事件；
- Snapshot 页面；
- Judge Vote、分歧和置信度；
- Gold 校准；
- 配额、异常和采集健康；
- 无 403/500；
- 无布局重叠；
- 1440×900 和 390×844 截图。

**步骤 4：提交**

```bash
cd web && npm test -- --run
cd web && npm run lint
cd web && npm run build
cd web && npx playwright test
make demo-phase4-agentops
git diff --check
git status --short
git add -A
git commit -m "test: 跑通第四阶段企业 AgentOps 闭环"
```

---

## Task 12：全量回归、质量审计与文档收口

**文件：**

- 修改：`README.md`
- 修改：`CLAUDE.md`
- 修改：`docs/enterprise-demo-guide.md`
- 新建：`docs/phase4-verification-report.md`
- 按审计结果修改必要代码和测试

**步骤 1：全量自动化验证**

```bash
go vet ./...
go test -race ./...
cd python && python -m pytest
cd web && npm test -- --run
cd web && npm run lint
cd web && npm run build
```

**步骤 2：真实基础设施验证**

```bash
make db-up
make demo-phase4-agentops
make phase4-load-smoke
make phase4-recovery-test
cd web && npx playwright test
```

**步骤 3：完成要求逐项审计**

对设计文档第 14 节每项建立“要求 → 证据”表：

- 代码位置；
- 测试名称；
- 运行命令；
- PostgreSQL 查询；
- ClickHouse 查询；
- Redis 查询；
- 前端截图；
- 结果。

间接证据或仅单元测试不足以证明真实 E2E。

**步骤 4：架构和安全审计**

确认：

- handler 无 SQL；
- Redis 无业务事实；
- Project Token 和 LLM Key 未落库、未入日志；
- SSE 不跨项目；
- Snapshot 和实时 Payload 已脱敏；
- OpenHands 源码未修改；
- 旧 API 和 Demo 仍通过；
- 主工作区用户未提交修改未被覆盖。

**步骤 5：提交收口文档**

```bash
git add README.md CLAUDE.md docs
git commit -m "docs: 完成第四阶段验收与运维说明"
```

**步骤 6：最终 Worktree 状态**

```bash
git status --short
git log --oneline --decorate -15
```

预期：Worktree 干净；每个任务独立 Commit；所有验收证据记录在
`docs/phase4-verification-report.md`。
