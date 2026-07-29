# Online Eval & Annotation Queue Demo

本文档说明如何从零开始运行 AgentScope 的线上评测和标注队列功能。

## 前置条件

- Go 1.26.5
- Python 3.11+
- uv 0.11.4
- Node.js `^20.19.0` 或 `>=22.12.0`
- Docker (PostgreSQL + ClickHouse)
- LLM API Key 仅在启用 LLM Judge 时需要

## 环境准备

```bash
# 安装 Python SDK 与前端依赖
cd python && pip install -e . && cd ..
cd web && npm ci && cd ..
```

## 配置

```bash
# 复制环境变量模板并编辑
cp .env.example .env
# 编辑 .env，尤其是 AGENTSCOPE_SESSION_SECRET；然后导出配置
set -a
. ./.env
set +a
```

关键环境变量：

| 变量 | 说明 | 必填 |
|------|------|------|
| `AGENTSCOPE_DATABASE_URL` | PostgreSQL 连接字符串 | 是 |
| `AGENTSCOPE_CLICKHOUSE_DSN` | ClickHouse 连接字符串 | 是 |
| `AGENTSCOPE_SESSION_SECRET` | 至少 32 字节的 Session 签名密钥 | 是 |
| `AGENTSCOPE_SESSION_TTL` | Session 服务端有效期，默认 `168h` | 否 |
| `AGENTSCOPE_SECURE_COOKIES` | HTTPS 部署必须设为 `true` | 否 |
| `OPENAI_API_KEY` | OpenAI Judge API Key | 使用 OpenAI Judge 时 |
| `ANTHROPIC_API_KEY` | Anthropic Judge API Key | 使用 Anthropic Judge 时 |

## 启动服务

### 终端 1：Go 服务端

```bash
make dev
# 后端监听 :8080，前端监听 :5173；同时启动依赖并应用全部 migration
```

## 运行流程

### 1. 注册用户和创建项目

```bash
# 注册用户
curl -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"demo@test.com","password":"demo123","display_name":"Demo User"}'

# 创建项目
curl -X POST http://localhost:8080/api/projects \
  -H "Content-Type: application/json" \
  -H "Cookie: session=<session_cookie>" \
  -d '{"name":"Demo Project"}'

# 创建 Project Token（用于 SDK 鉴权）
curl -X POST http://localhost:8080/api/projects/<project_id>/tokens \
  -H "Content-Type: application/json" \
  -H "Cookie: session=<session_cookie>" \
  -d '{"description":"demo-token","scopes":["otlp:write","agent:register","agent:runtime"]}'
```

### 2. 创建抽样规则

通过 API 创建抽样规则，控制哪些线上运行的 Agent 会被自动评测：

```bash
# 100% 采样 - 所有运行都评测
curl -X POST http://localhost:8080/api/projects/<project_id>/sampling-rules \
  -H "Content-Type: application/json" \
  -H "Cookie: session=<session_cookie>" \
  -d '{"name":"全面评测","sample_rate":1.0}'

# 50% 采样 - 仅失败运行
curl -X POST http://localhost:8080/api/projects/<project_id>/sampling-rules \
  -H "Content-Type: application/json" \
  -H "Cookie: session=<session_cookie>" \
  -d '{"name":"失败采样","sample_rate":0.5,"filters":"{\"has_error\":true}"}'

# 100% 采样 - 特定 Agent
curl -X POST http://localhost:8080/api/projects/<project_id>/sampling-rules \
  -H "Content-Type: application/json" \
  -H "Cookie: session=<session_cookie>" \
  -d '{"name":"Agent-A 评测","sample_rate":1.0,"agent_id":"<agent_id>"}'
```

或通过前端页面：项目工作台 → 线上评测抽样规则

### 3. 运行一个最小 Python Agent，并确保 Trace 在终态前落库

> `agent:register` 用于 `agentscope.init()` 注册 Agent，`agent:runtime` 用于 Run 生命周期，`otlp:write` 用于 Trace 导出。没有 Sampling Rule 时，Job 会被标记为 `skipped`，而不是产生评测分数。

```bash
# 使用上一步创建后仅显示一次的完整 Project Token。
export AGENTSCOPE_PROJECT_TOKEN="ascope_<your_project_token>"
python - <<'PYCODE'
import os
import agentscope_sdk as agentscope

tracer = agentscope.configure_tracing(
    "http://127.0.0.1:8080",
    os.environ["AGENTSCOPE_PROJECT_TOKEN"],
    service_name="online-eval-demo",
    export_batch_size=1,
)
runtime = agentscope.init(
    server_url="http://127.0.0.1:8080",
    project_token=os.environ["AGENTSCOPE_PROJECT_TOKEN"],
    agent_name="online-eval-demo",
)
try:
    # Run 和 OTLP Span 使用同一个 trace_id，服务端才能关联执行事实。
    with tracer.start_as_current_span("agent.run") as root:
        trace_id = f"{root.get_span_context().trace_id:032x}"
        run = agentscope.start_run(input="请概述退款流程", trace_id=trace_id)
        if run is None:
            raise RuntimeError("启动 run 失败")
        with agentscope.trace_tool(
            tracer, "lookup_refund_policy", tool_type="function",
            result_summary="退款需联系客服提交申请", success=True,
        ):
            pass
        # 先 flush，再 finish：降低 Worker 读取 ClickHouse 时 Trace 尚未到达的概率。
        agentscope.force_flush()
        if not agentscope.finish_run(output="请联系客服提交退款申请。", latency_ms=25):
            raise RuntimeError("完成 run 失败")
        print(f"run 已完成: {run.id}")
finally:
    agentscope.shutdown_runtime()
    agentscope.shutdown_tracing()
PYCODE
```

### 4. 查看线上评测结果

Run 终态、terminal state event 和 `online_eval_jobs` 会在**同一个 PostgreSQL 事务**中落库；HTTP 返回成功时，系统已拥有可恢复的评测任务。Worker 异步执行昂贵工作，并会周期性回收过期租约。

1. Worker 根据 Sampling Rule 决定是否抽样；未命中时 Job 终态为 `skipped`。
2. 命中时 Worker 从 ClickHouse 读取 Trace evidence，写入 `eval_scores`。
3. 低分、失败、低置信或高分歧 Run 自动创建 annotation item。
4. Job 可见状态包括 `queued`、`running`、`retry_wait`、`succeeded`、`skipped`、`dead_letter`；手工重试会开启新的 attempt cycle。

#### A. Trace 启发式运行健康评分（`trace_heuristic`）

该评测器用于发现**执行异常和证据缺失**，不是 LLM Judge：

- `efficiency`：Run 完成状态；
- `final_answer`：output 是否存在；
- `tool_correctness`：Tool 调用成功比例；
- `trajectory`：是否观察到 ERROR span；
- `rag_groundedness`：是否观察到 retrieval evidence；
- `safety`：是否记录运行时错误。

因此它**不验证**最终答案正确性、RAG 事实性/引用忠实度或策略安全。每一条 score 的 `reason` 都会标出 “Trace heuristic (execution evidence only)”；页面也会以“Trace 运行健康启发式”展示来源。语义质量应使用离线/在线的 Rule、Rubric + LLM Judge 或人工 Gold。

#### B. Baseline Fallback（`baseline_fallback`）

当 Trace evaluator 不可用或未返回结果时，系统仅写入 `baseline_completion_status`。它只描述完成状态，不代表回答质量。

### 5. 查看标注队列

访问项目工作台 → 标注队列

- 选择队列查看需人工标注的项目
- 每项包含：run_id, trace_id, 评测分数, 原始输出
- 可编辑：
  - **修正输出**（corrected_output）：修正后的预期输出
  - **预期工具路径**（expected_tool_path）：期望的工具调用
  - **备注**（comment）：标注说明

### 6. 提升为数据集用例

编辑标注后，点击"提升"按钮：

1. 选择目标数据集
2. 自动生成 dataset case：
   - input = 原始 run 的 input
   - expected = corrected_output
   - metadata = { run_id, trace_id, eval_scores, expected_tool_path }
3. 防止重复提升（status 变为 promoted 后不可再次提升）

### 7. 离线回归实验

提升后的数据集用例可用于传统的离线实验：

```bash
curl -X POST http://localhost:8080/api/projects/<project_id>/experiments \
  -H "Content-Type: application/json" \
  -H "Cookie: session=<session_cookie>" \
  -d '{
    "dataset_id": "<dataset_id>",
    "target_name": "my-agent",
    "case_ids": ["<case_id_1>", "<case_id_2>"]
  }'
```

## 架构图

```text
Agent SDK
  ├─ OTLP Trace → ClickHouse（异步批写）
  └─ finish / fail
       │
       ▼
PostgreSQL 单事务
  ├─ 更新 Run 终态
  ├─ 写入 terminal state event
  ├─ 清理 active instance link
  └─ UPSERT online_eval_jobs
       │
       ▼
Online Eval Worker（可多进程）
  ├─ 周期回收 stale lease
  ├─ Claim：worker_id + lease_token
  ├─ Sampling Rule
  ├─ Trace 启发式运行健康评分 / baseline fallback
  └─ 同事务写入 score、Job 成功态、annotation item
       │
       ▼
Human Review → Gold → Dataset Case → Offline Experiment / Judge / Comparison
```

**租约规则：**Job 的 `Complete`、`Skip`、`Retry` 必须同时校验 `worker_id + lease_token`。过期 Worker 即使晚到，也不能覆盖已重新领取任务的新 Worker 结果。

## API 参考

### 抽样规则

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/api/projects/{projectID}/sampling-rules` | 创建规则 |
| GET | `/api/projects/{projectID}/sampling-rules` | 列出规则 |
| GET | `/api/sampling-rules/{ruleID}` | 获取规则 |
| PATCH | `/api/sampling-rules/{ruleID}` | 更新规则（启用/禁用） |
| DELETE | `/api/sampling-rules/{ruleID}` | 删除规则 |

### 标注队列

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/api/projects/{projectID}/annotation-queues` | 创建队列 |
| GET | `/api/projects/{projectID}/annotation-queues` | 列出队列 |
| GET | `/api/annotation-queues/{queueID}/items` | 列出标注项 |
| PATCH | `/api/annotation-items/{itemID}` | 更新标注 |
| POST | `/api/annotation-items/{itemID}/promote-to-dataset` | 提升为用例 |

### 创建抽样规则请求体

```json
{
  "name": "50% 采样",
  "sample_rate": 0.5,
  "agent_id": "<optional_agent_id>",
  "filters": "{\"has_error\":true,\"status\":\"failed\"}"
}
```

### 更新标注请求体

```json
{
  "status": "completed",
  "corrected_output": "修正后的输出",
  "expected_tool_path": "[{\"tool\":\"search\",\"args\":{\"q\":\"test\"}}]",
  "comment": "标注说明"
}
```

### 提升为数据集用例请求体

```json
{
  "dataset_id": "<dataset_id>"
}
```

## 数据库表关系

```
projects
  ├── trace_sampling_rules  (project_id → 抽样规则)
  ├── runs                  (project_id → Agent 运行记录)
  │     └── eval_scores     (run_id → 评测分数)
  ├── annotation_queues     (project_id → 标注队列)
  │     └── annotation_items (queue_id → 标注项, run_id → run)
  └── datasets              (project_id → 数据集)
        └── dataset_cases   (dataset_id → 测试用例)
```
