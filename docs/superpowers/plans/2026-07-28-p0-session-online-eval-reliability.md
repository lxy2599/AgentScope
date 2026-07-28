# P0：会话失效与在线评测可靠投递实施计划

**状态：** 已确认，待实施  
**日期：** 2026-07-28  
**范围：** Go HTTP API、PostgreSQL、React 控制台、Run 终态后的在线评测调度；不改变 Trace、数据集、离线实验和 Judge 评分公式。  
**目标：** 消除两个会导致系统仍呈现“Demo 化”特征的 P0 缺口：前端退出未使服务端会话失效，以及在线评测依赖进程内 Goroutine、无法可靠恢复。

---

## 1. 背景与问题

### 1.1 当前会话退出只是 UI 退出

当前登录成功后，服务端签发带签名和有效期的 HttpOnly `session` Cookie。前端退出仅清除
`sessionStorage` 中的用户展示信息：

```text
前端 user state 清空
  ≠
浏览器 session Cookie 被清除
  ≠
服务端撤销该会话
```

因此在 Cookie 仍未过期时，浏览器仍会自动携带它访问受保护 API。系统需要从“前端隐藏登录态”
升级为“服务端可失效的用户会话”。

### 1.2 当前线上评测是非持久化后台任务

Run 进入终态后，Runtime Handler 通过 Goroutine 调用 `OnlineEvalDispatcher`；Dispatcher 又在
进程内启动 Goroutine 执行评测。成功时会写 Eval Score，并可能创建 Annotation Item；失败时主要
记录日志。

```text
Run 终态
  → go DispatchRunAsync
  → Sampling Rule
  → go evaluateAsync
  → 评测 / 写分 / 创建人工标注项
```

该模型在单进程 Demo 中有效，但没有任务事实、重试、服务重启恢复、死信状态或显式降级语义。
用户无法可靠地区分“未抽样”“评测排队”“评测失败”“任务丢失”“基础状态分降级”。

---

## 2. 目标与非目标

### 2.1 目标

1. 用户退出后，当前浏览器会话立即不能调用受保护 API；
2. 修改密码、禁用用户或安全响应时，可撤销该用户的历史会话；
3. 在线评测任务在 PostgreSQL 有权威状态，服务重启后可恢复；
4. 评测失败具备退避重试、最终失败和可诊断原因；
5. Sampling 决策和 baseline fallback 被明确持久化和展示；
6. 低分/失败 Run 的人工标注回流保持幂等；
7. 不把 Redis 当作 Session、在线评测任务或撤销事实的权威来源。

### 2.2 非目标

- 本期不引入外部身份提供商、SSO、MFA 或组织级 SCIM；
- 本期不强制引入 Kafka、Temporal、Celery 等独立队列产品；
- 本期不改变离线 Dataset/Experiment 执行队列；
- 本期不重写 TraceAwareEvaluator 或 LLM Judge 评分算法；
- 本期不做跨地域多活调度。

---

## 3. 设计原则

```text
PostgreSQL  保存 Session、撤销、评测任务、重试和最终状态的权威事实
Redis       仅用于可重建实时投影、SSE 与短期限流，不保存任务或撤销事实
Cookie      只保存不可猜测的 Session ID 与签名，不保存用户角色快照
Outbox      用于在权威事务成功后触发 Worker/实时事件
Worker      可横向扩展，通过数据库领取锁保证同一 Job 不被并发执行
Fallback    可以存在，但必须显式标为 baseline_fallback，不能伪装为正式 Judge 结果
```

---

# Part A：可撤销会话

## 4. 目标状态

### 4.1 新会话流程

```text
登录 / 注册
  → 创建 user_sessions 记录（active）
  → 签发 session Cookie：v2.<session_id>.<issued_at>.<signature>
  → 后续请求验签、查询 Session、检查 revoked_at / expires_at
  → 得到 user_id
```

### 4.2 退出流程

```text
前端点击退出
  → POST /api/auth/logout
  → 数据库将当前 session revoked_at 写入 now()
  → 响应 Set-Cookie: session=; Max-Age=0
  → 前端清理 sessionStorage / Query Cache
  → 跳转登录页
```

### 4.3 安全响应流程

```text
修改密码 / 管理员禁用用户 / 用户选择「退出所有设备」
  → UPDATE user_sessions SET revoked_at=now(), revoked_reason=...
  → 所有旧 Cookie 在下一次请求认证时立即拒绝
```

## 5. 数据模型与迁移

新增迁移：`migrations/00017_user_sessions.sql`。

```sql
CREATE TABLE user_sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  secret_hash BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_used_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ,
  revoked_reason TEXT,
  user_agent_summary TEXT,
  ip_prefix TEXT
);

CREATE INDEX idx_user_sessions_active_user
  ON user_sessions(user_id, expires_at DESC)
  WHERE revoked_at IS NULL;
```

说明：

- Cookie 中存 `session_id` 和随机 session secret；数据库只存 secret 的 SHA-256 哈希；
- `secret_hash` 避免单独泄露数据库时可直接伪造会话；
- `user_agent_summary`、`ip_prefix` 仅用于用户可见的设备识别和安全审计，不能保存完整敏感 Header；
- 旧 v1 Cookie 在过渡期仍可验证，但只允许短期兼容。完成迁移窗口后移除 v1 验证分支。

## 6. 后端改动

### 6.1 Auth domain

调整 `internal/auth`：

```go
type Session struct {
  ID, UserID, RevokedReason string
  CreatedAt, ExpiresAt time.Time
  LastUsedAt, RevokedAt *time.Time
}

type SessionService interface {
  CreateSession(...)
  AuthenticateSession(...)
  RevokeSession(...)
  RevokeAllUserSessions(...)
  ListUserSessions(...)
}
```

Session 验证顺序：

```text
Cookie 格式 / 签名错误      → 401
Session 不存在              → 401
secret hash 不匹配          → 401
revoked_at 非空             → 401
expires_at <= now()         → 401
否则更新 last_used_at（可合并写）并放行
```

### 6.2 HTTP API

新增接口：

```text
POST /api/auth/logout
POST /api/auth/logout-all
GET  /api/auth/sessions
DELETE /api/auth/sessions/:sessionID
```

权限：

| 操作 | 权限 |
|---|---|
| logout | 当前已认证用户；只能撤销当前 Session |
| logout-all | 当前已认证用户；撤销自身所有 Session，当前请求除外或全部后重新登录 |
| list sessions | 当前已认证用户；只列自己的 Session 元数据 |
| delete session | 当前已认证用户；只撤销自己的非当前 Session |
| disable user 后撤销 | 管理员能力，留给后续用户管理模块 |

### 6.3 Cookie 与前端

- 保持 `HttpOnly`、`SameSite=Lax`；生产环境要求 `Secure=true`；
- 退出成功或失败时都清理前端的展示状态，但只有服务端成功响应才宣称“会话已失效”；
- `apiRequest` 收到 `401` 时清理本地 user state 和 React Query cache，并跳转登录页；
- 前端 Session 页面只显示设备摘要、创建/最近使用时间，不显示 Cookie、secret 或完整 IP。

## 7. 会话验收

1. 登录后调用 `GET /api/projects` 成功；
2. 调用 Logout 后，用原 Cookie 调用同一接口返回 `401`；
3. 删除指定设备 Session 后，该 Session 立即返回 `401`；
4. `logout-all` 后所有旧 Cookie 被拒绝；
5. Cookie、API 响应、日志、审计、Snapshot 中不含 Session secret；
6. 生产模式缺少 `AGENTSCOPE_SECURE_COOKIES=true` 时服务拒绝启动或输出明确配置错误。

---

# Part B：在线评测可靠 Job 系统

## 8. 目标状态

```text
Run 进入终态
  → PostgreSQL Transaction 内写 outbox: run.terminal
  → Online Eval Relay 创建 / 去重 online_eval_job
  → Worker 领取 queued/retry_wait Job
  → Sampling 决策持久化
      ├─ 未抽样：skipped
      └─ 命中：running
  → 执行 TraceAware / Judge 评测
      ├─ 成功：写分、写聚合、succeeded
      ├─ 可重试失败：retry_wait + available_at
      └─ 不可重试/超过次数：failed / dead_letter
  → 低分或失败：幂等创建 Annotation Item
  → Outbox 发布 job 状态和评测结果的实时事件
```

## 9. 数据模型与迁移

新增迁移：`migrations/00018_online_eval_jobs.sql`。

```sql
CREATE TABLE online_eval_jobs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
  trace_id TEXT NOT NULL,
  evaluator_version TEXT NOT NULL,
  status TEXT NOT NULL,
  sampling_rule_id UUID,
  sampling_reason TEXT,
  evaluation_source TEXT,
  fallback_reason TEXT,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 5,
  available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  claimed_by TEXT,
  claimed_at TIMESTAMPTZ,
  started_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  last_error TEXT,
  annotation_item_id UUID,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(project_id, run_id, evaluator_version)
);

CREATE INDEX idx_online_eval_jobs_claimable
  ON online_eval_jobs(available_at, created_at)
  WHERE status IN ('queued', 'retry_wait');
```

状态机：

```text
queued → sampling → running → succeeded
queued → sampling → skipped
running → retry_wait → running
running → failed
retry_wait → dead_letter
```

状态含义：

| 状态 | 说明 |
|---|---|
| `queued` | 已收到终态 Run，待 Worker 处理 |
| `sampling` | 正在执行/记录 Sampling 决策 |
| `skipped` | 未命中 Sampling Rule，明确不是漏评 |
| `running` | 已被某 Worker 领取并开始评测 |
| `retry_wait` | 可恢复错误，等待退避后重试 |
| `succeeded` | 已写入评测结果；需结合 source 判断质量 |
| `failed` | 不可恢复错误或达到最大重试次数 |
| `dead_letter` | 需人工介入的长期失败任务 |

## 10. Worker 与领取算法

首期使用 PostgreSQL，不引入外部 Broker。

```sql
SELECT id
FROM online_eval_jobs
WHERE status IN ('queued', 'retry_wait')
  AND available_at <= now()
ORDER BY available_at, created_at
FOR UPDATE SKIP LOCKED
LIMIT $batch_size;
```

领取后必须在同一事务中更新：

```text
status=running
attempt_count=attempt_count+1
claimed_by=<instance id>
claimed_at=now()
started_at=now()
```

Worker 规则：

- 启动 `N` 个有限并发 Worker；
- 每个 Job 有独立 timeout；
- 服务关闭时停止领取新 Job，等待当前 Job 到 deadline，再释放或标记 retry；
- 过期 `claimed_at` 的 running Job 由 recovery sweep 重新置为 `retry_wait`；
- 失败原因使用安全摘要，禁止写 Prompt、Authorization、Token 或完整敏感输入。

## 11. Sampling、评测来源和降级语义

### 11.1 Sampling 决策必须可查询

每个 Job 必须保存：

```text
sampling_rule_id
sampling_reason
sampled / skipped
```

因此用户可区分：

```text
没有分数，因为未抽样
```

与：

```text
本应有分数，但评测执行失败
```

### 11.2 评测来源必须可查询

统一字段：

```text
evaluation_source:
  trace_heuristic
  rubric_llm_judge
  baseline_fallback
  manual_gold
```

`baseline_fallback` 的展示要求：

```text
基础运行状态分（降级）
不作为正式质量回归门禁
```

不能将其与真实 TraceAware、Rubric 或 LLM Judge 分数混合成同一种“评测通过”。

### 11.3 幂等 Annotation 回流

对每个 `(project_id, run_id, queue_id)` 保持最多一个自动创建的 Annotation Item，避免 Job 重试时
重复投递人工队列。创建成功后写入 `annotation_item_id`，并发布实时事件。

## 12. API 与前端

新增接口：

```text
GET /api/projects/:projectID/online-eval-jobs
GET /api/projects/:projectID/online-eval-jobs/:jobID
POST /api/projects/:projectID/online-eval-jobs/:jobID/retry
```

前端在 Run 详情、Active Runs、Annotation Queue 中统一展示：

```text
未抽样 / 排队中 / 评测中 / 已完成 / 已降级 / 重试等待 / 已失败
```

核心展示字段：

- Sampling Rule 与采样原因；
- 队列等待时长、执行时长、尝试次数；
- evaluator version 与 `evaluation_source`；
- fallback / failure 原因安全摘要；
- Annotation Item 跳转；
- 重试按钮仅对 owner/admin 开放。

## 13. 迁移策略与兼容性

### 阶段 B1：双写与影子验证

1. 创建 Job 表和 Worker，但保留现有 Goroutine Dispatcher；
2. Run 终态同时创建 Job，Worker 仅记录结果/状态，不影响现有页面门禁；
3. 对同一批 Run 比较旧链路和 Job 链路的结果数量、耗时和 Annotation 幂等性；
4. 监控 Job 丢失率、重复率、失败率。

### 阶段 B2：Job 成为唯一调度来源

1. 移除 Handler 中 `go DispatchRunAsync`；
2. Run 终态只写权威 Outbox / Job；
3. Worker 负责全部评测与回流；
4. 保留旧 Dispatcher 代码一个版本作为回滚点，随后删除。

### 阶段 B3：运营化

1. 项目级并发、每日预算、Provider 限流；
2. dead-letter 告警；
3. Job SLO：排队 P95、成功率、重试率、人工回流延迟；
4. 多实例 Worker 压测和故障演练。

## 14. 在线评测验收

### 可靠性

- Run 终态后，Job 记录必定存在，或事务整体失败；
- Worker 在服务重启后可继续处理 queued/retry_wait Job；
- Worker 崩溃中断的 running Job 可被 recovery sweep 恢复；
- 一次短暂 ClickHouse / Judge / PostgreSQL 故障会触发重试，而非永久漏评；
- 达到最大重试次数后 Job 明确显示 failed/dead_letter。

### 语义

- `skipped`、`failed`、`baseline_fallback` 和正式评测在 API/UI 上可区分；
- baseline fallback 不参与需要正式 Judge 的实验回归门禁；
- 低分/失败 Job 重试后不会重复创建 Annotation Item；
- Job、Score、Annotation Item、Run 的 project_id 始终一致。

### 性能

- HTTP Runtime 完成请求不等待在线评测；
- Worker 并发受配置和项目配额限制；
- 10 倍于当前 Sampling 流量的烟测下，Run 终态 P95 不因评测调度明显恶化；
- 队列积压、失败和 worker starvation 有指标和告警。

---

## 15. 实施顺序与提交边界

### Commit 1：会话退出安全基线

```text
feat: 增加服务端会话退出与失效
```

包含 `00017_user_sessions.sql`、v2 Cookie、Logout API、前端退出、401 清理与测试。

### Commit 2：在线评测 Job 数据模型与 Worker

```text
feat: 持久化在线评测任务与重试
```

包含 `00018_online_eval_jobs.sql`、Job repository/service、领取锁、Worker、恢复扫描与测试。

### Commit 3：线上评测状态工作台

```text
feat: 展示在线评测任务状态与降级语义
```

包含 Job API、Run/Annotation UI、Sampling/Source/Fallback 展示与浏览器验收。

### Commit 4：验收记录

```text
docs: 记录 P0 会话与线上评测可靠性验收
```

## 16. 文件影响预估

```text
migrations/00017_user_sessions.sql                 新增
migrations/00018_online_eval_jobs.sql              新增
internal/auth/*                                    Session domain 扩展
internal/adapter/auth.go                           Session persistence
internal/httpapi/auth_handlers.go                  Logout / Session API
internal/httpapi/middleware.go                     v2 Session 校验
internal/onlinerval/*                              Job service、worker、状态机
internal/adapter/*                                 Job persistence、outbox 适配
internal/app/app.go                                Worker 生命周期与 shutdown drain
web/src/app/App.tsx                                server logout / 401 cleanup
web/src/api/types.ts                               Session / OnlineEvalJob DTO
web/src/api/hooks.ts                               Session / Job hooks
web/src/features/runs/*                            Run 评测状态展示
web/src/features/projects/*                        Session / Job 运营入口
```

## 17. 风险与决策

| 风险 | 决策 |
|---|---|
| 无状态 Cookie 改为可撤销会话增加数据库读取 | 首期直接读取 PostgreSQL；后续可加安全失效的短缓存，撤销仍以数据库为准 |
| Job Worker 增加实现复杂度 | 先使用 PostgreSQL `SKIP LOCKED`，不引入新基础设施 |
| 评测失败时用户体验变差 | 明确状态优于静默漏评；允许管理员重试并展示失败摘要 |
| baseline fallback 影响历史报表 | 新字段标明来源；报表默认分开聚合，避免混淆 |
| 旧 v1 Cookie 兼容 | 限时兼容，发布窗口后强制全部重新登录 |

## 18. 完成定义

当且仅当以下条件全部满足，P0 才能宣布完成：

1. Logout 后旧 Cookie 被真实 API 拒绝；
2. 可以撤销单个及全部 Session；
3. 所有终态且命中采样的 Run 均有可查询 Online Eval Job；
4. 服务重启、Worker 中断和下游短暂故障不会造成无状态漏评；
5. Job 状态、采样决策、fallback 来源和 Annotation 回流在 UI 中可追溯；
6. PostgreSQL 数据核验、Go race 测试、Web 测试、真实 API 流程和桌面/移动端浏览器验收通过；
7. 日志、数据库、SSE、Snapshot 和测试产物均不泄露 Cookie、Session secret、Project Token 或敏感 Prompt。
