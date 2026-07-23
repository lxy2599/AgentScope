# Phase 3.7 生产采集治理设计

## 目标

把 AgentScope 已有的 Trace 采集链路从“可写入 ClickHouse”升级为“可诊断、可度量、可治理”的生产链路。平台必须能回答：某个项目是否正在采集、数据为何缺失、SDK 是否发生重试或丢弃、服务端是否限流或写入失败，以及哪些 Agent 的吞吐、错误率和延迟异常。

本阶段面向所有通过 AgentScope Python SDK 或兼容 OTLP 接入的 Agent；不针对任何具体 demo 或开源 Agent 编写特例。

## 现状与问题

当前链路已经支持 BatchSpanProcessor、gzip、服务端 body/span 限制、项目级 token bucket、ClickHouse 原始 Trace 表、小时聚合表和基础 Ingest Dashboard。它仍有三个断点：

1. Python SDK 没有稳定的导出运行时统计。用户不知道本地队列、导出成功、失败、重试、丢弃和最后错误。
2. 服务端只将成功写入的数据反映在 Trace 表中；限流、解码失败、身份字段缺失和 ClickHouse 写入失败没有统一的项目级健康事件。
3. Dashboard 只有业务量与少量 Agent 错误统计，无法明确区分“无流量”“SDK 导出失败”“服务端拒绝”“存储失败”“字段不完整”。

## 边界

本阶段实现 SDK 与服务端的采集健康闭环、滚动统计、可解释 API 和前端诊断页。

不实现 Kafka/NATS、跨进程共享限流、账单计量、告警通知、SSE/WebSocket 或完整配置中心。限流继续是进程内项目级保护；其部署限制必须在 API 和文档中说明。

## 方案比较

### 方案 A：仅扩展 ClickHouse 原始 Trace 聚合

优点是改动最小。缺点是只能看到成功写入的数据，无法观察 SDK 本地失败、HTTP 429/5xx 和被拒绝请求，不能回答“为什么没有数据”。不采用。

### 方案 B：SDK 运行时指标 + 服务端健康事件 + ClickHouse 聚合

SDK 公开只读统计快照；服务端对每次 ingest 的接受、拒绝、写入失败和身份完整性写入轻量健康事件；ClickHouse 同时保存原始事件和按项目/Agent/时间窗口的聚合查询。Dashboard 通过同一个 API 返回数据和状态解释。

这是本阶段采用的方案。它保留 OTLP 主数据路径，能覆盖端到端故障，且不新增消息中间件。

### 方案 C：引入独立监控系统和消息队列

可提供更强的多实例一致性与告警能力，但需要 Prometheus/Kafka/OTel Collector 等额外部署、运维和鉴权设计，超过当前 Phase 3 范围。后续规模化部署时再评估。

## 架构

```text
Python SDK
  TracingConfig + ObservedExporter
    -> 本地计数：导出批次、span、成功、失败、重试、丢弃、队列压力、最后错误
    -> OTLP HTTP /v1/traces

Go ingest
  token auth -> body/span 限制 -> 项目限流 -> decode -> run 关联 -> ClickHouse 批写入
    -> IngestHealthRecorder（accept/reject/storage failure/identity missing）

ClickHouse
  原始 trace_* 表
  trace_ingest_health_events（短期故障审计）
  trace_ingest_rollups（15m/1h 项目与 Agent 统计）

API / UI
  ingest health summary + per-agent rows + diagnostics
  -> 采集健康页显示状态、指标、异常原因与最近错误
```

## 数据模型与口径

### SDK 统计

新增 `TracingStats` 只读快照，并通过 `get_tracing_stats()` 获取。字段至少包括：

- `batches_attempted`、`batches_succeeded`、`batches_failed`
- `spans_attempted`、`spans_exported`、`spans_dropped`
- `retries`、`last_error`、`last_error_at`
- `last_success_at`

统计按已配置的 `(server_url, project_token)` exporter 聚合。SDK 不承诺读取 OpenTelemetry 内部队列长度，因为该值不是稳定公开 API；改用明确的导出结果、失败和丢弃计数。所有数字均为当前进程生命周期内的累计值。

默认保持安全：`capture_content=false`、`enable_redaction=true`。新增 `queue_size` 参数与 `AGENTSCOPE_EXPORT_QUEUE_SIZE`，并验证 batch、queue、delay、timeout 均为合法正整数；无效配置回退安全默认值。

### 服务端健康事件

每个 ingest 请求产生一个不含业务内容的健康事件，记录：项目、Agent（可为空）、时间、结果分类、HTTP 状态、span 数、语义记录数、是否缺少 run_id/trace_id、错误代码和延迟。

结果分类固定为：`accepted`、`rate_limited`、`rejected_body`、`rejected_decode`、`auth_failed`、`storage_failed`。身份不完整不拒绝正常 OTLP 数据，但必须以 `missing_run_id`、`missing_trace_id` 计数进入事件与聚合。

鉴权失败无法可靠归属项目时不写项目统计，避免把未知 token 污染任意项目；仅在服务端结构化日志记录错误。其余已经认证的请求必须写健康事件，即使后续失败。

### 聚合

新增 15 分钟和小时 bucket 的项目/Agent 聚合。查询窗口支持 5、15、60 分钟及最多 24 小时任意正整数分钟；后端统一按 UTC bucket 查询。

项目汇总至少包含：成功请求、拒绝请求、存储失败、span/语义记录数、缺失身份字段数、错误 span 数、token、成本、p50/p95 span 延迟、最近成功时间、最近失败时间和最近错误分类。

Agent 汇总使用成功 ingest 的 resource/semantic Agent 名称；未识别 Agent 归入 `unknown`，但必须可见，不能被静默丢弃。

## API 与前端

保留现有路径，扩展为：

- `GET /api/projects/{projectID}/ingest-stats?minutes=N`
- `GET /api/projects/{projectID}/ingest-stats/agents?minutes=N`
- `GET /api/projects/{projectID}/ingest-stats/events?minutes=N&limit=N`

ClickHouse 未配置、查询失败和无数据必须返回可区分的状态字段，不允许以 HTTP 500 伪装为空数据。权限仍由项目成员校验控制。

Dashboard 展示四层信息：

1. 当前健康状态：健康、无流量、存在拒绝、存在存储失败、后端不可用。
2. 项目卡片：成功/拒绝/失败、span、语义记录、缺失身份、吞吐、p50/p95、token、成本。
3. Agent 表：Agent 名称、请求数、错误率、拒绝数、p95、最近成功和异常。
4. 最近采集事件：时间、结果、错误分类、HTTP 状态、span 数、Agent 和可操作的诊断说明。

页面只展示摘要和错误码，永不展示 token、prompt、工具参数或原始异常 payload。

## 错误处理与隐私

- 健康事件写入失败不得掩盖主 Trace 写入结果；记录日志并继续返回主链路结果。
- 主 Trace 写入失败必须返回 500，并尽最大努力记录 `storage_failed`。
- 限流保留 429 和可读错误码；SDK 识别可重试 HTTP 状态后有限重试，非可重试 4xx 不重试。
- 所有统计与健康事件只存元数据，不存完整内容。
- 现有 PII 脱敏继续作为 SDK 默认，新增测试覆盖错误文本不会包含 token。

## 验收与测试

先写失败测试，再实现。至少覆盖：

1. Python：安全默认值、queue 配置、成功/可重试失败/不可重试失败、统计快照、脱敏。
2. Go：认证后 accepted、429、body/decode 拒绝、存储失败、缺失 run_id/trace_id 都生成正确健康统计；无 ClickHouse 时 API 返回可解释状态而非 500。
3. ClickHouse：15m/1h 聚合按项目和 Agent 隔离，p50/p95、错误率、成本/token 和身份缺失计数正确。
4. 前端：五种健康状态、Agent 行和最近事件的渲染；项目越权仍返回 403。
5. 端到端：运行一个通用 Agent，制造一条成功批次、一条被限流或拒绝批次及一条缺少身份字段的 batch；核对 SDK 统计、API、PostgreSQL run 关联与 ClickHouse 原始/聚合数据一致。

完整验证命令：

```bash
go test ./...
cd python && python -m pytest
cd ../web && npm test -- --run && npm run build
make demo-enterprise-agentops
```

## 后续全仓审查

3.7 完成后，在同一 worktree 进行一次受控健康审查，检查 Go、Python、前端、迁移和 demo 的：正确性、测试覆盖、配置一致性、认证边界、数据血缘和文档准确性。

只直接修复可复现的低风险缺陷、测试空洞、稳定性问题和本阶段相关重复逻辑。涉及 API 破坏性调整、数据迁移重构、依赖替换或新的产品能力时，只形成后续建议，不在本阶段直接实施。
