# Phase 4 验证报告

## 1. 范围与结论

本报告对应
`docs/superpowers/specs/2026-07-24-phase4-realtime-evaluation-governance-design.md`
第 14 节完成定义，验证时间为 2026-07-24。

用于真实 E2E 的资源：

- Project：`3a9458c3-3805-45f2-a394-a67b7c1fc96c`
- OpenHands Run：`f1fe584f-a569-4681-b958-76f5b1943a80`
- Trace：`de91fafabd532c2e66199ae60e4fa0df`
- Snapshot：`93ab9ca0-c494-42c2-910c-73a753ff912b`
- Candidate Experiment：`018af50a-545e-4ea5-9a05-a5810d257a4c`
- Comparison：`0db832b9-805b-404e-b666-c24e75f78b57`

该资源由 `make demo-phase4-agentops` 使用真实 OpenHands 和真实模型生成。最终强校验、
Playwright、容量与恢复测试均通过。验收查询只使用这些精确 ID，不借用库中其他 Demo
数据。

## 2. 完成定义证据

| 要求 | 实现与自动化证据 | 真实证据 | 结果 |
|---|---|---|---|
| OpenHands 真实任务通过通用 Adapter 完整结束 | `python/agentscope_sdk/adapters/openhands.py`；`test_openhands_adapter.py` 7 项 | 固定上游提交 `a375c66a...`；Run 为 `completed`；上游源码 `git status --porcelain` 为空 | 通过 |
| Run 状态和语义事件在执行期间通过 SSE 到达 | `internal/realtime/`、`ingest_handlers.go`、`realtime_handlers.go`、`useProjectRealtime`；存储后发布、Cursor/reset/去重测试 | Run 尚为 `running` 时收到语义事件；真实 OpenHands 共 16 条 `run.semantic.appended`，之后才收到 `run.completed` | 通过 |
| Redis 故障不破坏权威写入，恢复后最终一致 | PostgreSQL Outbox + Relay；`TestPostgresOutboxToRedisStream` | 暂停 Redis 时 OTLP/Runtime 继续写入；恢复后 Outbox 在 `3585ms` 清空 | 通过 |
| 每个新 Run 有不可变、脱敏、可校验 Snapshot | `internal/snapshots/`；`TestCanonicalizeRedactsSecretsRecursively`；`TestRuntimeServiceStartRunStoresRedactedSnapshot` | Snapshot 存在，SHA-256 为 64 位，结果哈希与 API/数据库一致 | 通过 |
| 多 Judge 保存独立投票、分歧、置信度和 veto | `internal/evals/governance.go`、`python/agentscope_sdk/multi_judges.py` | 3 用例 × 2 Judge 共 6 票，3 条聚合记录均包含置信证据 | 通过 |
| 人工 Gold 校准和统计显著性可从真实数据生成 | `internal/evals/calibration.go`、`internal/experiments/compare.go`；校准和对比测试 | Gold、Calibration、Baseline/Candidate 和 Comparison 均按项目关联 | 通过 |
| 项目配额在多服务实例下一致 | Redis Lua 原子配额；`TestRedisLimiterRealRedisSharesLimitAcrossInstances`、`TestRedisLimiterTokenBucketDoesNotOversubscribeConcurrently` | Demo 创建项目配额并查询用量；容量流量无超卖或错误 | 通过 |
| SDK 背压、TTFT、Token、成本和工具时延可查询 | SDK export metrics、OpenHands metrics correlation、ClickHouse 结构化提取测试 | 5 条 LLM 均有正耗时、Token 和估算成本；1 条 TTFT；5 条工具；背压丢弃计数 `3960` | 通过 |
| ClickHouse Rollup、TTL 和异常检测有真实数据 | `00005_phase4_performance_governance.sql`；异常 Service/Handler 测试 | 11 条小时 Rollup、3 条背压健康事件、1 条运行延迟异常 | 通过 |
| 压测报告可重复且包含量化结果 | `cmd/agentscope-loadtest`、`scripts/test-phase4-load.sh` | 50 请求、500 spans、10.78 req/s、P95 472.27ms、错误率 0、丢弃率 0 | 通过 |
| 全量自动化与真实 E2E 通过 | Go/Python/Vitest/Playwright 全量命令 | PostgreSQL、ClickHouse、Redis、OpenHands 和桌面/移动页面互相对应 | 通过 |

## 3. 自动化验证

| 命令 | 结果 |
|---|---|
| `go vet ./...` | 通过 |
| `go test -race ./...` | 全部 Go 包通过，无竞态 |
| `cd python && python -m pytest` | `385 passed` |
| `cd web && npm test -- --run` | `20` 个文件、`132` 项通过 |
| `cd web && npm run lint` | TypeScript 无错误 |
| `cd web && npm run build` | Vite 8 生产构建通过 |
| `cd web && npm audit --omit=dev` | `0 vulnerabilities` |
| `cd web && npm audit` | `0 vulnerabilities` |
| `cd web && npm run test:e2e` | 桌面和移动端 `2 passed` |

前端安全审计期间发现旧 React Router、Vite 和 Vitest 存在已公开漏洞，因此升级为
React Router `7.18.1`、Vite `8.1.5`、Vitest `4.1.10`，升级后重新完成了上述全部
前端验证。

## 4. 真实数据校验

执行命令：

```bash
AGENTSCOPE_POSTGRES_DATABASE=agentscope_phase4_test \
AGENTSCOPE_CLICKHOUSE_DATABASE=agentscope_traces \
scripts/verify-phase4-data.sh \
  --result-file .external/phase4-agentops-result.json
```

### PostgreSQL

- Run 状态为 `completed`，`run_id`、`trace_id` 和 `project_id` 一致；
- 有 2 条状态历史、不可变 Snapshot 和 16 条已发布实时语义事件，待发布 Outbox 为 0；
- Rubric 模板与已发布只读版本存在；
- 双 Judge 投票、聚合、人工 Gold 和校准报告完整；
- 实验对比包含显著性计算；
- 项目配额策略和运行延迟异常记录存在。

### ClickHouse

- `trace_spans=19`
- `trace_thoughts=5`
- `trace_llm_calls=5`
- `trace_tool_calls=5`
- `trace_final_answers=1`
- 5 条 LLM 记录均有模型、正耗时、输入/输出 Token 和正估算成本；
- TTFT 记录 1 条；
- 小时成本性能 Rollup 11 条；
- SDK 背压健康事件 3 条。

### Redis 与上游

- 项目 Redis Stream 包含精确 OpenHands Run；
- Outbox 已全部发布；
- OpenHands HEAD 为固定提交；
- OpenHands 上游源码没有本地修改。

## 5. 容量与故障恢复

`make phase4-load-smoke` 结果：

| 指标 | 数值 |
|---|---:|
| 请求 / Span | 50 / 500 |
| 并发 / Batch | 4 / 10 |
| 吞吐 | 10.78 req/s；107.81 spans/s |
| P50 / P95 / P99 | 297.35 / 472.27 / 473.91 ms |
| SSE 首事件 / 事件 P95 | 468.45 / 263.49 ms |
| 错误率 / 丢弃率 | 0 / 0 |

`make phase4-recovery-test` 依次验证：

1. Redis 暂停期间 PostgreSQL/ClickHouse 权威写入继续成功；
2. Redis 恢复后 Outbox 在 `3585ms` 内清空；
3. SSE 使用过期 Cursor 重连后收到 `cursor_expired` reset；
4. ClickHouse 暂停时采集路径返回可诊断失败；
5. SDK 队列饱和时导出丢弃指标，相关 4 项测试通过。

## 6. 浏览器证据

Playwright 使用同一 Demo 账号，拒绝任何 API 403/500，并检查页面横向溢出。桌面
`1440x900` 和移动端 `390x844` 均通过。

截图位于忽略提交的 `web/test-results/phase4/`：

- `01-agents.png`
- `02-openhands-run.png`
- `03-multi-judge.png`
- `04-comparison.png`
- `05-ingest-governance.png`

每个文件分别有 desktop-chromium 和 mobile-chromium 版本。

## 7. 架构与安全审计

- `internal/httpapi` 未发现直接数据库 SQL，Handler 只处理鉴权、参数和响应；
- 跨项目 Run、Agent、SSE、异常和线上评测均有拒绝测试；
- Project Token 仅返回一次明文，数据库只保存哈希和前缀；
- LLM Key 使用环境变量名引用，Demo 结果不保存 Key 或 Project Token；
- Snapshot 递归脱敏 `api_key`、`Authorization`、Cookie 等字段；
- 实时事件使用低敏感 envelope，Redis 不是业务事实源；
- OpenHands 使用公开 callback/metrics，未修改上游源码；
- PostgreSQL、ClickHouse 和 Redis 的职责符合设计边界。

## 8. 非阻断项与外部限制

- Vite 构建产物主 JS 约 `1.34MB`，构建成功但提示可继续按路由拆包；
- 测试和浏览器控制台仍有 Ant Design 旧属性弃用提示及少量测试 `act(...)` 警告；
- 第一次重复执行使用的 OpenCode Go 账户月度额度已用尽；随后使用已有的 DeepSeek
  官方配置从零生成了本报告列出的新资源，真实 OpenHands、16 条实时语义事件和完整
  治理闭环均通过。统一 Demo 不会在供应商失败时回退到伪造结果。

## 9. Phase 4 与质量优化合并后复验

2026-07-24 将 Phase 4 分支与全仓质量优化分支合并后，再次使用空 PostgreSQL、
ClickHouse 和 Redis 环境执行完整复验。该轮结果覆盖并替代本报告前文中较早的测试
数量和容量数字，但前文资源仍保留为一次独立验收记录。

合并后资源：

- Project：`e4454f5d-c6c6-46df-9a6b-fd282f7885d3`
- OpenHands Run：`a45285e3-55f7-438c-b8c2-8476f55316b1`
- Trace：`5bc463fd8d939deafdb801d759ee7409`
- Snapshot：`3e03566a-f169-4098-9ede-aa815d0d711e`
- Candidate Experiment：`a71b7c55-23db-49f3-a75b-a3a40516ebf2`
- Comparison：`06d82e65-ad4c-404b-bf8d-6c8de2928a0f`
- Calibration：`acd59184-a1a3-42a8-a11a-fdcd725088f1`

合并后自动化结果：

| 检查 | 结果 |
|---|---|
| PostgreSQL / ClickHouse migration | 12 / 5 个迁移从空库成功应用 |
| Go 真实数据库竞态测试 | `go test -count=1 -race -p 1 ./...` 通过 |
| Python | `453 passed`，Ruff、format、mypy、pip-audit 通过 |
| Web | 20 个文件、135 项通过，TypeScript、Vite build、npm audit 通过 |
| OpenClaw | TypeScript 检查和 9 项测试通过，npm audit 通过 |
| Go 依赖 | 0 个可达漏洞；1 个依赖模块提示不在可达调用路径 |
| Playwright | 1440×900 与 390×844 均通过，无 API 403/500 和页面溢出 |

真实 OpenHands 仍使用固定上游提交 `a375c66a...`，未修改源码。该 Run 产生 19 个
Span、5 条 Thought、5 条 LLM、5 条 Tool 和 1 条 Final Answer；6 条 Judge Vote、
3 条聚合、3 条 Gold、10 条小时 Rollup 均按精确 ID 校验。SDK 在刻意施加背压时记录
3928 条丢弃 Span，健康事件成功入库。

独立容量烟测为 50 请求、500 Span、P95 `685.53ms`、错误率 0、丢弃率 0。恢复测试中
Redis 故障不影响权威写入，恢复后 Outbox 在 `1830ms` 内排空；SSE Cursor 恢复、
ClickHouse 故障暴露和 SDK 队列饱和测试均通过。

## 10. Project Token 三阶段治理复验（2026-07-28）

本轮在独立 worktree 中实现并验证了 Project Token 的完整治理闭环，覆盖安全基线、
生命周期审计和项目级生产策略三阶段；PostgreSQL 仍是 Token 生命周期、权限和审计的
权威来源，Redis 未承载任何撤销或授权事实。

### 已实现能力

- **阶段一：安全基线。** owner/admin 才能创建、查看、撤销和查询审计；Token 明文只在
  创建或轮换响应中返回一次。撤销和过期状态在每次鉴权中同步检查，旧 Token 会立即被拒绝。
- **阶段二：轮换与审计。** 新 Token 使用 `rotates_token_id` 关联旧 Token；旧 Token 不会自动
  失效。鉴权成功后的最近使用信息经 250ms 本地合并窗口异步写入 PostgreSQL，并在服务关闭时
  drain。创建、撤销、轮换发起和轮换完成均写入不包含明文或完整哈希的审计事件。
- **阶段三：策略与风险。** 支持 environment/token type、永不过期限制、production/CI TTL
  上限、到期和闲置提醒阈值，以及风险摘要 API。控制台提供最小权限模板、筛选、轮换首次使用
  门禁、审计抽屉和策略编辑区。

### 数据库与真实 API 验收

在本地 PostgreSQL `agentscope_test` 上实际应用了以下迁移：

```text
00014_project_token_governance.sql
00015_project_token_audit.sql
00016_project_token_policy.sql
```

随后通过公开 API 完成“创建 runner Token → 使用旧/新 Token 访问 Runner → 轮换 → 等待最近使用
聚合 → 撤销旧 Token → 查询审计/风险”的真实流程。结果如下：

| 检查 | 结果 |
|---|---|
| Token 列表 | 2 条；响应不包含任一明文 Token |
| Runner 鉴权（旧/新，撤销前） | 均通过鉴权并返回 `404 no_jobs`，而非认证失败 |
| 撤销 | `200`；旧 Token 随后被拒绝，新 Token 保持可用 |
| PostgreSQL 生命周期核验 | `2` Token，`1` 已撤销，`2` 有 `last_used_at`，`1` 有轮换关联 |
| 审计与风险 | 审计事件不含 `ascope_` 明文；风险摘要字段完整 |

### 自动化与浏览器验收

| 检查 | 结果 |
|---|---|
| `go test ./...` | 全部通过 |
| `go vet ./...` | 通过 |
| `go test -race ./internal/projects ./internal/httpapi ./internal/adapter ./internal/app` | 通过 |
| `cd web && npm test -- --no-file-parallelism` | 22 个文件、146 项通过 |
| `cd web && npm run lint && npm run build` | 通过 |
| Token 治理 Playwright 真实页面校验 | 1440×900 与 390×844 均渲染成功、无文档横向溢出、无新的控制台错误 |

仓库原有 `npm run test:e2e` 需要 `.external/phase4-agentops-result.json` 作为外部 Phase 4
Demo 输入；该隔离 worktree 未携带该忽略文件，因此该命令在读取 fixture 前失败，和本轮 Token
治理代码无关。上述 Token 页面浏览器验收使用真实后端、真实登录和公开 API 完成，替代验证了
本轮新增页面的桌面与移动端布局。

## 11. P0 会话失效与在线评测可靠性复验（2026-07-28）

本轮在独立 worktree 完成并复验了可撤销服务端 Session 与 PostgreSQL 权威的在线评测
Job/Worker。Redis 没有承担会话撤销、Job 状态、重试或人工回流的任何事实。

### 已实现能力

- **服务端 Session。** 新增 `user_sessions`；浏览器 Cookie 使用 v2 签名的不透明
  `session_id + random secret` payload，数据库仅保存 32-byte SHA-256 hash。支持当前设备退出、
  退出全部设备、设备列表和移除其他设备；生产环境强制 `Secure Cookie`。
- **可靠在线评测。** 终态 Run 同步写入 `online_eval_jobs`，Worker 用 PostgreSQL
  `FOR UPDATE SKIP LOCKED` 领取任务，启动时恢复超时 claim；采样结果、Trace 评测或
  baseline fallback、重试/死信、评分写入和人工标注回流均可查询。
- **幂等闭环。** `eval_scores.online_eval_job_id` 与 `annotation_items.online_eval_job_id`
  建立唯一关联；同一 Job 重试不会重复写入同指标评分或重复创建自动标注项。
- **控制台。** 用户菜单新增“登录设备”；Run 详情新增在线评测任务状态，明确展示未抽样、
  Trace 多维评测、baseline fallback、重试/死信和人工回流状态。生产静态资源路由修复，
  不再将 `/assets/*` 重写为 `index.html`。

### 真实存储/API/浏览器证据

在隔离 `agentscope_test` PostgreSQL 上从空库应用 `00001` 至 `00018`。公开 API 实际完成：

1. 注册并创建项目、`agent:runtime` Token 与 100% 采样规则；
2. 完成 Run 后立即查询到 `queued` Job，Worker 随后写入 `succeeded`、采样规则 ID 和
   `trace_heuristic` 来源；
3. 失败 Run 自动写入 6 条 `eval_scores`，并创建 1 条 `pending annotation_item`，其
   `online_eval_job_id` 与 Job 的 `annotation_item_id` 互相可追溯；
4. 执行 logout 后旧 Cookie 请求返回 `401`，数据库 `user_sessions.revoked_reason=logout`，
   `secret_hash` 长度为 32 bytes，未保存明文 secret；
5. Playwright 浏览器实际登录，进入“登录设备”页并打开失败 Run 详情，确认显示
   “Trace 多维评测”和“已入标注队列”。

### 自动化门禁

| 检查 | 结果 |
|---|---|
| `go test ./internal/...` | 通过 |
| `cd web && npm run lint` | 通过 |
| `cd web && npm test -- --no-file-parallelism` | 22 个文件、146 项通过 |
| `cd web && npm run build` | 通过 |
| `gofmt` | 本轮 Go 改动通过；仓库既有 `internal/adapter/promote_test.go` 在本轮之前已不符合全仓 fmt 检查 |
