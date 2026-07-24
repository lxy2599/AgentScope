# Phase 4 实时可观测、可信评测与生产治理设计

**日期：** 2026-07-24
**状态：** 已确认，待实施
**依据：** `docs/可观测.md`、Phase 3 现有实现、OpenHands 当前事件契约

## 1. 背景

Phase 3 已经完成通用 Runtime Contract、Agent 控制中心、Trace Explorer、Rubric
评测、Trace 到 Dataset 闭环、实验对比、生产采集健康，以及 Hermes、OpenClaw
真实接入。当前平台可以记录并查询 Agent 运行，但仍存在五个生产级缺口：

1. 尚未用 OpenHands 这类长任务代码 Agent 验证通用协议。
2. 页面主要依赖轮询，运行状态和语义事件不能稳定实时到达。
3. Run 与 Experiment 只保存版本标签，不能证明运行时实际使用了什么配置。
4. LLM Judge 有权重、阈值和否决能力，但缺少多 Judge、分歧、置信度和人工校准。
5. 限流仍以单进程为主，缺少分布式配额、压力指标和可重复的容量验收。

`docs/可观测.md` 强调评估对象是“模型与 Harness 的组合体”，评估必须可复现，
并通过模型替换、消融、在线样本、人工校准和统计显著性支撑工程决策。Phase 4
将这些方法论落成平台契约，而不是继续增加孤立 Demo。

## 2. 目标

Phase 4 必须同时完成以下目标：

- 用无侵入 OpenHands Adapter 验证任意 Agent 框架接入能力。
- 在多后端实例场景下实时展示 Agent、Run、状态和语义事件。
- 为每个 Run 保存不可变、可校验、隐私安全的执行快照。
- 让多维评测具备多 Judge 聚合、分歧度、置信度、人工 Gold 校准和在线采样。
- 提供分布式配额、SDK 背压指标、成本与时延分析、异常检测和高并发基准。
- 保持 PostgreSQL、ClickHouse 和 Redis 的职责单一，并在 Redis 故障时优雅降级。

## 3. 非目标

- 不托管 OpenHands Cloud，不把 OpenHands 运行时嵌入 Go 服务。
- 不把 Redis 作为 Run、Trace、评测或计费的权威存储。
- 不在服务端保存 API Key、访问令牌或未脱敏环境变量。
- 不在本阶段引入 Kafka、Flink、独立计费服务或完整告警通知平台。
- 不承诺跨地域 Exactly Once；平台提供项目内有序、可恢复、前端幂等的实时语义。

## 4. 总体架构

```text
External Agent / OpenHands
        |
        | Runtime API + OTLP
        v
Go API -----------------------------------------------------+
  |                                                         |
  | transaction                                             | batch
  v                                                         v
PostgreSQL                                               ClickHouse
  - runs                                                  - spans/events
  - run_state_events                                      - semantic tables
  - run_snapshots                                         - cost/latency rollups
  - realtime_outbox
  - quotas/eval calibration
        |
        | outbox relay
        v
Redis Streams
  - realtime fan-out
  - reconnect cursor
  - distributed rate limit
        |
        | SSE
        v
React UI
```

### 4.1 存储职责

- **PostgreSQL** 保存业务事实、状态机、配置快照、评测投票、校准结果、配额和 Outbox。
- **ClickHouse** 保存 Trace 明细、语义事件、Token、成本、时延和聚合分析。
- **Redis Streams** 只保存短期实时通知和游标；丢失后可从权威 API 重建页面。
- **Redis Lua** 保存短期分布式 Token Bucket 状态；持久配额定义仍在 PostgreSQL。

### 4.2 一致性原则

运行状态类事件与 PostgreSQL 业务更新在同一事务内写入 `realtime_outbox`。Trace
类事件只有在 ClickHouse 批量写入成功后才产生实时通知。Relay 使用 Outbox ID
作为幂等键投递 Redis，前端以事件 ID 去重。

不尝试让 PostgreSQL、ClickHouse 和 Redis 组成分布式事务。系统保证：

- 权威写入成功后才通知。
- 重复通知可幂等处理。
- Redis 不可用时 Outbox 保留，Relay 恢复后继续投递。
- Redis 游标过期时 SSE 发送 `reset`，前端重新请求权威详情。

## 5. Phase 4.1：OpenHands Adapter

### 5.1 接入方式

使用 OpenHands `Conversation(callbacks=[...], token_callbacks=[...])` 的只读观察者
接口，不修改上游源码，不 monkey patch 核心类。Adapter 依赖保持可选，SDK 未安装
OpenHands 时不影响其他用户。

OpenHands 的事件系统是不可变、可序列化的追加日志，明确允许辅助服务读取事件流，
因此它既是接入点，也是验证 AgentScope 通用事件模型的合适对象。

参考：

- https://docs.openhands.dev/sdk/arch/events
- https://docs.openhands.dev/sdk/api-reference/openhands.sdk.conversation
- https://docs.openhands.dev/sdk/guides/llm-streaming

### 5.2 事件映射

| OpenHands 事件 | AgentScope 语义 |
| --- | --- |
| `MessageEvent(source=user)` | Run 输入或用户消息 |
| `MessageEvent(source=agent)` | `thought` 或 `final_answer` |
| `ActionEvent` | thought + tool start + security risk |
| `ObservationEvent` | tool finish/result |
| `UserRejectObservation` | guardrail/approval denied |
| `AgentErrorEvent` | tool error，Run 可继续 |
| `ConversationErrorEvent` | Run failure |
| `ConversationStateUpdateEvent` | Run state transition |
| `SystemPromptEvent` | Prompt/Tool Schema snapshot |
| `LLMCompletionLogEvent` | LLM input/output/usage/cost |
| `TokenEvent` / token callback | TTFT、流式输出进度 |
| `Condensation*` | context compression/memory event |
| `PauseEvent` | `waiting_user` |

Adapter 必须关联 OpenHands event ID、tool call ID、LLM response ID 和 conversation ID，
确保并行工具调用可以正确配对。未知事件保存为通用 Span，不得导致 Agent 运行失败。

### 5.3 真实验收

提供固定上游版本的接入清单、安装脚本、真实演示和验证脚本。演示至少执行：

- 多轮 LLM 推理；
- 终端命令；
- 文件读取与修改；
- 工具失败后恢复；
- 最终回答；
- Token/成本采集；
- Run 状态实时变化。

验证 PostgreSQL Run/State/Snapshot、ClickHouse Span/LLM/Tool/Thought/Final Answer、
Redis Stream、SSE 和前端页面一致。

## 6. Phase 4.2：实时事件平面

### 6.1 统一事件信封

所有实时事件使用 `realtime-event-v1`：

```json
{
  "id": 12345,
  "schema_version": "realtime-event-v1",
  "project_id": "uuid",
  "run_id": "uuid",
  "agent_id": "uuid",
  "agent_instance_id": "uuid",
  "type": "run.state.changed",
  "occurred_at": "2026-07-24T10:00:00Z",
  "payload": {},
  "payload_redacted": true
}
```

事件类型至少覆盖：

- `agent.registered`
- `agent.heartbeat`
- `agent.status.changed`
- `run.started`
- `run.state.changed`
- `run.semantic.appended`
- `run.completed`
- `run.failed`
- `ingest.accepted`
- `ingest.rejected`
- `eval.completed`
- `quota.warning`
- `quota.exceeded`

### 6.2 Outbox 与 Relay

新增 `realtime_outbox`：

- `id BIGSERIAL`：全局有序 SSE ID；
- 项目、Run、Agent 和事件类型索引；
- JSONB payload 只放低敏摘要；
- `published_at`、`attempts`、`last_error`；
- 7 天清理策略。

Relay 使用 `FOR UPDATE SKIP LOCKED` 支持多实例并发领取，以
`outbox:{id}` 为 Redis 幂等字段。Redis Stream 按项目分区：
`agentscope:events:{project_id}`，近似裁剪保留最近事件。

### 6.3 SSE API

```text
GET /api/projects/{projectID}/events/stream
  ?run_id=
  &agent_id=
  &types=run.started,run.state.changed
```

- 使用现有 Session Cookie 鉴权和项目成员校验。
- 支持 `Last-Event-ID`。
- 定期发送心跳。
- 限制项目和用户连接数。
- 慢消费者断开并返回可诊断原因。
- Redis 不可用时返回 `realtime_unavailable`，前端自动继续轮询。
- 游标早于 Stream 最小 ID 时发送 `reset`。

### 6.4 前端行为

封装单一 `useProjectEventStream`，负责连接、退避、去重和重置。事件只用于：

- 更新 Agent 在线状态和心跳；
- 更新 Active Runs 状态；
- 触发 Run Detail/Timeline 增量刷新；
- 展示采集和配额告警。

React Query 仍负责权威数据缓存。SSE 不直接构造完整业务实体，防止重连后状态漂移。

## 7. Phase 4.3：不可变运行快照

### 7.1 数据模型

新增 `run_snapshots`，每个 Run 只能创建一次：

- `run_id` 唯一外键；
- `schema_version`；
- `canonical_payload JSONB`；
- `payload_sha256`；
- `capture_policy`；
- `created_at`。

规范化 Payload 包含：

- model provider/name/version/parameters；
- system prompt/template/rendered prompt 的版本、哈希和可选脱敏内容；
- tool/skill/MCP/knowledge base schema 与版本；
- harness/framework/SDK/agent/upstream commit；
- feature flags/ablation flags；
- runtime/container image/dependency lock digest；
- dataset/case/version/input hash；
- redaction/content capture policy。

数据库触发器或权限规则拒绝 `UPDATE` 和 `DELETE`，保证不可变。重复提交同一哈希返回
原快照；同一 Run 不同哈希返回冲突。

### 7.2 SDK 与 API

Python SDK 提供规范化构建器，在 `start_run` 时创建快照。未使用 SDK 的框架可通过
Runtime API 传入同一结构。

新增：

- `GET /api/runs/{runID}/snapshot`
- Run Detail 的“运行配置”视图
- Experiment Compare 的快照差异视图

默认只保存哈希、版本和经过现有 Redactor 处理的内容。API Key、Token、Cookie、
环境变量值和文件系统密钥永远不得进入快照。

## 8. Phase 4.4：可信评测与校准

### 8.1 Rubric 模板

新增项目级、版本化 `rubric_templates`，支持：

- 多维标准、权重和评分锚点；
- essential items；
- veto items；
- Judge 提示词版本；
- 适用 Agent/任务标签；
- 发布后不可修改，只能创建新版本。

### 8.2 多 Judge 投票

每个维度可配置多个 Judge。新增 `eval_judge_votes` 保存每个 Judge 的：

- provider/model/prompt version；
- score/passed/reason；
- raw response hash；
- latency/tokens/cost；
- error；
- created_at。

聚合支持：

- mean；
- median；
- trimmed mean；
- majority vote。

聚合结果保存：

- 最终分数和通过状态；
- Judge 数量和成功数量；
- 标准差、极差、分歧度；
- 置信度；
- veto 结果；
- 降级原因。

Judge 不足最小法定数量时不得伪装成高置信结果。

### 8.3 人工 Gold 校准

Annotation Item 增加结构化人工维度分数和 Gold 标记。新增校准运行与报告：

- score MAE；
- 二元一致率；
- Cohen's Kappa；
- 分维度偏差；
- 样本量；
- Judge 版本；
- 置信区间。

高分歧、低置信度、Judge 错误和安全否决样本自动进入 Annotation Queue。

### 8.4 在线采样

扩展现有 Sampling Rule，组合以下策略：

- 固定随机基线；
- 失败和异常 Run 全采；
- 高成本、高时延、工具重试加权；
- Judge 低置信度和高分歧加权；
- Agent/版本/环境分层采样；
- 每项目预算上限。

在线评测结果必须关联 Run Snapshot，防止将不同 Harness 配置混为一组。

### 8.5 统计决策

Experiment Comparison 增加配对 Bootstrap 置信区间、样本量和显著性结论。页面必须
区分“没有显著变化”与“显著回归”，不得仅凭平均分差异下结论。

## 9. Phase 4.5：高并发与成本治理

### 9.1 分布式配额

新增 `project_quotas` 和用量查询，支持：

- requests/min；
- spans/min；
- ingest bytes/min；
- concurrent SSE connections；
- tokens/day；
- cost/day。

短窗口使用 Redis Lua Token Bucket 原子判断，日级 Token/成本以 ClickHouse 聚合为
权威并在 Redis 缓存。响应返回标准限流头和项目配额头。

Redis 故障策略可配置：

- ingest 请求默认 fail-open，但记录 `quota_backend_unavailable`；
- SSE 连接限制默认 fail-closed；
- 显式硬成本预算默认 fail-closed。

### 9.2 SDK 背压

Python SDK 暴露并上报：

- queue depth/capacity；
- enqueued/exported/dropped spans；
- batch size；
- retry count；
- export latency；
- last error；
- flush/shutdown result。

业务线程不得等待网络导出。队列满时按配置丢弃并记录计数，不得静默阻塞。

### 9.3 成本与性能

扩展 LLM 和 Tool 语义数据：

- time to first token；
- input/output/cache tokens；
- model/provider；
- estimated/actual cost；
- tool latency/retry/error；
- run wall time；
- waiting LLM/tool/retrieval time。

新增小时和天级 Rollup，原始 Trace 与聚合数据采用分层 TTL。TTL 必须通过配置和迁移
明确，不能继续只依赖散落的硬编码。

### 9.4 异常检测

以同项目、Agent、版本和环境为基线，使用滚动中位数和 MAD 检测：

- 延迟异常；
- Token/成本异常；
- 工具失败率异常；
- ingest 拒绝或丢弃异常。

异常结果写入可查询事件并在控制中心展示。样本不足时明确标记
`insufficient_baseline`，不得生成误导性告警。

### 9.5 压测

提供仓库内可重复压测工具，至少覆盖：

- 多项目并发 OTLP；
- 大批量 Span；
- 多后端实例 Outbox Relay；
- 多 SSE 客户端；
- Redis 短暂不可用与恢复；
- ClickHouse 慢写；
- SDK 队列饱和。

报告至少包含吞吐、P50/P95/P99、错误率、丢弃率、恢复时间和资源配置。阈值通过环境
变量配置，CI 运行小规模回归，本地/集成环境运行完整容量测试。

## 10. 安全与隐私

- 实时 payload 只允许低敏摘要。
- 所有内容进入 Outbox、Snapshot 或 Judge 记录前经过 Redactor。
- 原始 Judge 响应默认只保存哈希和受限摘要。
- SSE 必须校验 Session 和项目成员关系。
- Redis 仅在内部网络监听并启用认证配置，不向宿主机暴露默认端口。
- 项目之间使用独立 Stream key，并在服务端再次校验事件 `project_id`。
- 任何日志不得输出 Project Token、LLM Key、Session Cookie 或完整 Prompt。

## 11. 兼容与迁移

- Redis 配置默认可选；未配置时服务继续启动，实时能力退化为轮询。
- 旧 Run 没有 Snapshot 时 API 返回明确 `snapshot_unavailable`，不伪造快照。
- 旧 Evaluator 继续单 Judge 路径，新配置才启用投票。
- 现有 OTLP、Runtime、Runner 和业务 API 保持兼容。
- ClickHouse 新列使用 `ADD COLUMN IF NOT EXISTS`，旧数据读取提供默认值。
- 前端保留轮询作为 SSE 降级和页面首次加载路径。

## 12. 测试策略

### 12.1 单元测试

- OpenHands 事件映射和未知事件降级；
- Outbox 幂等、Relay 重试和 SSE 过滤；
- Snapshot 规范化、哈希、脱敏和不可变冲突；
- Judge 聚合、分歧、置信度、veto 和法定数量；
- Gold 校准指标；
- Redis Token Bucket 和故障策略；
- 异常检测及样本不足。

### 12.2 集成测试

- PostgreSQL + Redis Outbox 到 SSE；
- ClickHouse 成功写入后产生语义事件；
- Redis 断开、恢复和 Cursor 过期；
- 多实例 Relay 不重复发布；
- 项目权限隔离；
- 配额跨实例一致；
- Rollup 和 TTL schema。

### 12.3 前端测试

- EventSource 重连、去重、reset 和轮询降级；
- Agent/Active Run/Timeline 实时更新；
- Snapshot 详情和差异；
- 多 Judge 投票、分歧、校准和统计结论；
- 配额、异常和 SDK 背压展示。

### 12.4 真实端到端

真实 OpenHands 运行必须证明：

1. OpenHands 上游源码未被修改。
2. Run 开始后页面实时出现状态。
3. 思考、LLM、终端、文件、错误恢复和回答均有语义记录。
4. Run Snapshot 可查询且哈希一致。
5. 多 Judge 与人工 Gold 校准流程可跑通。
6. PostgreSQL、ClickHouse、Redis 和 API 数据互相对应。
7. 刷新、断线和 Redis 恢复后页面最终一致。

## 13. 实施顺序与提交边界

1. `docs:` Phase 4 spec 和实施计划。
2. `feat:` Redis/Outbox/统一实时事件基础设施。
3. `feat:` SSE API 和前端实时订阅。
4. `feat:` 不可变 Run Snapshot。
5. `feat:` OpenHands Adapter 与真实接入。
6. `feat:` Rubric 模板、多 Judge 和置信聚合。
7. `feat:` Gold 校准、在线采样和统计显著性。
8. `feat:` 分布式配额、SDK 背压和成本分析。
9. `feat:` Rollup、异常检测和治理页面。
10. `test:` 高并发、故障恢复和完整 E2E。
11. `docs:` 运维、接入和验收报告。

所有实现都在一个独立 Worktree 中完成，但按以上边界分 Commit。每个 Commit 在提交前
运行对应测试，最终必须运行 Go、Python、前端、数据库集成、Playwright、真实
OpenHands 和高并发验证。

## 14. 完成定义

Phase 4 只有在以下证据同时成立时才算完成：

- OpenHands 真实任务通过通用 SDK Adapter 接入并完整结束。
- Run 状态和语义事件通过 SSE 在执行期间到达页面。
- Redis 故障不破坏权威写入，恢复后最终一致。
- 每个新 Run 有不可变、脱敏、可校验的 Snapshot。
- 多 Judge 评测保存独立投票、分歧、置信度和 veto。
- 人工 Gold 校准报告和统计显著性可以从真实数据生成。
- 项目配额在多服务实例下行为一致。
- SDK 背压、TTFT、Token、成本和工具时延均可查询。
- ClickHouse Rollup、TTL 和异常检测有真实数据验证。
- 压测报告包含可重复命令和量化结果。
- 全量自动化测试与真实端到端测试通过。
