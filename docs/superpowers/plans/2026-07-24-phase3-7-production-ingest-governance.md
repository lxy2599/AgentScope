# Phase 3.7 生产采集治理实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `using-git-worktrees`, `test-driven-development`, `systematic-debugging`, and `verification-before-completion`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为任意 Agent 提供 SDK 导出统计、服务端采集健康事件、项目与 Agent 聚合统计，以及可诊断的采集健康页面。

**Architecture:** 保持 OTLP 主链路与 PostgreSQL + ClickHouse 架构。SDK 在 exporter 边界统计结果；Go 服务端记录认证请求的无内容健康事件；ClickHouse 聚合项目与 Agent 统计；前端消费明确健康状态。

**Tech Stack:** Python/OpenTelemetry/pytest、Go/ClickHouse/go test、React/TypeScript/Ant Design/Vitest、Docker Compose。

## 全局约束

- 工作仅在 `worktree-phase3-7-production-ingest` 完成，提交信息使用中文。
- 默认 `capture_content=false`、`enable_redaction=true`；健康事件不得保存 prompt、工具参数、token 或原始异常 payload。
- 继续兼容 `/v1/traces` 与现有 ingest stats API；不引入 Kafka、NATS、SSE/WebSocket 或外部监控系统。
- 主 Trace 写入失败必须对客户端可见；健康事件写入失败不得改变主 Trace 写入结果。
- 每项实现均先写失败测试，再做最小实现和验证。
- 演示必须验证 API、PostgreSQL Run 关联及 ClickHouse 原始/聚合数据。

## 任务 1：SDK 导出统计

**文件：**
- 修改：`python/agentscope_sdk/tracing.py`
- 修改：`python/agentscope_sdk/__init__.py`
- 修改：`python/tests/test_tracing_batch.py`

**产出接口：**
- 冻结 dataclass `TracingStats`：`batches_attempted`、`batches_succeeded`、`batches_failed`、`spans_attempted`、`spans_exported`、`spans_dropped`、`retries`、`last_error`、`last_error_at`、`last_success_at`。
- `get_tracing_stats(server_url: str, project_token: str) -> TracingStats`。
- `configure_tracing(..., export_queue_size: int | None = None, max_export_retries: int | None = None)`。

- [ ] 先用 fake `SpanExporter` 写失败测试：成功 batch、一次 transient failure 后成功、永久失败；断言统计与错误文本不泄露 bearer token。
- [ ] 执行 `cd python && python -m pytest tests/test_tracing_batch.py -q`，预期因新接口不存在失败。
- [ ] 实现线程安全计数器与 `ObservedSpanExporter`。仅记录 span 数、export result 和脱敏错误类别。重试默认最多 2 次；队列大小由 `AGENTSCOPE_EXPORT_QUEUE_SIZE` 控制，默认 2048；无效配置回退安全默认值。
- [ ] 执行 `cd python && python -m pytest tests/test_tracing_batch.py tests/test_redact.py -q`，预期通过。
- [ ] 提交：`feat: 增加 SDK 采集导出统计`。

## 任务 2：服务端健康事件与 ClickHouse 聚合

**文件：**
- 新建：`internal/ingest/health.go`
- 新建：`clickhouse/migrations/00004_ingest_health.sql`
- 修改：`internal/httpapi/ingest_handlers.go`
- 修改：`internal/httpapi/dependencies.go`
- 修改：`internal/tracestore/clickhouse.go`
- 修改：`internal/tracestore/clickhouse_test.go`
- 修改：`internal/httpapi/ingest_handlers_test.go`

**产出接口：**
- `ingest.HealthEvent`：project、agent、outcome、HTTP status、count、identity flags、duration、error code。
- `ingest.HealthRecorder`：`RecordHealthEvent(context.Context, ingest.HealthEvent) error`。
- 可选依赖：`Dependencies.IngestHealth ingest.HealthRecorder`。

- [ ] 用 fake recorder 写失败测试，覆盖 accepted、rate limited、body too large、decode failed、storage failed；断言 outcome、HTTP status、span count、`MissingRunID`、`MissingTraceID`。鉴权失败不得调用 recorder。
- [ ] 写 ClickHouse 集成测试，插入跨项目、跨 Agent、跨 15 分钟 bucket 的事件，验证项目隔离、拒绝数、身份缺失和最近错误。
- [ ] 执行 `go test ./internal/httpapi ./internal/tracestore -run 'Test.*Ingest.*Health|Test.*Health.*Aggregation' -v`，预期失败。
- [ ] 实现结果枚举：`accepted`、`rate_limited`、`rejected_body`、`rejected_decode`、`storage_failed`。在 project 身份已知后用 defer 记录一次事件；记录失败仅日志化。缺少 run/trace ID 不拒绝请求但必须计数。
- [ ] 创建 `trace_ingest_health_events`：只保存元数据，按月分区、按 `(project_id, event_time, agent_name)` 排序、TTL 90 天；创建 15 分钟和小时聚合，查询必须按 project ID 过滤。
- [ ] 执行 `gofmt`、`go test ./internal/httpapi ./internal/tracestore ./internal/ingest -v`、`git diff --check`。
- [ ] 提交：`feat: 记录采集健康事件并聚合统计`。

## 任务 3：健康 API 与前端诊断页

**文件：**
- 修改：`internal/httpapi/ingest_stats_handlers.go`
- 修改：`internal/httpapi/routes.go`
- 修改：`internal/app/app.go`
- 修改：`web/src/api/types.ts`
- 修改：`web/src/api/hooks.ts`
- 修改：`web/src/features/ingest/IngestDashboard.tsx`
- 修改：`web/src/features/ingest/ingest.test.tsx`
- 修改：`internal/httpapi/ingest_stats_handlers_test.go`

**产出接口：**
- 扩展 `IngestStatsDTO`：status、请求结果计数、身份计数、p50/p95、最近时间。
- `GET /api/projects/{projectID}/ingest-stats/events?minutes=N&limit=N`。
- 状态枚举：`healthy`、`no_traffic`、`degraded`、`storage_error`、`unavailable`。

- [ ] 写失败测试：未装配 ClickHouse 返回 `200/unavailable`；无事件返回 `no_traffic`；storage failure 返回 `storage_error`；越权仍为 403。前端 fixture 覆盖五种状态、Agent 行和最近事件。
- [ ] 执行 `go test ./internal/httpapi -run IngestStats -v` 与 `cd web && npm test -- --run src/features/ingest/ingest.test.tsx`，预期失败。
- [ ] 扩展 querier 而不让 handler 直接依赖 ClickHouse；`minutes` 限制 1..1440，`limit` 限制 1..100。
- [ ] Dashboard 按 status 展示健康状态、项目请求结果、身份完整性、p50/p95、最近成功/失败、Agent 表与事件表。错误码映射固定中文诊断，不显示原始异常。
- [ ] 执行 Go API 测试、前端测试和 `npm run build`。
- [ ] 提交：`feat: 提供可诊断的采集健康页面`。

## 任务 4：端到端演示、数据库验证与受控全仓优化

**文件：**
- 新建：`python/examples/demo_ingest_health.py`
- 新建：`scripts/verify-ingest-health-demo.sh`
- 修改：`Makefile`
- 修改：`docs/enterprise-demo-guide.md`
- 修改：仅限审查发现且可复现的低风险缺陷文件。

**产出接口：**
- `make demo-ingest-health` 在任一 API/数据库验证失败时非零退出。
- 终端摘要输出 project ID、Agent 名称和 expected accepted/rejected/identity counts。

- [ ] 新增 `python/tests/test_demo_ingest_health_structure.py`，断言 demo 创建项目/token，发送正常与缺少 run ID 的 trace，触发已认证拒绝，并将 project ID 传给同时查询 API、PostgreSQL、ClickHouse 的 verifier。
- [ ] 执行该测试，预期因 demo/verifier 不存在失败。
- [ ] 实现 demo：用 SDK/OTLP 产生成功数据，并用可控 body/span 限制或 rate limiter 产生一条已认证拒绝。verifier 必须核对 `runs`、`trace_spans`、`trace_ingest_health_events` 和聚合表；任一期望不符即非零退出；默认不得跳过数据库验证。
- [ ] 启动依赖和服务，执行 `make demo-ingest-health`；核对 API、PostgreSQL 与 ClickHouse 的成功、拒绝、身份缺失计数。
- [ ] 执行 `go vet ./...`、`go test ./...`、Python 全量 pytest、前端全量 Vitest 与 build、`git diff --check master...HEAD`。
- [ ] 审查 API 契约、认证边界、迁移幂等性、demo 链接与文档命令。只修复测试或完整演示可复现的低风险问题；破坏性 API 或架构重构只记录建议。
- [ ] 提交 demo：`test: 增加生产采集健康闭环验证`；审查修复使用独立中文 `fix:` 提交。

## 完成条件

1. SDK 可返回安全导出统计，失败、重试和丢弃不泄露 token 或内容。
2. 已认证 ingest 的接受、拒绝、写入失败和身份缺失均有 ClickHouse 健康证据。
3. Dashboard 能区分无流量、降级、存储失败、后端不可用，并保持项目权限边界。
4. `make demo-ingest-health` 严格验证 API、PostgreSQL、ClickHouse 原始表和聚合表。
5. Go/Python/前端全量测试、构建、`go vet` 和 `git diff --check` 全部通过。

