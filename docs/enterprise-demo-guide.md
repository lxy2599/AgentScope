# AgentScope 企业级演示指南

## 概述

AgentScope 是一个通用 Agent 可观测与多维评测平台。本文档介绍如何运行和验证四个企业级演示场景。

四个演示从简到繁依次为：

1. **通用 Agent 可观测演示 (generic-observability-agent)** — 展示 SDK 语义埋点和 Deep Trace
2. **客服订单处理 Agent 演示 (support-agent)** — 展示复杂多步骤 Agent 流程
3. **真实 LLM Agent 评测演示 (real-agent)** — 展示真实 LLM + 评测闭环
4. **多维评测演示 (multi-eval)** — 展示多维度指标评测

---

## 1. 项目启动

### 前置要求

- Docker & Docker Compose
- Go 1.22+
- Python 3.10+
- Node.js 20+

### 启动后端依赖（PostgreSQL + ClickHouse）

```bash
make db-up
```

### 应用数据库迁移

```bash
make dev
```

`make dev` 会自动启动 PostgreSQL、ClickHouse、后端服务和前端开发服务器。

也可以通过以下命令分别启动：

```bash
# 仅启动数据库
docker compose up -d

# 应用迁移
./scripts/dev.sh

# 启动后端
make server

# 启动前端
cd web && npm run dev
```

### 访问地址

| 组件 | 地址 |
|---|---|
| 前端 | http://127.0.0.1:5173 |
| 后端 API | http://127.0.0.1:8080 |
| 健康检查 | http://127.0.0.1:8080/healthz |

---

## 2. LLM Key 配置

| 环境变量 | 说明 |
|---|---|
| `AGENTSCOPE_LLM_PROVIDER` | 如 `deepseek`、`glm` |
| `AGENTSCOPE_LLM_BASE_URL` | API 基础 URL |
| `AGENTSCOPE_LLM_API_KEY` | API 密钥 |
| `AGENTSCOPE_LLM_MODEL` | 如 `deepseek-chat`、`glm-4.7-flash` |

示例（DeepSeek）：

```bash
export AGENTSCOPE_LLM_PROVIDER=deepseek
export AGENTSCOPE_LLM_BASE_URL=https://api.deepseek.com/v1
export AGENTSCOPE_LLM_API_KEY=sk-xxxx
export AGENTSCOPE_LLM_MODEL=deepseek-chat
```

示例（GLM）：

```bash
export AGENTSCOPE_LLM_PROVIDER=glm
export AGENTSCOPE_LLM_BASE_URL=https://open.bigmodel.cn/api/paas/v4
export AGENTSCOPE_LLM_API_KEY=你的API密钥
export AGENTSCOPE_LLM_MODEL=glm-4.7-flash
```

**注意**：未配置 LLM key 时，support-agent 和 generic-observability-agent 会自动使用 Fake LLM 模式运行；real-agent-demo 没有 Fake LLM 回退。

---

## 3. 四个 Demo 的区别

### 3.1 通用 Agent 可观测演示 (`make demo-generic-observability-agent`)

**目标**：展示 Agent SDK 的语义埋点和 Deep Trace。

- 使用 deterministic mock 模式，**不要求 LLM key**
- 模拟完成后自动输出：
  - 登录邮箱/密码
  - 前端地址
  - 项目地址
  - Agent 列表地址
  - Run Detail 链接
  - PostgreSQL / ClickHouse 数据验证 SQL

**产生的语义轨迹**：

| 轨迹类型 | 说明 |
|---|---|
| thought | Agent 思考过程 |
| llm | LLM 调用（mock） |
| tool | 知识库搜索工具 |
| retrieval | 知识库检索 |
| skill | 答案合成 Skill |
| final_answer | 最终答案 |

**运行命令**：

```bash
make demo-generic-observability-agent
```

### 3.2 客服订单处理 Agent 演示 (`make demo-support-agent`)

**目标**：展示复杂多步骤 Agent 流程在平台中的可观测性。

- 模拟客服 Agent 处理四类问题：退款、物流、退换货政策、综合查询
- 使用 Fake LLM 或真实 LLM
- 展示 Agent 控制中心的在线状态和心跳

**产生的语义轨迹**：

| 轨迹类型 | 说明 |
|---|---|
| thought | Agent 意图分析和决策 |
| llm | LLM 调用（mock/真实） |
| tool | 订单查询工具 (lookup_order) |
| retrieval | 政策知识库检索 |
| skill | 回复生成 Skill |
| final_answer | 最终客服回复 |

**运行命令**：

```bash
make demo-support-agent
```

### 3.3 真实 LLM Agent 评测演示 (`make demo-real-agent`)

**目标**：展示真实 LLM 调用 + 评测闭环。

- **必须配置 LLM key**，无 mock 回退
- 创建数据集和实验
- 运行真实 Agent 评测
- 评测结果自动关联 Run 和 Trace

**运行命令**：

```bash
export AGENTSCOPE_LLM_PROVIDER=deepseek
export AGENTSCOPE_LLM_BASE_URL=https://api.deepseek.com/v1
export AGENTSCOPE_LLM_API_KEY=sk-xxxx
export AGENTSCOPE_LLM_MODEL=deepseek-chat
make demo-real-agent
```

### 3.4 多维评测演示 (`make demo-multi-eval`)

**目标**：展示多维度指标评测（包含权重和阈值）。

- 不要求 LLM key（但也可以配置 LLM Judge）
- 创建 Metric Definition（含权重）
- 创建 Multi Evaluator Config
- 运行实验，生成各维度分数

**可配置的 LLM Judge**：

```bash
export DEEPSEEK_API_KEY=sk-xxxx
make demo-multi-eval
```

**运行命令**：

```bash
make demo-multi-eval
```

---

## 4. 运行后访问哪些页面

### 关键页面地址

| 页面 | 地址 |
|---|---|
| 登录 | `http://127.0.0.1:5173/login` |
| 项目首页 | `http://127.0.0.1:5173/projects/{project_id}` |
| Agent 列表 | `http://127.0.0.1:5173/projects/{project_id}/agents` |
| Agent 详情 | `http://127.0.0.1:5173/projects/{project_id}/agents/{agent_id}` |
| Run 列表 | `http://127.0.0.1:5173/projects/{project_id}/runs` |
| Run Detail | `http://127.0.0.1:5173/runs/{run_id}` |
| Active Runs | `http://127.0.0.1:5173/projects/{project_id}/active-runs` |
| 实验报告 | `http://127.0.0.1:5173/experiments/{experiment_id}` |
| 配置中心 | `http://127.0.0.1:5173/projects/{project_id}/metric-config` |

### 演示完成后查看

1. **Agent 控制中心** — 查看已注册 Agent 的在线状态和实例
2. **Active Runs** — 查看正在运行的 Agent 任务
3. **Run Detail** — 查看完整的 Agent 时间线（thought → LLM → tool → retrieval → skill → answer）
4. **实验报告** — 查看评测结果、综合分和多维分数
5. **配置中心** — 查看和创建 Metric Definition

---

## 5. 数据库验证

### PostgreSQL 数据验证

```bash
# 连接 PostgreSQL
docker exec -it agentscope-postgres-1 psql -U agentscope -d agentscope_test

# 查看最近 runs
SELECT id, agent_name, status, trace_id, input_summary, output_summary
FROM runs
ORDER BY created_at DESC
LIMIT 20;

# 查看评测结果
SELECT run_id, score, passed, reason
FROM eval_case_results
ORDER BY created_at DESC
LIMIT 20;

# 查看多维评测分数
SELECT run_id, metric_name, metric_kind, score, passed, reason
FROM eval_scores
ORDER BY created_at DESC
LIMIT 20;

# 查看项目
SELECT id, name FROM projects ORDER BY created_at DESC;
```

### ClickHouse 数据验证

```bash
# 连接 ClickHouse
docker exec -it agentscope-clickhouse-1 clickhouse-client --database agentscope_traces

# 查看 span
SELECT run_id, name, kind, status_code
FROM trace_spans
ORDER BY start_time DESC
LIMIT 20;

# 查看 LLM 调用
SELECT run_id, provider, model, input_tokens, output_tokens
FROM trace_llm_calls
ORDER BY start_time DESC
LIMIT 10;

# 查看 thought
SELECT run_id, summary
FROM trace_thoughts
ORDER BY start_time DESC
LIMIT 10;

# 查看 final answer
SELECT run_id, answer_summary
FROM trace_final_answers
ORDER BY start_time DESC
LIMIT 10;

# 查看工具调用
SELECT run_id, tool_name, arguments_summary, result_summary
FROM trace_tool_calls
ORDER BY start_time DESC
LIMIT 10;
```

### 数据完整性验证脚本

```bash
# 对特定项目运行验证
./scripts/verify-demo-data.sh --project-id <project_id>

# 指定实验
./scripts/verify-demo-data.sh --project-id <project_id> --experiment-id <experiment_id>
```

---

## 6. 数据模型关系

```
Project
  ├─ Agent (注册的 Agent 定义)
  │   └─ AgentInstance (运行实例，带心跳)
  ├─ Dataset (数据集)
  │   └─ DatasetCase (测试用例: input/expected)
  ├─ Experiment (实验)
  │   └─ EvalJob (评测任务)
  │       └─ EvalCaseResult (每个用例的执行结果)
  │           ├─ score / passed / reason
  │           └─ run_id ─── Run (运行记录)
  │                            ├─ input_summary / output_summary / error
  │                            ├─ trace_id ─── Trace (OpenTelemetry 轨迹)
  │                            │                 └─ Span (单次操作)
  │                            │                    ├─ Thought (Agent 思考)
  │                            │                    ├─ LLM Call (LLM 调用)
  │                            │                    ├─ Tool Call (工具调用)
  │                            │                    ├─ Retrieval Call (检索)
  │                            │                    ├─ Skill Call (Skill 调用)
  │                            │                    └─ Final Answer (最终答案)
  │                            └─ EvalScore (多维评测分数)
  │                                ├─ metric_name / metric_kind
  │                                ├─ score [0,1] / passed / reason
  │                                └─ details (详细归因)
  └─ MetricDefinition (指标定义)
       ├─ name / kind
       ├─ weight (权重，用于综合分计算)
       └─ threshold (阈值)
```

---

## 7. 常见问题

### 403 错误

- 还未登录，或 session 过期
- 访问了不属于自己项目的资源
- Project Token 权限不足（scope 不匹配）

### API key 未配置

- support-agent / generic-observability-agent：自动使用 Fake LLM，功能不受影响
- real-agent-demo：直接失败，需要配置 LLM 环境变量

### 为什么没有某类 span

Agent 的 SDK 埋点是程序控制的。如果你的 Agent 没有执行 tool 调用或检索，就不会产生对应的 span。这是正常行为。

验证是否真的"没有"：

```sql
-- 检查 run_id 对应的 span
SELECT name, kind FROM trace_spans WHERE run_id='<run_id>';
```

### 为什么实验 pending

- Runner 未启动，或启动后未认领任务
- 检查 `make dev` 的后端是否在运行
- 运行 demo 脚本会自动启动 Runner

### 为什么看不到 score

- 实验未完成
- 未配置 evaluator 或 eveluator_config
- 多维评测需要 Metric Definition + Multi Evaluator Config
- 如果 score 显示 0 但没有 evaluator，说明是"0 分占位"，不是实际评测得分

---

## 8. 面试/答辩讲解路径

### 路径一：SDK 接入

1. `python/agentscope_sdk/runtime.py` — Agent 注册和心跳
2. `python/agentscope_sdk/tracing.py` — OTLP 导出和语义埋点
3. `python/agentscope_sdk/target.py` — 目标函数注册

### 路径二：Agent 在线状态

1. Agent 注册 → `POST /api/projects/{id}/agents`
2. 心跳 → `POST /api/agents/{id}/heartbeat`
3. Agent 控制中心页面 → Active/Idle 状态显示

### 路径三：实时运行可观测

1. Run 生命周期：`start_run` → `update_run_status` → `finish_run`
2. Active Runs 页面实时轮询更新
3. Run State Event 记录每次状态变迁

### 路径四：Deep Trace

1. OpenTelemetry OTLP HTTP 导出
2. ClickHouse 存储所有 span / event / semantic records
3. 前端 Timeline 展示 Agent 的思考、LLM 调用、工具使用
4. Run Detail 页面 → Agent Timeline → 每个节点的详细数据

### 路径五：多维评测

1. Metric Definition（含权重和阈值）
2. Multi Evaluator Config
3. Deterministic evaluator + LLM Judje 降级
4. 综合分 = 加权平均

### 路径六：数据库验证

1. PostgreSQL `runs` 表 → 业务状态
2. ClickHouse `trace_spans` 表 → 可观测数据
3. ClickHouse `trace_thoughts` → thought 明细
4. PostgreSQL `eval_scores` → 评测分数
5. `verify-demo-data.sh` 自动化验证

### 路径七：高并发采集设计

1. 基于 OpenTelemetry 标准协议，兼容全生态
2. BatchSpanProcessor 批处理导出
3. Rate limiter 按 project 限流
4. Gzip 压缩减少网络传输
5. ClickHouse 列式存储，适合时序聚合查询

---

## 9. API 参考

### 关键 API 端点

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/auth/register` | 用户注册 |
| POST | `/api/auth/login` | 用户登录 |
| GET | `/api/projects` | 项目列表 |
| POST | `/api/projects` | 创建项目 |
| POST | `/api/projects/{id}/tokens` | 创建 Project Token |
| GET | `/api/runs/{run_id}` | Run Detail (含 trace) |
| GET | `/api/projects/{id}/runs` | 项目 Run 列表 |
| GET | `/api/projects/{id}/experiments` | 实验列表 |
| POST | `/api/projects/{id}/experiments` | 创建实验 |
| GET | `/api/experiments/{id}` | 实验详情 |
| GET | `/api/projects/{id}/experiments/{id}/results` | 实验结果 |
| GET | `/api/projects/{id}/experiments/{id}/eval-scores` | 多维评测分数 |
| POST | `/api/projects/{id}/eval-scores` | 创建评测分数 |
| POST | `/api/projects/{id}/eval-scores/batch` | 批量创建评测分数 |
| POST | `/api/projects/{id}/metric-definitions` | 创建指标定义 |
| POST | `/api/projects/{id}/evaluators` | 创建 evalutor |
| POST | `/v1/traces` | OTLP trace 入口 |
| POST | `/api/sdk/runs/start` | SDK runtime 创建 run |
| POST | `/api/sdk/runs/{id}/state` | SDK runtime 更新状态 |
| POST | `/api/sdk/runs/{id}/finish` | SDK runtime 完成 run |
| GET | `/api/agents` | Agent 列表 |
| POST | `/api/projects/{id}/agents` | 注册 Agent |
| POST | `/api/agents/{id}/heartbeat` | Agent 心跳 |
| GET | `/healthz` | 健康检查 |

---

## 10. 架构图

```
┌─────────────────────────────────────────────────────────┐
│                     User / Demo                          │
│  (Python SDK / CLI / Browser)                            │
└──────────┬──────────┬───────────────┬────────────────────┘
           │          │               │
           ▼          ▼               ▼
┌──────────────────┐ ┌──────────┐ ┌──────────────────┐
│   HTTP API /     │ │  OTLP    │ │   React Web UI   │
│   SDK Runtime    │ │  Traces  │ │   (TypeScript)   │
└────────┬─────────┘ └────┬─────┘ └────────┬─────────┘
         │                │                │
         ▼                ▼                │
┌─────────────────────────────────────────┐│
│         Go 服务端 (Chi Router)           ││
│                                         ││
│  ┌───────────┐  ┌──────────────────┐   ││
│  │ Auth/     │  │ Runs Service     │   ││
│  │ Projects  │  │ + Runtime        │   ││
│  └───────────┘  └────────┬─────────┘   ││
│                          │              ││
│  ┌───────────┐  ┌────────▼─────────┐   ││
│  │ Datasets/ │  │ Ingest Handler   │   ││
│  │ Exp/Eval  │  │ (OTLP Decode)    │   ││
│  └───────────┘  └────────┬─────────┘   ││
│                          │              ││
└──────────────────────────┼──────────────┘
                           │
           ┌───────────────┼──────────────────┐
           ▼               ▼                   ▼
┌──────────────────┐ ┌────────────────┐ ┌──────────────────┐
│   PostgreSQL     │ │   ClickHouse   │ │   Python Runner  │
│   (业务数据)      │ │   (Trace/     │ │   (评测执行)      │
│   runs/agents/   │ │    Span/       │ │                   │
│   experiments/   │ │    Event)      │ │                   │
│   eval_scores/   │ │                │ │                   │
└──────────────────┘ └────────────────┘ └──────────────────┘
```
