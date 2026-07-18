# Agent Control Center — Phase 2.1

目标：把 AgentScope 从只展示 demo trace，升级为可以看到 SDK 接入的 Agent 在线状态。

## 交付范围

### PostgreSQL 表
- `agents` — Agent 元数据注册
- `agent_versions` — Agent 版本历史
- `agent_instances` — 运行实例
- `agent_heartbeats` — 心跳记录
- `agent_capabilities` — Agent 能力声明

### Go 后端
- Agent 注册/列表/详情/心跳 领域服务
- SDK bearer token 注册 API
- SDK heartbeat API
- UI protected Agent API

### Python SDK
- `agentscope.init(...)` — SDK 初始化时注册 Agent
- Runtime heartbeat loop — 后台心跳线程
- `shutdown_runtime` — 优雅关闭
- `get_runtime` — 获取当前 runtime

### 前端
- Agent 列表页
- Agent 详情页
- 在线实例、运行实例、capabilities 展示

### Demo
- `make demo-real-agent` 运行时注册 `real-agent`
- 前端 Agent 页面能看到 `real-agent` 在线

## 任务列表

### Task 1: Agent 控制中心数据模型

**文件范围：**
- `migrations/*.sql` — PostgreSQL migration
- `queries/*.sql` — sqlc queries
- `internal/store/*.go` — sqlc generate 输出

**Data Model：**

```sql
-- agents: Agent 注册表
CREATE TABLE agents (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES projects(id),
    name        VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    metadata    JSONB NOT NULL DEFAULT '{}',
    version     INTEGER NOT NULL DEFAULT 1,
    status      VARCHAR(50) NOT NULL DEFAULT 'active', -- active, inactive, deprecated
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, name)
);

-- agent_versions: Agent 版本
CREATE TABLE agent_versions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agents(id),
    version     INTEGER NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(agent_id, version)
);

-- agent_instances: 运行实例
CREATE TABLE agent_instances (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agents(id),
    instance_id VARCHAR(255) NOT NULL,
    status      VARCHAR(50) NOT NULL DEFAULT 'online', -- online, offline, error
    hostname    VARCHAR(255) NOT NULL DEFAULT '',
    pid         INTEGER NOT NULL DEFAULT 0,
    metadata    JSONB NOT NULL DEFAULT '{}',
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(agent_id, instance_id)
);

-- agent_heartbeats: 心跳历史
CREATE TABLE agent_heartbeats (
    id          BIGSERIAL PRIMARY KEY,
    agent_id    UUID NOT NULL REFERENCES agents(id),
    instance_id UUID NOT NULL REFERENCES agent_instances(id),
    status      VARCHAR(50) NOT NULL DEFAULT 'online',
    payload     JSONB NOT NULL DEFAULT '{}',
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- agent_capabilities: Agent 能力
CREATE TABLE agent_capabilities (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agents(id),
    name        VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(agent_id, name)
);
```

### Task 2: Agent 领域服务

**文件范围：**
- `internal/agents/` — 领域服务包

**接口：**
- `RegisterAgent(ctx, projectID, name, description, metadata) (*Agent, error)`
- `ListAgents(ctx, projectID) ([]*Agent, error)`
- `GetAgent(ctx, agentID) (*Agent, error)`
- `GetAgentByName(ctx, projectID, name) (*Agent, error)`
- `UpdateAgentStatus(ctx, agentID, status) error`
- `RecordHeartbeat(ctx, agentID, instanceID, status, payload) error`
- `RegisterInstance(ctx, agentID, instanceID, hostname, pid, metadata) (*AgentInstance, error)`
- `ListInstances(ctx, agentID) ([]*AgentInstance, error)`
- `SetCapability(ctx, agentID, name, description, metadata) error`
- `ListCapabilities(ctx, agentID) ([]*AgentCapability, error)`

### Task 3: Agent API

**文件范围：**
- `internal/httpapi/` — HTTP handler

**API Endpoints：**
- `POST /api/v1/agents/register` — SDK 注册 Agent (bearer token auth)
- `POST /api/v1/agents/:agentID/heartbeat` — SDK 心跳 (bearer token auth)
- `GET /api/v1/agents` — UI 获取 Agent 列表 (session auth)
- `GET /api/v1/agents/:agentID` — UI 获取 Agent 详情 (session auth)

### Task 4: Python SDK 注册和心跳

**文件范围：**
- `python/agentscope_sdk/` — Python SDK

**新增：**
- `agentscope.init(...)` — SDK 初始化，注册 Agent 并启动心跳
- Heartbeat loop — 每 30s 发送心跳
- `shutdown_runtime` — 停止心跳、注销实例
- `get_runtime` — 获取当前 runtime 上下文

### Task 5: 前端 Agent 页面

**文件范围：**
- `web/src/` — React UI

**新增页面：**
- `/agents` — Agent 列表
- `/agents/:agentID` — Agent 详情
- 侧边栏导航增加 Agent 入口

### Task 6: Demo 说明更新

**文件范围：**
- `examples/` — demo 更新
- `docs/` — 使用说明

### Task 7: 集成验证

运行完整验证命令。

## 提交建议

1. `feat: 添加 Agent 控制中心数据模型`
2. `feat: 添加 Agent 控制中心领域服务`
3. `feat: 添加 Agent 控制中心 API`
4. `feat: 为 Python SDK 添加 Agent 注册和心跳`
5. `feat: 添加 Agent 控制中心前端页面`
6. `docs: 更新真实 Agent 在线演示说明`
7. `fix: 修复 Agent 控制中心集成验证问题` (如有需要)
