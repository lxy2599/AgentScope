# AgentScope Project Token 生命周期与生产治理实施计划

**状态：** 已确认，待实施  
**日期：** 2026-07-28  
**范围：** Go API、PostgreSQL、Project Token 鉴权、React 控制台；不改变 Trace / Run / 评测领域语义。  
**目标读者：** 后端、前端、SDK/Runner 接入实现者，以及负责项目安全与生产运行的管理员。

---

## 1. 背景与问题定义

AgentScope 通过 Project Token 让 Python SDK、Runner 与 Agent Runtime 向指定项目上报 OTLP、注册实例和更新运行状态。现有实现已具备安全基础：Token 使用随机 32 字节生成，数据库只保存 SHA-256 哈希与前缀，明文只在创建时返回一次，并按 Scope 执行鉴权。

但当前能力仍是“创建并使用”的 MVP：

1. 控制台只能创建 Token，不能列出已有 Token；
2. SQL 已有 `ListProjectTokens`，但没有对应领域服务、HTTP API 与前端页面；
3. 没有撤销、失效、轮换和审计能力，Token 泄露后不能快速止损；
4. `expires_at` 与 `last_used_at` 已存在但尚未写入或校验；
5. 创建 Token 只校验项目成员资格，普通 `member` 也可能创建高权限 Token；
6. 前端默认一次性授予四个 Scope，不符合最小权限原则；
7. 无法区分本地、CI、测试、生产 Token，也无法识别临近到期、长期未使用或过宽权限的 Token。

本计划将 Token 从“接入字符串”升级为可撤销、可轮换、可审计的项目级凭证治理能力。

> **核心原则：Token 明文只在创建时返回一次；撤销保留记录而不硬删除；鉴权链路始终校验生命周期；PostgreSQL 保存权威 Token 元数据与审计事实。**

---

## 2. 目标与非目标

### 2.1 目标

- 管理员能够创建、查看、撤销、轮换 Project Token；
- Token 遵循最小权限、到期失效与项目隔离规则；
- 生产上报链路不因记录“最近使用时间”而降低吞吐或可靠性；
- 管理员能够追溯 Token 的创建、撤销、轮换和最近使用信息；
- 控制台可以识别即将到期、长期未使用、权限过宽的高风险 Token；
- 保持 SDK/Runner 的 `Authorization: Bearer <token>` 接入方式兼容。

### 2.2 非目标

- 不支持读取历史 Token 明文或恢复明文；
- 不将 Token 明文、完整哈希、请求中的 `Authorization` 头写入日志、审计表、SSE 或 Snapshot；
- 不将 Redis 作为 Token 状态、撤销状态或审计记录的权威来源；
- 首轮不强制引入 IP 白名单、外部 KMS、组织级 SSO 审批流；
- 首轮不改变项目成员体系本身，只在现有 `owner/admin/member` 角色上收紧凭证管理权限。

---

## 3. 现状与设计约束

### 3.1 现有契约

- `project_tokens` 当前保存 `token_hash`、`token_prefix`、`scope`、`description`、`created_at`、`expires_at`、`last_used_at`；
- 后端已支持 `otlp:write`、`runner:write`、`agent:register`、`agent:runtime` 四个 Scope；
- `AuthenticateToken` 按哈希查询 Token 并验证 Scope；
- 前端当前使用 `POST /api/projects/:projectID/tokens` 创建 Token；
- PostgreSQL 是项目、权限与配置的权威存储；Redis 只可保存可重建的短期聚合数据。

### 3.2 不可违反的安全规则

```text
Token 明文       仅在 POST 创建 / 轮换响应中返回一次
数据库           仅保存哈希、前缀和元数据
撤销             软撤销，不可通过重新启用绕过审计
鉴权             每次请求检查 revoked_at / expires_at / scope
项目隔离         Token 项目只能访问该项目资源
审计             不记录明文、完整哈希或 Authorization Header
```

### 3.3 权限矩阵

| 动作 | owner | admin | member |
|---|:---:|:---:|:---:|
| 查看 Token 元数据 | ✓ | ✓ | 默认不可 |
| 创建 Token | ✓ | ✓ | 默认不可 |
| 撤销 Token | ✓ | ✓ | 默认不可 |
| 发起轮换 | ✓ | ✓ | 默认不可 |
| 查看审计事件 | ✓ | ✓ | 默认不可 |
| 使用已签发 Token 调用受限 API | 按 Scope | 按 Scope | 按 Scope |

说明：Token 的使用权限来自 Scope，与创建该 Token 的用户角色无关；但签发和治理权限必须要求 `owner/admin`。

---

## 4. 总体设计

### 4.1 生命周期状态

不新增容易与时间字段不一致的持久化 `status`；由字段计算展示状态：

```text
revoked_at 非空                         → revoked
revoked_at 为空且 expires_at <= now()   → expired
否则                                    → active
```

`revoked` 优先于 `expired` 展示。撤销的 Token 永远不可恢复；如需继续接入，应签发新 Token。

### 4.2 目标数据模型

阶段一迁移 `00014_project_token_governance.sql`：

```sql
ALTER TABLE project_tokens
  ADD COLUMN created_by UUID REFERENCES users(id),
  ADD COLUMN revoked_at TIMESTAMPTZ,
  ADD COLUMN revoked_by UUID REFERENCES users(id),
  ADD COLUMN revocation_reason TEXT,
  ADD COLUMN rotates_token_id UUID REFERENCES project_tokens(id);

CREATE INDEX idx_project_tokens_project_created
  ON project_tokens(project_id, created_at DESC);

CREATE INDEX idx_project_tokens_active_expiry
  ON project_tokens(project_id, expires_at)
  WHERE revoked_at IS NULL;
```

阶段二迁移 `00015_project_token_audit.sql`：

```sql
ALTER TABLE project_tokens
  ADD COLUMN last_used_endpoint TEXT,
  ADD COLUMN last_used_agent_id UUID,
  ADD COLUMN last_used_instance_id UUID;

CREATE TABLE project_token_audit_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  token_id UUID NOT NULL REFERENCES project_tokens(id) ON DELETE RESTRICT,
  actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  event_type TEXT NOT NULL,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_project_token_audit_events_token_time
  ON project_token_audit_events(token_id, created_at DESC);
```

阶段三迁移 `00016_project_token_policy.sql`：

```sql
ALTER TABLE project_tokens
  ADD COLUMN environment TEXT NOT NULL DEFAULT 'development',
  ADD COLUMN token_type TEXT NOT NULL DEFAULT 'custom';

CREATE TABLE project_token_policies (
  project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
  allow_non_expiring BOOLEAN NOT NULL DEFAULT true,
  max_production_ttl_days INTEGER,
  max_ci_ttl_days INTEGER,
  expiry_warning_days INTEGER[] NOT NULL DEFAULT ARRAY[14, 7, 1],
  unused_warning_days INTEGER NOT NULL DEFAULT 60,
  updated_by UUID REFERENCES users(id),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

不迁移 `scope TEXT` 为数组类型：现有服务以标准化、去重、空格分隔的字符串持久化 Scope，第一期保持兼容，API 层统一返回 `scopes: string[]`。

### 4.3 API 设计

```text
GET  /api/projects/:projectID/tokens
POST /api/projects/:projectID/tokens
POST /api/projects/:projectID/tokens/:tokenID/revoke
POST /api/projects/:projectID/tokens/:tokenID/rotate
GET  /api/projects/:projectID/tokens/:tokenID/audit-events
GET  /api/projects/:projectID/token-policy
PUT  /api/projects/:projectID/token-policy
```

列表响应永远不包含 `plain_token`、`token_hash` 或完整 Token：

```json
{
  "id": "uuid",
  "project_id": "uuid",
  "token_prefix": "ascope_a1b2c3d4e5f6",
  "description": "production-python-sdk",
  "scopes": ["otlp:write"],
  "environment": "production",
  "token_type": "trace_sdk",
  "created_at": "2026-07-28T10:00:00Z",
  "expires_at": "2026-10-26T10:00:00Z",
  "last_used_at": "2026-07-28T10:05:00Z",
  "status": "active"
}
```

### 4.4 最近使用信息的写入策略

OTLP / Runner 是高频路径，不能在每次鉴权时同步写 PostgreSQL。

```text
Token 鉴权成功
  → 读取权威 Token 元数据并验证生命周期 / Scope
  → 将使用信息写入本地去重窗口或 Redis 短期聚合键
  → 后台批量合并更新 PostgreSQL last_used_*
  → Redis 丢失后允许重新开始统计，不影响鉴权或撤销事实
```

- 撤销、到期、Scope 校验仍必须同步读取权威状态或使用可安全失效的短缓存；
- `last_used_*` 是运维可观测信息，不是授权事实；
- 写入失败不应阻断合法的 OTLP / Runner 请求，但必须计入内部运维指标。

---

## 5. 阶段一：安全基线与可控止损

**目标：** 从“只能创建”升级为“可看见、可撤销、权限受控”。

### 5.1 后端任务

1. 新增 `00014_project_token_governance.sql`；
2. 扩展 store、adapter、projects service 领域模型；
3. 为 `ProjectsService` 增加 `ListTokens`、`RevokeToken`；
4. 将创建 Token 的权限从 `IsProjectMember` 收紧为 `IsProjectAdmin`；
5. 新增 Token 列表和撤销 Handler；
6. 扩展 `AuthenticateToken`：撤销或过期 Token 返回认证失败；
7. 创建与撤销写入基础审计事件；
8. 保持旧 `POST /tokens` 兼容，但将请求扩展为可选 `expires_at`、`environment`、`token_type`。

### 5.2 前端任务

1. 增加 `useProjectTokens`、`useRevokeProjectToken`；
2. 将“接入与配额”改为 Token 管理工作台；
3. 展示前缀、描述、Scope、创建时间、到期时间、状态；
4. 创建 Token 时提供最小权限模板：
   - Trace 上报 SDK：`otlp:write`；
   - Runner：`runner:write`；
   - Agent Runtime：`agent:register` + `agent:runtime`；
   - 完整接入：四项 Scope，需二次确认；
5. 撤销操作使用危险确认框，要求确认 Token 前缀，并允许填写原因；
6. 继续只在创建成功后显示一次完整 Token，支持复制但不写入页面状态恢复、日志或 URL。

### 5.3 测试与验收

- owner/admin 可以创建、列出、撤销 Token；member 返回 403；
- 撤销后 Token 访问 OTLP、Runner、Runtime API 均被拒绝；
- 过期 Token 被拒绝；
- 列表、错误响应、审计事件、前端 DOM 和浏览器日志不含 Token 明文或完整哈希；
- 390px 与 1440px 下 Token 表格可阅读且不会造成文档横向溢出。

---

## 6. 阶段二：生命周期、无中断轮换与审计

**目标：** 能回答“Token 是否仍在使用”，并可安全替换生产 Token。

### 6.1 后端任务

1. 新增 `00015_project_token_audit.sql`；
2. 增加 `last_used_endpoint`、关联 Agent/Instance 的批量更新仓储接口；
3. 实现可重建的使用信息聚合与后台批量刷新；
4. 新增 `RotateToken` 领域服务和 API；
5. 新增 Token 审计事件查询 API；
6. 在创建、撤销、轮换发起、轮换完成、鉴权拒绝（仅聚合指标）间建立一致语义；
7. 轮换创建的新 Token 记录 `rotates_token_id`，旧 Token 不自动撤销。

### 6.2 轮换流程

```text
管理员发起轮换
  → 创建新 Token（仅一次返回明文）
  → 更新 SDK / Runner 部署变量
  → 新 Token 产生 last_used_at
  → 管理员确认切换
  → 撤销旧 Token，并写入 audit event
```

前端必须把“新 Token 已首次使用”作为撤销旧 Token 前的明确检查项，而不是创建完成后自动失效旧 Token。

### 6.3 前端任务

1. Token 列表展示最近使用时间、入口、关联 Agent/Instance、过期时间；
2. 增加“轮换”向导及进度状态；
3. 增加 Token 详情抽屉，展示 Scope、生命周期、审计历史；
4. 增加“即将到期”“从未使用”“长期未使用”的状态说明；
5. 支持按状态、环境、Scope、到期时间过滤。

### 6.4 测试与验收

- 高频 OTLP smoke / load 下，使用信息记录不会显著降低采集吞吐；
- 新 Token 成功使用前，UI 禁止将“轮换完成”标记为已完成；
- 旧 Token 撤销后，新 Token 持续可用，Run/Trace 上报不中断；
- 审计可追溯创建者、撤销者、时间、Token 前缀与原因，但不泄露明文。

---

## 7. 阶段三：生产策略与风险治理

**目标：** 让凭证治理由人工清单升级为项目级、可持续的生产策略。

### 7.1 后端任务

1. 新增 `00016_project_token_policy.sql`；
2. 增加 `environment` 与 `token_type` 校验；
3. 增加项目级 Token Policy 的读取与更新服务；
4. 在 Token 创建时强制校验策略：
   - 是否允许永不过期；
   - production / CI 最大 TTL；
   - 是否限制完整 Scope Token；
5. 输出项目级风险摘要：临近过期、长期未使用、高权限、无环境标记；
6. 可选接入 Outbox，为后续通知系统提供 `token.expiring`、`token.revoked` 事件。

### 7.2 前端任务

1. 为 Token 标记 `development/staging/production/ci/local`；
2. 增加 `trace_sdk/runner/agent_runtime/ci/custom` 类型；
3. 在项目设置页增加 Token Policy 编辑区；
4. 增加“需要处理”风险卡片：
   - N 个 Token 将在 7 天内到期；
   - N 个 Token 已长期未使用；
   - N 个生产 Token 持有完整 Scope；
5. 支持从 Token 跳转到最近关联 Agent、Run、采集健康事件。

### 7.3 可选增强（不阻塞阶段三验收）

- CI 专用短期 Token；
- Agent Instance 绑定；
- IP / CIDR 白名单；
- 审批流与外部密钥管理系统；
- 到期、撤销与异常来源的通知集成。

IP 约束不作为默认能力，因为 NAT、动态 Worker 与云部署会降低接入兼容性。

### 7.4 测试与验收

- 生产 Token 无法绕过项目 TTL 与 Scope 策略；
- 风险卡片与实际 Token 列表统计一致；
- Token 风险可定位至受影响 Agent / Run / Ingest 事件；
- 项目策略、Token 元数据、审计事件均在 PostgreSQL 中可恢复与审计；
- Redis 故障不影响 Token 撤销、到期和 Scope 鉴权。

---

## 8. 实施顺序与提交边界

```text
阶段一：数据迁移 + List/Revoke API + RBAC + Token 管理页
阶段二：last_used 批量更新 + Rotation + Audit + 详情抽屉
阶段三：环境/类型 + Project Policy + 风险工作台 + Outbox 事件
```

建议每个阶段分别提交，且每阶段按以下边界拆分：

1. PostgreSQL migration + store/sqlc；
2. projects domain service + HTTP API + 后端测试；
3. React types/hooks/pages + 单元测试；
4. 真实 SDK/Runner 流程与浏览器 E2E 验收；
5. 更新本计划和 `docs/phase4-verification-report.md` 的命令、数据与风险记录。

---

## 9. 总体验收清单

- [ ] 明文 Token 从不写入 PostgreSQL、日志、审计、SSE、Snapshot 或 URL；
- [ ] 只有 owner/admin 可管理 Token；
- [ ] 所有 Token 可以被列出、撤销并保留历史；
- [ ] 撤销与过期在鉴权路径立即生效；
- [ ] Scope 默认最小化，完整 Scope 需要显式确认；
- [ ] 支持无中断轮换；
- [ ] 最近使用信息不会拖慢高吞吐 OTLP / Runner 链路；
- [ ] Token 具有环境、类型、生命周期和风险状态；
- [ ] 项目级策略可以限制生产与 CI Token；
- [ ] 后端单元测试、迁移测试、真实接入测试、桌面/移动端浏览器验收均通过。
