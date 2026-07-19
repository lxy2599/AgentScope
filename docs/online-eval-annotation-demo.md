# Online Eval & Annotation Queue Demo

本文档说明如何从零开始运行 AgentScope 的线上评测和标注队列功能。

## 前置条件

- Go 1.22+
- Python 3.10+
- Docker (PostgreSQL + ClickHouse)
- 至少一个 LLM API Key（OpenAI / Anthropic）

## 环境准备

```bash
# 1. 启动数据库
make db-up

# 2. 运行数据库迁移（Goose 或直接执行 SQL）
go run tools/run-migrations/main.go

# 3. 安装 Go 依赖
go mod download

# 4. 安装 Python SDK 依赖
cd python && pip install -e . && cd ..

# 5. 安装前端依赖
cd web && npm install && cd ..
```

## 配置

```bash
# 复制环境变量模板并编辑
cp .env.example .env
```

关键环境变量：

| 变量 | 说明 | 必填 |
|------|------|------|
| `DATABASE_URL` | PostgreSQL 连接字符串 | 是 |
| `CLICKHOUSE_DSN` | ClickHouse 连接字符串 | 是 |
| `SESSION_SECRET` | Session 签名密钥 | 是 |
| `OPENAI_API_KEY` | LLM Judge API Key | 评测必填 |
| `ANTHROPIC_API_KEY` | Anthropic API Key | 评测备选 |

## 启动服务

### 终端 1：Go 服务端

```bash
make server
# 监听在 :8080
```

### 终端 2：前端开发服务器（可选）

```bash
cd web && npm run dev
# 监听在 :5173
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
  -d '{"description":"demo-token","scopes":["otlp:write","runner:write","agent:runtime"]}'
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

### 3. 运行 Python Agent Demo

```bash
cd python

# 配置访问令牌
export AGENTSCOPE_API_KEY="ascope_<your_project_token>"
export AGENTSCOPE_BASE_URL="http://localhost:8080"

# 运行 Agent（需要提供一个 OpenAI API Key）
export OPENAI_API_KEY="sk-..."
python -m agentscope_sdk.examples.simple_agent \
  --project-id <project_id>
```

### 4. 查看线上评测结果

当 Agent run 完成后：

1. 服务端自动检查抽样规则
2. 如果规则匹配，Dispatcher 触发异步线上评测
3. 评测有两种模式：

   **A. Configured Evaluator（默认）— 真实多维评测**
   - 使用 `TraceAwareEvaluator`，加载 ClickHouse trace 数据
   - 基于真实 trace 信息对每个 metric kind 打分：
     - `efficiency` — 从 run status 判断完成状态
     - `final_answer` — 检查 output 是否存在
     - `tool_correctness` — 统计 trace tool call 的通过率
     - `trajectory` — 基于 span 数据分析轨迹合理性
     - `rag_groundedness` — 检查 retrieval call 记录和返回文档
     - `safety` — 检查 run error 和安全风险
   - 如果某个维度缺少 trace 数据，返回明确说明原因（如 "trace data unavailable for trajectory evaluation"），不会用 run status 伪装成该维度的评分

   **B. Baseline Fallback — 完成状态基线分**
   - 仅生成 `baseline_completion_status` 指标，metric_kind 为 `efficiency`
   - metric_name/reason 明确标注为基线分：`"No online evaluator configured; baseline status score only"`
   - 不会生成 tool_correctness/trajectory/rag_groundedness 等伪指标
   - 在没有配置 evaluator 或 evaluator 返回空结果时自动启用

4. 评测结果写入 `eval_scores` 表
5. 低分或失败的 run 自动创建 annotation item

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

```
Agent Run 完成
     │
     ▼
[SDK Runtime Handler]
     │
     ├── FinishRun / FailRun (同步)
     │
     └── OnlineEvalDisp.DispatchRun (异步 goroutine)
              │
              ▼
     [Sampling Rule Check]
         │           │
     不匹配         匹配
         │           │
       结束          ▼
    [OnlineEvaluator 接口]
         │               │
         ▼               ▼
[TraceAwareEvaluator]   [未配置或无结果]
   (加载 ClickHouse          │
    trace 数据)              ▼
         │           [Baseline Fallback]
         ▼           (仅 completion_status)
   ┌─────────────────┐
   │ 6 个维度评分:    │
   │ efficiency      │
   │ final_answer    │
   │ tool_correctness│
   │ trajectory      │
   │ rag_groundedness│
   │ safety          │
   └─────────────────┘
         │
         ▼
   [Create EvalScore]
         │
         ▼
[Check: Low Score / Failed?]
    │              │
   否             是
    │              │
  结束            ▼
      [Create Annotation Item]
           │
           ▼
    [Human Review]
           │
           ▼
   [Promote to Dataset]
```

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
