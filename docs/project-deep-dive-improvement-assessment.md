# AgentScope 深度调研：完善度、可靠性与演进建议

> **调研性质**：静态代码、迁移脚本、测试与 CI 配置审阅；未修改业务实现，也未对运行中的数据库或外部服务施加故障。本文的“风险”表示由当前实现推导出的失效模式，不等同于已经发生的线上事故。  
> **调研范围**：Go 服务端、PostgreSQL/ClickHouse/Redis 边界、在线评测 Worker、Python SDK、React 前端、测试与部署产物。  
> **结论先行**：项目已经是一个功能闭环完整、工程化明显高于普通作品集的 AgentOps 平台；当前最需要补的不是再堆页面或指标，而是把**在线评测的事实一致性、任务可靠性、评分语义和生产运维闭环**收紧。

---

## 1. 当前项目究竟完成了什么

AgentScope 的主链路已经不是“Trace 看板 + 一个评测页面”，而是一个较完整的双闭环系统：

```text
Agent / SDK
  ├─ 注册、心跳、实例状态
  ├─ Run start / state / finish / fail
  └─ OTLP Trace 上报
          │
          ├─ PostgreSQL：项目、Run、状态事件、评测任务、标注、数据集、实验
          ├─ ClickHouse：Span 与 LLM/Tool/Retrieval 等链路明细
          └─ Redis（可选）：实时事件投影

Run 终态
  └─ online_eval_jobs（PostgreSQL durable queue）
          └─ Sampling Rule
                 └─ TraceAwareEvaluator（执行健康启发式信号，不是语义质量 Judge）
                        ├─ eval_scores
                        └─ 低分/失败 → annotation_items
                                      └─ Gold 标注 → Dataset Case
                                                   └─ 离线实验、Judge、实验对比
```

**已经具备的强项：**

1. **在线与离线链路确实打通。**Run、Trace、在线评测、人工回流、Dataset Case、离线实验/对比不是概念拼装，而是有对应的服务、表和页面。
2. **在线任务已形成 PostgreSQL 权威的可靠闭环。**Run 终态、terminal state event、实例清理与 `online_eval_jobs` 在同一事务中提交；任务有 lease token、周期 stale recovery、死信、人工重试和评分/标注幂等键。
3. **分层存储设计合理。**PostgreSQL 承载业务事实，ClickHouse 承载高吞吐 Trace，Redis 仅作为实时投影而不是事实源。
4. **Trace 采集工程完成度较高。**Go 侧有批写缓冲、限流、健康事件；Python SDK 有 OTLP、批处理、导出指标与脱敏处理。
5. **工程质量基线不差。**Go、Python、Web 都有单测和 CI；CI 会执行 Go race、真实 PostgreSQL/ClickHouse migration、Python 静态检查、前端 lint/test/build/audit。

但从“项目能演示”走向“评测结果可被信任、在线链路在故障下仍可解释”还差关键一段。下文按优先级说明。

---

## 2. 总体评估

### 2.1 成熟度评分（面向生产可靠性，不是功能完成度）

| 维度 | 评分 | 判断 |
|---|---:|---|
| 业务闭环与产品覆盖 | 86/100 | 在线观测、人工回流、离线回归均已覆盖，作品集表达强。 |
| Trace 采集与数据分层 | 80/100 | OTLP + ClickHouse + 批写/限流/健康事件完整，但跨存储最终一致性仍需显式建模。 |
| 在线评测任务可靠性 | 72/100 | 原子终态入队、lease token 与周期恢复已补齐；仍缺队列指标、故障注入 E2E、Trace-ready 协议和抽样规则快照。 |
| 评分有效性与可解释性 | 55/100 | 当前所谓“多维评测”主要是 Trace 启发式，不应与语义质量/LLM Judge 混同。 |
| SDK 生产可用性 | 60/100 | 单进程单 Run 的 demo 使用流畅；并发服务、终态请求失败恢复仍不足。 |
| 测试、契约与交付 | 68/100 | 单测和 CI 基线好，但关键跨存储 E2E、API 契约、部署产物不足。 |
| 运维可观测性 | 48/100 | 缺 readyness、metrics、队列积压/Worker 存活等平台自身可观测性。 |

**综合：约 68/100（生产稳健性视角）。**

这个分数不否定项目价值：若按“秋招独立项目、可运行的 AgentOps 平台”衡量，它明显强于典型 CRUD 项目。扣分主要来自高阶要求：当平台开始承诺“在线自动评测”“自动人工回流”“可靠任务队列”时，用户会自然要求它在重试、故障、延迟和数据不完整情况下仍然正确、可追踪、可解释。

### 2.2 最核心的判断

> 当前短板不是“功能少”，而是**功能扩张速度快于事实一致性、评测语义、跨端契约和运维闭环的收口速度**。

因此后续不建议优先新增第 N 个评测指标或第 N 个 dashboard；先把已存在的在线闭环做成可信系统，项目档次会提升更明显。

---

## 3. P0：优先处理的可靠性问题

> **修复进度（`fix/online-eval-reliability`）**：本节的 P0-1、P0-2、P0-3 已完成第一轮实现与测试；仍建议在后续版本增加 Prometheus 指标、故障注入 E2E 和 Worker supervisor。

P0 表示：一旦发生网络抖动、进程重启、数据库短暂异常或任务超时，可能造成“Run 已完成但没有评测”“旧 Worker 覆盖新 Worker 结果”“任务长期卡住且无人发现”等错误。这些问题直接影响在线评测闭环的可信度。

### P0-1：Run 终态与在线评测入队不是同一原子事务，存在“终态 Run 无评测任务”窗口

- **风险类型**：数据一致性 / Change Propagation
- **债务性质**：accidental
- **优先级**：9 = Pain 3 × Spread 3

#### 修复状态

**已修复。**生产 Runtime adapter 现在把 Run 终态、terminal state event、实例 active-run 清理与 `online_eval_jobs` UPSERT 放进一个 PostgreSQL 事务；HTTP handler 在该保证可用时不再执行第二次非原子入队。

#### 原始症状

SDK 的 `finish` / `fail` 请求先把 Run 写成终态，再单独插入 `online_eval_jobs`。如果二者之间请求取消、连接中断、数据库瞬断或进程退出，Run 已经完成，但评测任务可能永久缺失。

#### 代码依据

- `internal/httpapi/sdk_runtime_handlers.go:83-101`：先调用 `FinishRun`，随后调用 `OnlineEvalJobs.Enqueue`。
- `internal/httpapi/sdk_runtime_handlers.go:125-143`：`FailRun` 走同样的两步。
- `internal/runs/runtime.go:369-392`、`:410` 之后：Run 终态更新和 Run state event 已由 Runtime Service 分别完成；没有携带 online-eval outbox/job 的事务接口。
- `internal/adapter/online_eval_jobs.go:43-50`：入队是独立的 `INSERT ... ON CONFLICT`。

#### 后果

1. 用户看到 Run 是 `completed` / `failed`，但 Run Detail 页面会显示“尚未生成在线评测任务”。
2. 失败 Run 可能不进入人工队列，回流数据集的链路被静默截断。
3. SDK 收到 HTTP 500 后重试虽然大概率能靠 `(project_id, run_id, evaluator_version)` 去重恢复，但如果 SDK 未重试、网络在响应前断开，服务端没有补偿扫描，就会永久丢任务。
4. 后续用“在线评测覆盖率”计算质量时，分母/分子会不可信。

#### 建议

将“Run 转终态 + 创建 online_eval_job（或 outbox event）”收口到同一个 PostgreSQL 事务。

推荐设计：

```text
POST finish/fail
  └─ 事务内：
       1. 更新 runs 状态
       2. 插入 run_state_event
       3. INSERT online_eval_jobs ... ON CONFLICT DO NOTHING
       4. （可选）插入 realtime_outbox
```

若短期不改 Runtime Service 事务边界，至少补一条**补偿任务**：周期扫描“终态 Run 且不存在对应 evaluator_version job”的记录并补入队，同时记录审计事件和指标。

#### 验收标准

- 在 `FinishRun` 成功、`Enqueue` 失败的故障注入下，最终仍只存在一个 Job。
- 对任意终态 Run，能查询到 `online_eval_jobs` 记录或明确的“不适用”记录。
- 有集成测试覆盖：断点发生在两次写入之间、客户端超时、重复 finish/fail、服务重启。

---

### P0-2：在线任务的租约归属没有贯穿终态更新；旧 Worker 可能完成已被重新领取的 Job

- **风险类型**：任务并发一致性 / Dependency Disorder
- **债务性质**：accidental
- **优先级**：9 = Pain 3 × Spread 3

#### 修复状态

**已修复。**`online_eval_jobs` 新增 `lease_token`；Claim 会生成新 token，`Complete`、`MarkSkipped`、`Retry` 都校验 `worker_id + lease_token`。过期恢复会清空旧 token，手工重试也会重置 attempt cycle。

#### 原始症状

任务领取时会写入 `claimed_by`，重试时也校验 `claimed_by`；但 `Complete` 和 `MarkSkipped` 只校验 `status='running'`，不校验“当前提交者仍持有该租约”。当任务超时、旧 Worker 尚未退出、新 Worker 恢复并重新领取任务时，旧 Worker 仍可能把任务标记为成功或跳过。

#### 代码依据

- `internal/adapter/online_eval_jobs.go:55-63`：`Claim` 将 Job 设为 `running` 并写入 `claimed_by`、`claimed_at`。
- `internal/adapter/online_eval_jobs.go:155-161`：`Retry` 有 `AND claimed_by=$6`，体现了租约校验意图。
- `internal/adapter/online_eval_jobs.go:78-86`：`MarkSkipped` 仅为 `WHERE id=$1 AND status='running'`。
- `internal/adapter/online_eval_jobs.go:110-115`：`Complete` 同样仅为 `WHERE id=$1 AND status='running'`。
- `internal/adapter/online_eval_jobs.go:164-169`：恢复 stale claim 后会清空领取信息并重新可领取。

#### 典型失效时序

```text
T0：Worker A Claim job-1，claimed_by=A
T1：A 因 ClickHouse 慢查询 / GC / 网络问题超过 stale lease
T2：新 Worker 启动，RecoverStaleClaims 将 job-1 置回 retry_wait
T3：Worker B Claim job-1，claimed_by=B
T4：A 终于返回，调用 Complete 或 MarkSkipped
T5：由于 SQL 只检查 status=running，A 可以结束 B 正在处理的任务
```

评分表有 `(online_eval_job_id, metric_name, metric_kind)` 唯一索引，能削弱重复写分数的影响；但这不能保证**哪个 Worker 的评分是最终事实**，也不能保证 sampling/annotation 结果与当前有效租约一致。

#### 后果

- stale Worker 的旧 Trace 读结果可能覆盖新 Worker 的结果。
- 任务状态、`sampling_reason`、`fallback_reason` 与真实执行者不一致。
- 故障很难复盘，因为 Job 没有 `lease_version`，也没有完整 transition audit。

#### 建议

1. `Claim` 生成并持久化 `lease_token`（UUID 或递增版本号），Worker 内存中持有该 token。
2. `Complete`、`MarkSkipped`、`Retry` 全部使用：
   ```sql
   WHERE id = $job_id
     AND status = 'running'
     AND claimed_by = $worker_id
     AND lease_token = $lease_token
   ```
3. 若更新行数为 0，返回明确的 `ErrLeaseLost`，不再尝试覆盖或重试该 Job。
4. 写一组真实 PostgreSQL 并发集成测试模拟 A lease 过期、B 接手、A 晚到的情形。

#### 验收标准

同一 Job 的旧 lease 永远无法改变新 lease 的状态、评分、标注项；测试能稳定复现并验证该结论。

---

### P0-3：Worker 的恢复与失败路径会静默退化，任务可能长期卡在 `running`

- **风险类型**：可恢复性 / 运维可观测性
- **债务性质**：accidental
- **优先级**：8 = Pain 3 × Spread 3

#### 修复状态

**已修复第一轮。**Worker 已将 stale claim recovery 改为周期执行；`MarkSkipped`、`Retry` 写库失败会向 `ProcessAvailable` 返回错误并记录日志。

#### 原始症状

Worker 有重试和 stale claim 机制，但部分关键失败被忽略，且 stale 恢复只在 Worker 启动时执行一次。

#### 代码依据

- `internal/onlinerval/worker.go:60-75`：`Run` 只在启动时调用一次 `RecoverStaleClaims`；初始化恢复失败会直接返回，外层仅记录“worker stopped”。
- `internal/app/app.go:231-236`：Worker goroutine 退出后没有 supervisor/restart/backoff，也没有 readiness/告警联动。
- `internal/onlinerval/worker.go:117`：`MarkSkipped` 错误被 `_ =` 忽略。
- `internal/onlinerval/worker.go:157-165`：`retry` 忽略 `repo.Retry` 的返回错误。
- `internal/onlinerval/worker.go:133-135`：`Complete` 出错后再调用 `retry`；若 retry 更新也失败，Job 保留 `running`，服务正常运行时不会再次执行 stale claim recovery。

#### 后果

- 服务启动瞬间 PostgreSQL 短暂不可用，在线评测 Worker 会退出；HTTP 服务仍存活，但所有新 Job 只会堆积。
- 单次数据库异常后 Job 可能卡在 `running`，直到**下次进程重启**才有机会回收。
- 人工在 UI 上只能看到 Job 长时间运行，无法区分“正在评测”“Worker 已死”“租约已丢失”“写库失败”。

#### 建议

1. 将 stale claim recovery 纳入周期循环，例如每 30 秒/每个 poll 周期执行一次，且失败指数退避重试。
2. `process` 需要把 `MarkSkipped`、`Retry` 的失败记录到日志和指标；必要时返回错误给 `ProcessAvailable`。
3. Worker 使用独立 supervisable goroutine：启动失败不断重试或让进程 fail-fast 交给编排平台重启，二者选其一，但不能仅 log。
4. 加入如下指标：
   - `online_eval_jobs{status}` 数量、最老排队时长；
   - Worker last successful poll / last successful completion；
   - retry、dead-letter、lease-recovery、lease-lost 计数；
   - evaluator / ClickHouse / PostgreSQL 分阶段耗时与错误率。

#### 验收标准

- 在 `Complete` 后 `Retry` 写库失败的注入测试下，Job 可在有限时间内再次被恢复处理。
- 数据库在启动时不可用 30 秒后恢复，Worker 能自动恢复，不依赖进程重启。
- `online_eval_queue_oldest_age_seconds` 可以被监控和告警。

---

## 4. P1：在线评测“可信度”仍需要收口

### P1-1：抽样决策在 Worker 执行时才计算，历史 Run 会受“未来规则”影响

- **风险类型**：Domain Model Distortion / Knowledge Duplication
- **债务性质**：accidental
- **优先级**：6 = Pain 2 × Spread 3

#### 症状与依据

- Run 转终态时只创建 Job：`internal/httpapi/sdk_runtime_handlers.go:89-99`。
- Worker 真正执行 `ShouldSample`：`internal/onlinerval/worker.go:107-124`。
- Sampling Rule 是可编辑的：`internal/sampling/service.go:200-213`。
- Job 虽然记录 `sampling_rule_id`、`sampling_reason`，但没有规则快照、规则版本、决策时间对应的规则内容：`internal/onlinerval/jobs.go:39-60`。

如果 Run 已完成但 Job 还在排队，此时用户修改或禁用规则，**同一个历史 Run 的抽样结果会改变**。这会使“为什么这个 Run 被评测/没被评测”的答案取决于 Worker 的调度延迟，而不是 Run 发生时的治理策略。

#### 建议

在 Run 终态事务内完成抽样决策，或至少把规则版本/filters/sample rate/strategy 快照写入 Job；Worker 只执行已经确定的任务。预算计数也应明确发生在决策时还是执行时。

---

### P1-2：`TraceAwareEvaluator` 是“Trace 启发式健康评分”，不是语义质量评测

> **修复状态**：已完成命名、页面提示、score reason 与 Quickstart 文档校准；评测器仍保持低成本的 Trace 启发式职责，语义 Judge 是后续能力扩展。

- **风险类型**：Domain Model Distortion
- **债务性质**：intentional 原型实现，但当前产品命名容易造成误解
- **优先级**：6 = Pain 3 × Spread 2

#### 当前真实能力

`TraceAwareEvaluator` 的实现没有调用 LLM Judge，也没有读取 Gold 参考答案、Rubric、用户反馈或事实知识库。它目前主要根据运行状态、输出是否为空、工具成功比例、是否有 ERROR span、是否检索到文档、是否存在 run error 来生成分数：

| 展示指标 | 当前实现的实际含义 | 代码位置 |
|---|---|---|
| `completion_status` | Run 是否 completed | `internal/onlinerval/evaluator.go:59-75` |
| `final_answer_quality` | completed 且 output 非空即 1.0 | `:78-112` |
| `tool_correctness` | Tool call 成功率 | `:114-154` |
| `trajectory_quality` | 有无 Span ERROR；无 ERROR 固定 0.7 | `:157-195` |
| `rag_groundedness` | 有检索记录且有文档时，假定 groundedness 可接受 | `:197-236` |
| `safety` | Run 是否携带 error | `:238-257` |

因此例如：

- 生成了错误答案但 output 非空的 Run，`final_answer_quality=1.0`；
- 工具都成功调用但任务目标完全跑偏，`tool_correctness=1.0`；
- 检索到任意文档即可能给 `rag_groundedness=0.7`；
- 没抛异常的越权/幻觉回答，`safety=1.0`。

这不是代码错误，而是**指标语义与命名不匹配**。当前实现更准确的定位是：

> 基于 Trace 的运行完整性、执行健康度和链路异常启发式（Trace Heuristic Health Evaluation）。

#### 后果

- 产品界面中“Trace 多维评测”容易被理解为“模型回答质量被自动评估”。
- 当低分自动回流人工队列时，可能漏掉“执行顺利但答案错误”的高价值失败样本。
- 评分拿去做版本回归时，可能优化的是“少报错、少空输出”，而非任务正确性。

#### 建议

**短期：命名与展示诚实化。**

- 前端把 `trace_heuristic` 展示为“Trace 启发式健康评分”，不要泛称“质量评测”。
- 每个指标展示 `signal_type`（execution / trace / semantic / human）与 `evidence_missing`。
- 对无 Tool/Retrieval/Span 的 `0.5`，增加 `not_applicable` / `insufficient_evidence`，避免把缺证据混进数值均值。

**中期：实现分层评测器。**

```text
Layer 1：确定性运行健康规则（当前 TraceAwareEvaluator）
Layer 2：可配置 Rule / 正则 / JSON schema / tool-path evaluator
Layer 3：Rubric + LLM Judge（带 prompt/rubric/model version、token/cost、原始判词）
Layer 4：Gold / 人工标注校准与离线回归
```

在线默认应把 Layer 1 用作筛选与异常发现；高价值抽样再运行 Layer 2/3，并记录 evaluator version 和输入快照。

---

### P1-3：Trace 与在线评测之间没有“Trace 已齐备”的同步协议，易出现过早评分

- **风险类型**：跨存储最终一致性
- **债务性质**：accidental
- **优先级**：6 = Pain 2 × Spread 3

#### 代码依据

- Run 终态 API 可立即入队：`internal/httpapi/sdk_runtime_handlers.go:83-101`。
- Worker 被触发后立即从 ClickHouse 读取 Run Detail：`internal/onlinerval/worker.go:126` → `internal/onlinerval/evaluator.go:31-56`。
- Trace 由独立的 OTLP 请求经过批缓冲写入 ClickHouse；`BufferedTraceWriter` 会按 batch/interval 处理：`internal/ingest/writer_buffer.go:42-44`、`:110-131`、`:166-225`。

SDK 端通常需要先 `force_flush()` 再 `finish_run()`，但服务端没有以“某 Trace 已完整落库”为前提的明确状态；多个进程或 SDK 调用顺序稍有变化，就可能先评测到空 Span/空 ToolCall，后续 Trace 才到。

#### 后果

- 当前 evaluator 对缺失数据返回大量 `0.5`，并可能触发“低分人工回流”。
- 评测结果依赖网络时序；同一 Run 在不同负载下得到不同评分。
- 用户难以区分“Agent 没有调用 Tool”与“Tool Span 还未写到 ClickHouse”。

#### 建议

1. 在 Job 中增加 `trace_ready_at` / `trace_observed_at` / `min_trace_wait` 语义。
2. Worker 在 Trace evidence 不足时不要立刻将其作为业务分数；可返回 `waiting_for_trace` / `retry_wait`，设一个有限的 grace period。
3. 在 OTLP ingest 侧维护按 `(project_id, run_id, trace_id)` 的完整性信号（例如 root span end 或 exporter flush marker），或由 SDK 显式上报 `trace_flushed`。
4. Job 结果必须区分：`no_trace_expected`、`trace_not_arrived_yet`、`trace_missing_after_timeout`，而不是统一给 0.5。

---

### P1-4：人工重试没有重置 attempt 语义，死信 Job 重驱后只剩一次机会

> **修复状态**：已修复。`RetryManually` 现在会清空 claim、清除最后错误并将 `attempt_count` 重置为 0，因此人工重试开启新的 attempt cycle；完整 attempt history 仍是下一阶段可观测性增强项。

- **风险类型**：任务状态模型不完整
- **债务性质**：accidental
- **优先级**：4 = Pain 2 × Spread 2

`RetryManually` 会把 `dead_letter` / `failed` Job 重新设为 `queued`，但没有重置 `attempt_count`：

- `internal/adapter/online_eval_jobs.go:194-202`
- Worker 在 Claim 时先 `attempt_count = attempt_count + 1`：`:55-63`
- Worker 判断 dead letter：`internal/onlinerval/worker.go:157-165`

一个已尝试 5 次、到达 dead letter 的 Job，人工点击重试后会以第 6 次运行；只要再遇到一次临时错误，就会再次 dead letter。

这不是一定错误——也可以定义成“人工重试只给予一次额外尝试”——但当前没有显式字段或 UI 文案表达此策略，因此运维人员很容易误判“重试没有生效”。

**建议：**增加 `manual_retry_count`，或在人工重试时开启新的 attempt cycle；同时保存完整 attempt history，不要仅保留最后一条 `last_error`。

---

## 5. P1：Python SDK 不适合直接承载并发 Agent 服务

### P1-5：一个 Runtime 只有一个 `_current_run`，并发请求会互相覆盖

- **风险类型**：并发模型缺失 / Domain Model Distortion
- **债务性质**：accidental（demo 顺序执行下不显眼）
- **优先级**：6 = Pain 3 × Spread 2

#### 代码依据

- 模块级全局 `_runtime`：`python/agentscope_sdk/runtime.py:279-280`。
- `Runtime` 内只有一个 `_current_run`：`:315`。
- `start_run` 成功后直接 `self._current_run = run`：`:521-527`。
- `finish_run`、`fail_run`、`update_run_status` 都从该共享字段读取 run ID：`:529-625`、`:629-672`。

#### 失效示例

```text
请求 A: start_run() → _current_run = A
请求 B: start_run() → _current_run = B
请求 A: finish_run() → 实际 finish B
请求 B: finish_run() → _current_run 已被 A 清空，失败
```

这会让 Flask/FastAPI、线程池、async worker 等真实 Agent 服务产生错误关联。当前接口返回 `RuntimeRun`，但终态方法并不接受该 RunHandle，所以调用方无法安全地指定“我要结束哪个 Run”。

#### 建议

- 将 `RuntimeRun` 扩展为带 `finish/fail/update` 能力的显式 handle，所有终态操作都传 `run_id`。
- 若保留简化 API，使用 `contextvars.ContextVar` 管理“当前上下文 Run”，并清楚声明线程/协程语义。
- 增加并发测试：两个线程/两个 async task 交错 start/finish，验证 API 请求使用正确 run ID。

---

### P1-6：终态请求一旦网络失败就清除本地 Run，SDK 缺少可靠重试/恢复能力

- **风险类型**：边缘可靠性
- **债务性质**：accidental
- **优先级**：6 = Pain 3 × Spread 2

`finish_run` 与 `fail_run` 无论请求是否成功，都会在 `finally` 清空 `_current_run`：

- `python/agentscope_sdk/runtime.py:611-625`
- `python/agentscope_sdk/runtime.py:659-672`

这意味着“finish 请求超时但服务端可能已经完成”与“finish 请求根本没有送达”被同样处理；调用方只得到 `False`，却失去 `run_id` 和可重试状态。配合 P0-1，这会直接增加“终态 Run 无评测任务”的概率。

**建议：**

1. 为 terminal request 提供幂等键、有限重试、明确的 `unknown delivery` 状态。
2. 失败时保留 RunHandle，由调用方或后台 retry spool 重试。
3. 记录 SDK 本地 delivery metrics，而不是只写 warning 日志。
4. 文档明确要求：`force_flush()` 完成后再发送 finish，并在退出前 `shutdown_runtime()` + `shutdown_tracing()`。

---

## 6. P2：架构、测试、运维与产品体验债务

### P2-1：在线评测正处于 Dispatcher → Durable Worker 迁移期，兼容双路径仍增加认知和测试负担

- 默认生产 Runtime 已走“终态事务 → `online_eval_jobs` → Worker”路径；`internal/httpapi/sdk_runtime_handlers.go` 只在轻量测试或旧 wiring 不声明原子保证时，才 fallback 到 `OnlineEvalJobs.Enqueue` 或旧 Dispatcher goroutine。
- `internal/app/app.go` 仍创建旧 `OnlineEvalDisp` 以兼容既有接口与测试，同时启动 durable Worker。
- Worker 已有内存分支测试，并新增 PostgreSQL 集成测试覆盖成功/失败终态原子入队、stale lease 拒绝与 stale completion 回滚；旧 Dispatcher 仍保留较多独立测试。

这不是当前生产路径的立即 bug，但兼容层仍会造成两套实现语义：旧路径逐条写 score、异步 goroutine；新路径事务化完成、可重试、可查 Job 状态。

**建议：**明确迁移完成条件：

1. 所有生产依赖一律要求 `OnlineEvalJobs`；
2. Handler fallback 仅保留在测试 helper 中，或删除；
3. Dispatcher 标注 deprecated，迁移其必要测试到 Worker + PostgreSQL 集成测试；
4. 统一文档、页面文案、错误码与运行图。

---

### P2-2：关键在线闭环缺少真实存储集成/E2E 回归

`Worker` 的内存 fake repository 测试覆盖正常完成、未抽样、重试/死信和恢复错误上浮。新补充的 `OnlineEvalJobRepository` / Runtime PostgreSQL 集成测试已验证：

- completed / failed Run 的 terminal transaction 会同时写入 state event 与 Job；
- stale lease 不能 Skip Job；
- stale Worker 的 Complete 会整体回滚 score、annotation 与 Job side effect。

但仍缺少以下真实存储或端到端验证：

- `FOR UPDATE SKIP LOCKED` 的多 Worker 并发领取；
- unique index 下的重复 finish/fail / 幂等重放；
- manual retry 的完整 attempt-history 审计；
- Handler 入队 → Worker 完成 → Annotation → Promote → Dataset → Experiment 的完整闭环。

前端 Playwright E2E 只有 `web/e2e/phase4-agentops.spec.ts`，且依赖外部生成的 `.external/phase4-agentops-result.json`；主 CI 并不运行该 E2E。

**建议：**建设一个可在 CI 执行的 Docker Compose E2E 场景，至少覆盖：

```text
OTLP ingest → SDK finish → online_eval_jobs
→ trace ready / evaluator → eval_scores
→ low score → annotation item → Gold → Dataset case
→ offline experiment → comparison
```

再增加故障矩阵：ClickHouse 慢/不可用、PostgreSQL 短暂不可用、Worker 被 SIGTERM、重复 finish、重复 OTLP、Sampling Rule 修改。

---

### P2-3：没有 API 契约源，Go DTO、TS types、SDK payload 靠人工同步

现状：

- 未发现 OpenAPI/Swagger/proto 契约文件；
- `web/src/api/types.ts` 为 996 行，`web/src/api/hooks.ts` 为 939 行；
- `internal/httpapi/handlers.go` 1,334 行，包含大量 HTTP DTO/interface；
- Router 已有接近百条 HTTP 路由。

随着 online job 状态、sampling strategy、metric kind、token scopes、snapshot schema 继续演进，前后端与 Python SDK 很容易发生“字段名正确但语义不一致”的漂移。

**建议：**

- 中期引入 OpenAPI 作为 HTTP 契约源，生成 TypeScript client/types，并做 schema compatibility check；
- 或至少先为关键 DTO 建立 JSON schema + contract tests；
- 对状态枚举采用单一共享定义/代码生成，避免 Go、TS、Python、SQL migration 各自手写。

---

### P2-4：平台自身没有足够的运行态可观测性和 readiness

- `/healthz` 固定返回 `{"ok": true}`：`internal/httpapi/router.go:163-165`；不检查 PostgreSQL、ClickHouse、Redis、在线 Worker 或队列堆积。
- Worker 停止仅打印日志：`internal/app/app.go:232-235`。
- Trace buffer 和 health buffer 的统计主要在关闭时记录：`internal/app/app.go:365-389`。
- 未发现 `/metrics` / Prometheus exporter，也未发现 Dockerfile、Helm/Kubernetes/Terraform 等生产部署产物。

**建议：**

1. 区分 `/livez` 与 `/readyz`；readyz 至少检查 PG、ClickHouse 与关键 background worker 最近一次成功心跳。
2. 增加 `/metrics`：ingest queue、drop、flush error、online job backlog、oldest age、retry/dead-letter、realtime outbox backlog。
3. 将实时 outbox、在线评测、批写组件的状态放入 Ingest/Operations 页面，而不是只在关机日志里出现。
4. 补最小 Dockerfile + production compose，明确环境变量、migration、graceful shutdown、资源限制。

---

### P2-5：实时 outbox 是 at-least-once 投影，但缺少业务层事件覆盖和重试治理可见性

Realtime Relay 使用 PostgreSQL outbox、`SKIP LOCKED`、30 秒 claim timeout，方向正确：

- `internal/realtime/relay.go:53-94`
- `internal/store/realtime.sql.go:14-29`

但 `Relay.Run` 忽略 `RunOnce` 的结果和错误（`_, _ = r.RunOnce(ctx)`），没有暴露 retry/backlog/last_error 的产品或指标视图；此外需要明确在线 Job 状态、评分完成、annotation 创建等是否都写入 outbox。若没有，用户会看到“Run 已完成”却不知道评测进入何种状态，只能依赖前端轮询。

**建议：**为每个用户可见的异步状态定义事件契约，并把 relay 错误、attempt、延迟、堆积暴露到 metrics 和运营页面。

---

### P2-6：HTTP/Adapter 层成为横向中心，新增功能的改动面偏大

`internal/httpapi` 和 `internal/adapter` 承担大量跨领域拼装工作：

- `internal/httpapi/handlers.go` 有 36 个左右依赖字段和大量 interface；
- `internal/adapter` 导入多个领域服务并向 HTTP DTO 拼装；
- 新功能常同时修改 domain service、adapter、handler、router、TS types、hooks、page。

这并不等同于“Clean Architecture 做错了”——adapter 本来就应隔离基础设施。但当前边界已接近超级胶水层，未来继续加治理、告警、模型评测时，change propagation 会越来越明显。

**建议：**以业务能力拆 endpoint module 和 application facade：例如 `online-evaluation`, `annotation-feedback`, `experiment-reporting`, `runtime-observability`。每个能力拥有 request/response DTO、service 接口与契约测试；`Dependencies` 只注入较粗粒度的 facade。

---

## 7. 文档与产品表达需要立即校准

### 7.1 在线评测 Quickstart 已落后于实现

本轮已同步更新 `docs/online-eval-annotation-demo.md`：

- 以 `online_eval_jobs + Worker` 替换旧 Dispatcher/goroutine 架构图；
- 修正最小 token Scope，包含 SDK `init()` 所需的 `agent:register`；
- 给出 `configure_tracing()`、同 trace_id、语义 Tool Span、`force_flush()` 与终态 Run 的正确顺序；
- 明确没有 Sampling Rule 时 Job 会 `skipped`；
- 清楚区分“Trace 启发式运行健康评分”与“Rubric + LLM Judge / 人工 Gold”的语义边界。

**建议直接新增一份 10 分钟可验证的 Quickstart：**

```text
创建项目和 token（列出最小 scopes）
→ 创建 100% failure sampling rule
→ configure tracing
→ init runtime
→ start run + 输出一个 tool/error span
→ force_flush
→ finish/fail
→ 查询 online job 状态
→ 查看 score 的 evidence/fallback
→ 查看 annotation item
→ Gold → Dataset → offline experiment
```

每一步写出预期 PostgreSQL/ClickHouse/UI 证据和常见失败排查。

### 7.2 前端需要把“没评测”的原因可视化

Run Detail 已展示在线 Job 状态和 fallback 信息，这是优点（`web/src/features/runs/RunDetailPage.tsx:512-532`）。还应进一步区分：

| 用户看到的状态 | 真正含义 | 建议 UI 文案 |
|---|---|---|
| 无 Job | 终态未入队、未走 SDK 终态 API，或历史数据 | “未生成任务；查看原因/补偿入队” |
| `queued` / `retry_wait` | 正常排队或暂时失败 | 显示等待时长、下次执行时间、last error |
| `running` 很久 | 正常执行、Worker 丢失或 lease 异常 | 显示 claimed_at、worker、lease 状态 |
| `skipped` | 未被 sampling 选中 | 展示规则快照和抽样 hash/budget 原因 |
| `succeeded + baseline_fallback` | 没有得到 Trace 评分 | “仅完成状态基线；不代表回答质量” |
| `succeeded + trace_heuristic` | 运行健康启发式 | “不等同于语义正确性/安全审查” |
| `dead_letter` | 自动重试耗尽 | 显示完整 attempt history 与人工重试后策略 |

---

## 8. 建议的演进路线图

### 第一阶段：让在线任务“不会静默丢、不会越权提交”（1–2 周）

1. Run 终态与 Job 入队合并事务，或建立补偿扫描。
2. 给 Job 引入 lease token，完善 `Complete/Skip/Retry` 的归属校验。
3. stale recovery 周期化；Worker 失败可自动恢复。
4. 补 PostgreSQL 并发集成测试和 job 状态指标。
5. 修正 Quickstart 与页面文案。

**完成后的收益：**可自信地说“线上 Run 终态可靠入队、任务可重试、死信可恢复、人工回流幂等”。

### 第二阶段：让分数“可解释且不误导”（2–3 周）

1. 明确 `trace_heuristic`、`rule`、`llm_judge`、`human_gold` 的评测来源。
2. 将 `0.5` 缺证据改为 evidence state，不混入平均分。
3. 抽样策略和 evaluator/rubric/version 入 Job 快照。
4. 增加在线 Rule evaluator / Rubric + LLM Judge，并保存 prompt、model、token、成本、判词、版本。
5. 建立 Gold 对在线 Judge 的校准报表（precision/recall、agreement、drift）。

**完成后的收益：**在线评测不只是“看起来有六个分数”，而是有明确证据边界、可以与人工 Gold 对账的质量体系。

### 第三阶段：让系统“可部署、可监控、可持续演进”（持续）

1. `/livez`、`/readyz`、Prometheus metrics、告警规则。
2. Dockerfile / compose production 版、migration job、graceful shutdown 说明。
3. OpenAPI 或 schema contract tests，生成 TS client。
4. Compose E2E 纳入 CI，覆盖在线到离线的主链路及故障注入。
5. 将 HTTP/adapter 按业务 capability 逐步拆 facade，减少改动传播。

---

## 9. 面试/简历表述边界

当前项目最强、且可以稳妥表达的内容是：

- **高吞吐 OTLP Trace 采集和分层存储；**
- **Run 生命周期与 Trace 关联；**
- **PostgreSQL 持久在线评测任务、采样、重试/死信与人工标注回流；**
- **离线 Dataset / Rule / Rubric + LLM Judge / 多指标实验对比；**
- **线上失败样本回灌 Dataset，形成持续回归闭环。**

需要注意措辞：

- 可以写“**基于 Trace 的多维启发式在线评测**”；
- 不宜把当前在线 `TraceAwareEvaluator` 描述为“LLM 自动判断答案质量/安全性”，因为其实现尚未具备该能力；
- “可靠队列”可写，但更严谨的是“实现 PostgreSQL 持久任务、重试、死信与幂等落库”；在完成 P0 前，不建议绝对化写成“端到端不丢失”。

这不是削弱亮点，反而体现候选人知道系统边界：**你不仅能做功能，也能解释哪些质量信号是确定性事实，哪些只是启发式，哪些仍需要人工或 LLM Judge 校准。**

---

## 10. 调研结论

AgentScope 已经完成了一个很难得的 AgentOps 主闭环：**采集 → 观测 → 抽样 → 在线筛选 → 人工回流 → 数据集 → 离线回归**。项目下一阶段最有价值的工作不是扩模块，而是完成三个“收口”：

1. **事实收口：**Run 终态、Trace 到达、Job、Score、Annotation 的跨存储一致性与故障恢复；
2. **语义收口：**把 Trace 健康信号、规则评分、LLM Judge、人工 Gold 清楚地区分与校准；
3. **工程收口：**用契约、E2E、metrics、readyz、部署资产保证系统在真实运行环境中可验证。

做到这些后，这个项目的叙事会从“功能丰富的 AgentOps Demo”升级为“对可观测、评测可靠性和数据闭环有系统理解的工程项目”。

---

## 12. 2026-07-29：认证与实时投影可靠性收口

本节记录上述静态调研后已落地的三项修复，避免把“已识别风险”继续误读为当前实现
状态。它们不改变 PostgreSQL 是业务事实源的边界，而是补齐入口资源保护、Redis 不可用
时的实时读取路径，以及 outbox 的生命周期。

### 12.1 登录/注册：从无限 bcrypt 工作量改为本机 admission control

`internal/auth/admission.go` 在调用 `AuthService.Login/Register` 前执行两层 token bucket：
每个路由分别按**粗粒度 IP 前缀**与**规范化邮箱的 SHA-256 哈希**限流。控制器还用非阻塞
semaphore 限制同时进行的 bcrypt 操作，且 bucket 表有最大条目数与淘汰，避免攻击者用
随机 IP/邮箱制造无界内存增长。

- 默认策略：登录 IP 10 burst / 30 次每分钟，登录 principal 5 burst / 8 次每分钟；注册更严格；
- 命中策略或并发闸门时，HTTP 返回 `429`、`Retry-After` 和不泄露具体维度的
  `auth_rate_limited` / `auth_busy`；
- 此控制器刻意保持**本机、无 Redis 依赖**，因此 Redis 故障时仍能保护 bcrypt；
- 已覆盖邮箱不以明文进入 bucket key、IP/principal 分层、时间 refill、并发满载，以及
  HTTP 登录/注册入口的 429 行为。

**仍需明确的边界：**这是每个进程独立的 admission control，不是跨副本的全局账号锁定、
IP 信誉、CAPTCHA 或 WAF 替代品。若以后部署多个公开实例，需要在反向代理/WAF 或共享
存储层叠加全局防护，而不能误称它为分布式暴力破解防护。

### 12.2 Redis 故障：SSE 从“仅 transport”收口为 durable outbox replay

`realtime_outbox.id` 被定义为唯一的公开 SSE cursor，HTTP 响应不再把 Redis Stream ID
暴露给浏览器。`HybridSubscriber` 对首次连接优先使用 Redis；出现 Redis 建连失败、运行期
流错误/关闭时切到 `PostgresSubscriber`。带 `Last-Event-ID` 的重连直接从 PostgreSQL 按
outbox ID 重放，避免依赖已过期或不可转换的 Redis cursor。

- PostgreSQL fallback 默认每秒轮询、每批读取 100 条；
- 当 Redis 不部署时，`AGENTSCOPE_REALTIME_ENABLED=true` 可显式启用 PostgreSQL-only SSE；
  未设置该变量时保留“配置 Redis 即启用 realtime”的兼容行为；
- 保留窗口前的游标显式发送 `reset(cursor_expired)`，旧 Redis cursor 格式发送
  `reset(cursor_format_unsupported)`；现有 Web 客户端按 outbox 事件 ID 去重，收到 reset
  会 invalidate REST 查询并以无游标的新 EventSource 重连；
- fallback 读取已提交但尚未标记 Redis published 的 outbox 行，保证 Redis 中断时不因
  relay 延迟漏掉业务事件。结果是明确的 **at-least-once**，而不是 exactly-once。

**仍需明确的边界：**PostgreSQL fallback 以可用性换延迟和数据库读负载；它不是零延迟
替代 Redis。默认 1 秒 poll 以及 7 天 cursor 保留期均应按真实事件量、PostgreSQL 容量和
用户恢复窗口评估。

### 12.3 Outbox retention：已发布行被有界、并发安全地清理

新增迁移 `00020_realtime_outbox_retention.sql`，为 `published_at IS NOT NULL` 建立局部索引。
`OutboxJanitor` 启动时及后续每小时执行清理；SQL 用按 `(published_at, id)` 排序的候选集、
`FOR UPDATE SKIP LOCKED` 与 batch limit 进行删除。这样多实例即使同时触发，也不会双删
同一行；未发布/待 relay 的行不满足删除条件，因此不会因 retention 丢失待投递事件。

默认值为：保留 7 天、每小时运行、每批 1000 条，可由
`AGENTSCOPE_REALTIME_OUTBOX_RETENTION`、`...CLEANUP_INTERVAL` 和
`...CLEANUP_BATCH_SIZE` 调整。单元测试覆盖了批量 drain 和错误返回；真实 PostgreSQL
隔离 schema 测试应用 `00001`、`00008`、`00020` 后，验证了 durable reader 的 project
隔离顺序、latest/oldest cursor、每批 limit、删除旧 published 行，以及保留新 published 和
旧 pending 行。
