# AgentScope 二阶段企业级 AgentOps 增量设计

日期：2026-07-18

## 一句话目标

AgentScope 二阶段要从“离线评测 + Demo Trace 展示”升级为企业级 AgentOps 平台：团队能实时看到所有接入 SDK 的 Agent 是否在线、正在执行什么任务、每次任务内部经历了哪些 LLM/tool/skill/RAG/memory/guardrail 步骤，并能基于线上 trace 持续评测 Agent 的结果质量、轨迹质量、工具使用质量、成本、延迟和安全风险。

## 背景与问题

当前一阶段已经打通了基础闭环：

- Python SDK 能执行真实 LLM Agent demo。
- Go 后端能接收 OTLP trace。
- ClickHouse 能保存 span 和 span events。
- PostgreSQL 能保存项目、数据集、实验、job、结果和 run 元信息。
- 前端能展示实验报告和 Run Trace。

但这仍然偏 demo 级，不能算企业级 Agent 可观测平台。主要缺口是：

- 无法回答“现在有哪些 Agent 在线、离线、运行中、异常”。
- 无法看到每个 Agent 实例当前正在处理什么任务。
- Trace 粒度不足，不能完整还原工具调用、skill 调用、知识库检索、memory、guardrail 和多 agent handoff。
- 评测仍以离线 dataset 为主，缺少线上 trace 抽样、自动评测、人工标注和回归数据集闭环。
- 当前 ingestion 仍是 Go 服务同步接收并写入，未来大量 Agent 高并发时需要 batch、队列、采样、rollup 和 ClickHouse 查询优化。

二阶段的设计目标不是重写一阶段，而是在现有 PostgreSQL + ClickHouse + Go API + Python SDK + React UI 的基础上增量演进。

## 外部参考结论

调研对象包括 OpenTelemetry GenAI、OpenInference、Langfuse、Phoenix、LangSmith、DeepEval、Ragas、OpenAI Agent Evals 和 AgentOps。

关键结论：

- OpenInference 把 trace 定义为从用户输入到 LLM、tool、retrieval 和最终输出的完整执行路径，span kind 包括 `AGENT`、`LLM`、`TOOL`、`RETRIEVER`、`EMBEDDING`、`CHAIN`。
- OpenTelemetry GenAI 正在标准化 LLM provider、model、prompt、response、token、tool call、retrieval documents 等 attributes；默认不采集完整 prompt/tool args，需要显式开启 content capture。
- Langfuse、Phoenix、LangSmith 都把线上 trace、评测、数据集、实验、人工标注连成闭环。
- DeepEval、Ragas、LangSmith AgentEvals 和 OpenAI trace grading 都强调 Agent 评测不能只看最终答案，还要评估轨迹、工具选择、工具参数、RAG groundedness 和任务完成度。
- 高并发可观测不应让业务请求阻塞在上报链路上，SDK 和 ingest 层需要 batch、queue、retry、sampling、quota、TTL 和 rollup。

设计上应尽量兼容 OpenInference / OpenTelemetry GenAI 的语义，不自造完全封闭的字段体系。

## 产品定位

二阶段后 AgentScope 的定位应改为：

> 面向企业 Agent 的在线可观测、调试、评测和持续质量治理平台。

它要覆盖四条用户路径：

1. **在线监控**：研发或平台团队打开 Agent 控制中心，看到所有 Agent、实例、在线状态、当前任务、错误率、延迟和成本。
2. **Trace 调试**：开发者从失败结果或线上异常跳到 Trace Explorer，还原每一步 LLM、tool、skill、RAG、memory 和 guardrail。
3. **质量评估**：评测负责人创建离线实验或线上评测规则，对最终答案、轨迹、工具、RAG、安全和效率打分。
4. **持续改进**：从线上 trace 抽样和人工标注生成 regression dataset，对 Agent 新版本做质量、成本、延迟对比。

## 产品边界

二阶段必须做：

- Agent 注册、版本、实例和心跳。
- Agent 在线、离线、运行中、异常状态展示。
- 当前 active runs 和最近 runs。
- 深度 trace span 语义模型，覆盖 LLM/tool/skill/RAG/memory/guardrail/handoff。
- SDK 层的 Agent 初始化、运行生命周期、心跳和深度 tracing helper。
- ClickHouse 结构化 trace 表和基础 rollup。
- 多维 Agent 评测模型。
- 线上 trace 抽样、评测结果保存、人工标注队列的基础能力。
- LangGraph RAG Agent 示例，证明平台不只支持简单 QA demo。

二阶段暂不做：

- SaaS 级计费系统。
- 完整企业 SSO / 复杂 RBAC。
- 自研模型网关。
- 完整日志平台。
- 完整 OpenTelemetry Collector 替代品。
- 复杂告警通知渠道矩阵。
- OpenHands 级代码 Agent 的完整托管执行。

## 总体架构

二阶段目标架构：

```text
Agent SDK
  ├─ init / register / heartbeat
  ├─ run lifecycle
  ├─ deep tracing helpers
  ├─ tool / skill / RAG instrumentation
  ├─ batch / retry / sampling / redaction
  ↓

Ingest API / Gateway
  ├─ token auth
  ├─ agent instance auth/cache
  ├─ quota / rate limit
  ├─ OTLP trace ingestion
  ├─ agent runtime events ingestion
  ├─ async/batch write boundary
  ↓

Storage
  ├─ PostgreSQL
  │   ├─ users / projects / tokens
  │   ├─ agents / versions / instances / heartbeats
  │   ├─ runs / active run state
  │   ├─ datasets / experiments / eval jobs / results
  │   └─ annotation queues / labels
  │
  └─ ClickHouse
      ├─ raw spans
      ├─ span events
      ├─ llm calls
      ├─ tool calls
      ├─ retrieval calls
      ├─ run metrics
      ├─ eval scores
      └─ rollup metrics
  ↓

Evaluation Engine
  ├─ offline eval
  ├─ online eval
  ├─ trace grading
  ├─ deterministic graders
  ├─ LLM-as-Judge
  └─ human annotation
  ↓

Frontend
  ├─ Agent Control Center
  ├─ Online Runs
  ├─ Trace Explorer
  ├─ Tool / RAG Analytics
  ├─ Evaluation Reports
  ├─ Annotation Queue
  └─ Regression Dashboard
```

## 模块一：Agent Control Center

Agent Control Center 是二阶段地基。它负责回答“现在有哪些 Agent、是否在线、由哪些实例运行、正在做什么”。

### 数据模型

PostgreSQL 新增：

`agents`

- `id UUID`
- `project_id UUID`
- `name TEXT`
- `description TEXT`
- `owner TEXT`
- `environment TEXT`
- `created_at TIMESTAMPTZ`
- `updated_at TIMESTAMPTZ`

`agent_versions`

- `id UUID`
- `agent_id UUID`
- `version TEXT`
- `git_sha TEXT`
- `runtime TEXT`
- `framework TEXT`
- `model_provider TEXT`
- `model_name TEXT`
- `created_at TIMESTAMPTZ`

`agent_instances`

- `id UUID`
- `agent_id UUID`
- `version_id UUID`
- `project_id UUID`
- `instance_name TEXT`
- `hostname TEXT`
- `process_id TEXT`
- `runtime TEXT`
- `environment TEXT`
- `status TEXT`
- `last_seen_at TIMESTAMPTZ`
- `started_at TIMESTAMPTZ`
- `stopped_at TIMESTAMPTZ`
- `metadata JSONB`

`agent_heartbeats`

- `id UUID`
- `agent_instance_id UUID`
- `status TEXT`
- `active_run_id UUID`
- `capacity INT`
- `inflight_runs INT`
- `cpu_percent NUMERIC`
- `memory_mb NUMERIC`
- `reported_at TIMESTAMPTZ`

`agent_capabilities`

- `id UUID`
- `agent_id UUID`
- `kind TEXT`
- `name TEXT`
- `version TEXT`
- `schema JSONB`
- `metadata JSONB`

`kind` 用于区分 `tool`、`skill`、`knowledge_base`、`model`、`memory`、`guardrail`。

### 状态规则

Agent 实例状态由 heartbeat 推导：

- `online`：最近心跳在阈值内，且没有 active run。
- `running`：最近心跳在阈值内，且有 active run。
- `degraded`：心跳存在，但错误率、队列或资源指标异常。
- `offline`：超过心跳阈值未上报。
- `stopped`：SDK 正常 shutdown。

心跳阈值默认 60 秒，可按项目配置。

### SDK 形态

Python SDK 新增：

```python
import agentscope

agentscope.init(
    project_token=os.environ["AGENTSCOPE_PROJECT_TOKEN"],
    server_url="http://127.0.0.1:8080",
    agent_name="support-agent",
    version="1.2.0",
    environment="dev",
    capabilities={
        "tools": [...],
        "skills": [...],
        "knowledge_bases": [...],
    },
)
```

SDK 行为：

- 启动时注册或获取 agent。
- 创建 agent instance。
- 后台定时 heartbeat。
- run 开始时更新 active run。
- run 结束/失败时清空 active run 并上报状态。
- shutdown 时上报 stopped。

## 模块二：在线运行状态

在线运行状态负责回答“Agent 当前正在跑什么”。

### Run 生命周期

`runs.status` 二阶段标准化为：

- `queued`
- `running`
- `waiting_llm`
- `waiting_tool`
- `waiting_retrieval`
- `retrying`
- `completed`
- `failed`
- `timeout`
- `cancelled`

新增 `run_state_events`：

- `id UUID`
- `run_id UUID`
- `agent_instance_id UUID`
- `from_status TEXT`
- `to_status TEXT`
- `message TEXT`
- `attributes JSONB`
- `created_at TIMESTAMPTZ`

高频状态变化仍可进入 ClickHouse；PostgreSQL 只保存 run 当前状态和关键历史。

### 前端页面

新增：

- Agent 列表页：在线数量、运行中数量、错误率、成本、延迟。
- Agent 详情页：实例列表、当前任务、最近 runs、最近错误、能力列表。
- Active Runs 页：所有运行中任务，支持按项目、Agent、状态、耗时筛选。

## 模块三：深度 Trace 语义模型

二阶段 trace 不再只是 OpenTelemetry 原始 span 展示，而要成为 Agent trajectory。

### Span Kind

内部统一 span kind：

- `AGENT`
- `LLM`
- `TOOL`
- `SKILL`
- `RETRIEVER`
- `EMBEDDING`
- `MEMORY`
- `GUARDRAIL`
- `EVALUATOR`
- `CHAIN`
- `HANDOFF`

这些名称尽量兼容 OpenInference。落库时保留原始 OTel attributes，同时抽取常用结构化字段。

### ClickHouse 表设计

保留 `trace_spans` 和 `trace_span_events` 作为 raw 层，新增结构化宽表：

`llm_calls`

- `project_id`
- `agent_id`
- `agent_instance_id`
- `run_id`
- `trace_id`
- `span_id`
- `provider`
- `model`
- `prompt_name`
- `input_tokens`
- `output_tokens`
- `total_tokens`
- `cost`
- `temperature`
- `stream`
- `status`
- `error`
- `start_time`
- `duration_ms`
- `input_summary`
- `output_summary`
- `content_captured Bool`

`tool_calls`

- `project_id`
- `agent_id`
- `run_id`
- `trace_id`
- `span_id`
- `tool_name`
- `tool_type`
- `arguments_summary`
- `result_summary`
- `success`
- `error`
- `retry_count`
- `start_time`
- `duration_ms`
- `side_effect_risk`

`retrieval_calls`

- `project_id`
- `agent_id`
- `run_id`
- `trace_id`
- `span_id`
- `knowledge_base`
- `query_text`
- `rewritten_query`
- `top_k`
- `returned_count`
- `document_ids Array(String)`
- `scores Array(Float64)`
- `start_time`
- `duration_ms`

`agent_run_metrics`

- `project_id`
- `agent_id`
- `agent_instance_id`
- `run_id`
- `trace_id`
- `status`
- `started_at`
- `ended_at`
- `duration_ms`
- `llm_call_count`
- `tool_call_count`
- `retrieval_call_count`
- `step_count`
- `error_count`
- `input_tokens`
- `output_tokens`
- `total_cost`
- `eval_score`

Raw 表用于完整调试，宽表用于 dashboard 和高频筛选。

### Content Capture 与脱敏

默认只采集摘要和 metadata，不采完整 prompt、tool arguments、tool results 和模型输出。

SDK 配置：

```python
agentscope.init(
    ...,
    capture_content=False,
    redact_pii=True,
    max_content_bytes=4096,
)
```

支持采集级别：

- `metadata_only`
- `summary`
- `full_content`

`full_content` 必须显式开启，并允许项目级禁用。

## 模块四：Tool / Skill / Knowledge Base 可观测

Tool、Skill、Knowledge Base 需要成为一等对象，而不是 attributes 字符串。

### SDK helper

```python
with agentscope.trace_tool("search_order", args={"order_id": order_id}):
    result = search_order(order_id)

with agentscope.trace_skill("refund_policy_check"):
    decision = skill.run(...)

with agentscope.trace_retrieval("kb-search", query=query, top_k=5):
    docs = retriever.search(query)
```

### UI

Agent 详情页增加：

- 工具列表和调用次数。
- 工具成功率和失败率。
- 慢工具排名。
- 知识库检索次数、命中率、低分召回样本。
- skill 调用链和失败样本。

## 模块五：Agent 质量评估体系

二阶段评测从 case result 升级为多层评测。

### 评测层级

1. **最终答案评测**
   - correctness
   - relevance
   - completeness
   - style
   - helpfulness
   - safety

2. **轨迹评测**
   - trajectory match
   - required steps
   - forbidden steps
   - loop detection
   - unnecessary step count
   - plan adherence

3. **工具评测**
   - tool selection correctness
   - argument correctness
   - tool result handling
   - side effect safety

4. **RAG 评测**
   - retrieval relevance
   - contextual recall
   - groundedness
   - faithfulness
   - citation correctness

5. **效率评测**
   - latency
   - token
   - cost
   - step count
   - retry count

6. **安全评测**
   - PII leakage
   - prompt injection susceptibility
   - policy violation
   - unsafe tool use

### 数据模型

新增：

`eval_metric_definitions`

- `id UUID`
- `project_id UUID`
- `name TEXT`
- `kind TEXT`
- `config JSONB`
- `created_at TIMESTAMPTZ`

`eval_scores`

- `id UUID`
- `project_id UUID`
- `run_id UUID`
- `trace_id TEXT`
- `span_id TEXT`
- `metric_name TEXT`
- `metric_kind TEXT`
- `score NUMERIC`
- `passed BOOL`
- `reason TEXT`
- `details JSONB`
- `evaluator_run_id UUID`
- `created_at TIMESTAMPTZ`

`metric_kind` 包括：

- `final_answer`
- `trajectory`
- `tool_correctness`
- `argument_correctness`
- `rag_groundedness`
- `safety`
- `efficiency`

### 评测方式

优先级：

1. deterministic grader：exact、regex、JSONPath、tool sequence、latency/cost threshold。
2. LLM-as-Judge：task completion、helpfulness、safety、trajectory reasonableness。
3. human annotation：线上高风险样本、争议样本、低置信样本。

LLM-as-Judge 必须记录 judge prompt、judge model、score、reason 和 evaluator trace，方便审计。

## 模块六：Online Eval 与 Annotation Queue

企业级 Agent 质量治理需要从线上 trace 反哺数据集。

### Online Eval 流程

```text
线上 run 完成
  -> sampling policy 判断是否评测
  -> online evaluator 读取 run + trace
  -> 写入 eval_scores
  -> 低分/异常样本进入 annotation queue
  -> 人工标注 corrected output / expected tool path
  -> 一键生成 dataset case
  -> 后续离线 regression experiment 使用
```

### 数据模型

`trace_sampling_rules`

- `id UUID`
- `project_id UUID`
- `agent_id UUID`
- `name TEXT`
- `enabled BOOL`
- `sample_rate NUMERIC`
- `filters JSONB`
- `created_at TIMESTAMPTZ`

`annotation_queues`

- `id UUID`
- `project_id UUID`
- `name TEXT`
- `description TEXT`
- `created_at TIMESTAMPTZ`

`annotation_items`

- `id UUID`
- `queue_id UUID`
- `run_id UUID`
- `trace_id TEXT`
- `status TEXT`
- `priority INT`
- `assigned_to UUID`
- `scores JSONB`
- `corrected_output TEXT`
- `expected_tool_path JSONB`
- `comment TEXT`
- `created_at TIMESTAMPTZ`
- `updated_at TIMESTAMPTZ`

## 模块七：高并发可观测架构

二阶段要为大量 Agent 并发预留架构，不要求一次做到超大规模，但不能继续按 demo 思路扩。

### SDK 层

必须支持：

- 异步上报。
- batch。
- retry。
- local queue。
- 最大队列长度。
- drop policy。
- shutdown flush。
- sampling。
- redaction。

业务 Agent 不应因为观测平台慢而阻塞。

### Ingest 层

Go ingest API 增强：

- gzip request。
- request size limit。
- project token cache。
- batch insert。
- per-project quota。
- ingest metrics。
- backpressure。
- dead-letter 预留。

短期继续 Go API 直写 ClickHouse。中期引入 OpenTelemetry Collector 作为可选部署：

```text
SDK -> OTel Collector -> AgentScope Ingest / ClickHouse
```

大规模时再拆：

```text
SDK -> edge collector -> gateway collector -> queue -> ClickHouse
```

### ClickHouse 层

需要：

- Raw 表按时间分区。
- `ORDER BY (project_id, agent_id, run_id, start_time)`。
- TTL 控制 raw 数据保留。
- materialized views 生成分钟级和小时级 rollup。
- dashboard 查询只查 rollup 或结构化宽表，避免扫 raw attributes。
- 对高基数字段做治理，避免把任意用户输入变成索引维度。

## API 增量设计

新增 Agent API：

```text
POST /api/projects/{projectID}/agents
GET  /api/projects/{projectID}/agents
GET  /api/agents/{agentID}
GET  /api/agents/{agentID}/instances
GET  /api/agents/{agentID}/runs
GET  /api/agents/{agentID}/metrics
```

新增 SDK runtime API：

```text
POST /sdk/agents/register
POST /sdk/agent-instances/register
POST /sdk/agent-instances/{instanceID}/heartbeat
POST /sdk/runs/start
POST /sdk/runs/{runID}/state
POST /sdk/runs/{runID}/end
POST /sdk/runs/{runID}/fail
```

新增 Trace API：

```text
GET /api/runs/active
GET /api/runs/{runID}/trace
GET /api/runs/{runID}/llm-calls
GET /api/runs/{runID}/tool-calls
GET /api/runs/{runID}/retrievals
```

新增评测 API：

```text
POST /api/projects/{projectID}/eval-metrics
GET  /api/runs/{runID}/eval-scores
POST /api/runs/{runID}/evaluate
POST /api/projects/{projectID}/sampling-rules
GET  /api/projects/{projectID}/annotation-queues
POST /api/annotation-queues/{queueID}/items
POST /api/annotation-items/{itemID}/promote-to-dataset
```

## 前端信息架构

二阶段前端主导航建议：

- Projects
- Agents
- Online Runs
- Traces
- Evaluations
- Annotation
- Settings

Agent 列表页重点展示：

- Agent 名称。
- 环境。
- 在线实例数。
- 当前运行数。
- 近 1 小时成功率。
- p95 延迟。
- token / cost。
- 最近错误。

Trace Explorer 重点展示：

- Session / Turn / Run 层级。
- Span timeline。
- LLM calls。
- Tool calls。
- Retrieval calls。
- Memory / skill / guardrail。
- Errors。
- Eval scores。

Evaluation Dashboard 重点展示：

- Agent 版本对比。
- 多指标分数。
- 失败样本。
- 低分 trace。
- 工具错误。
- RAG groundedness 趋势。
- 成本和质量 tradeoff。

## 与现有一阶段的兼容策略

一阶段的 `runs` 继续作为核心业务锚点，不废弃。

兼容策略：

- 旧 `trace_spans` / `trace_span_events` 保留为 raw trace 表。
- 新结构化表从 OTLP attributes 抽取生成，不要求旧数据补齐。
- 旧 experiment / eval result 继续可用。
- 新 eval_scores 可逐步挂到 run / span 上。
- 旧 Python demo 继续运行，但新增 SDK init 后会显示在线 Agent。

## 分阶段实施计划

### Phase 2.1：Agent Control Center

目标：能看到所有接入 SDK 的 Agent 是否在线、有哪些实例、当前是否运行。

交付：

- PostgreSQL agents / versions / instances / heartbeats / capabilities。
- SDK `agentscope.init()`。
- SDK heartbeat loop。
- 后端 agent 注册和心跳 API。
- Agent 列表和 Agent 详情页。
- real-agent demo 在页面显示在线。

### Phase 2.2：Active Runs 与运行状态

目标：能看到当前所有正在运行的任务和状态变化。

交付：

- run lifecycle 标准化。
- run start/end/fail/state API。
- SDK run lifecycle helper。
- active runs 页面。
- 卡住/超时基础检测。

### Phase 2.3：Deep Trace 语义模型

目标：Trace 能展示 LLM、tool、skill、RAG、memory、guardrail。

交付：

- span_kind 抽取。
- ClickHouse llm_calls / tool_calls / retrieval_calls。
- SDK trace helper。
- Run Detail 页面升级。
- content capture / redaction 配置。

### Phase 2.4：LangGraph RAG Agent 示例

目标：证明平台支持复杂 Agent，不只是 QA demo。

交付：

- `examples/langgraph_rag_agent.py`。
- 工具调用。
- 知识库检索。
- 多 step trajectory。
- 前端展示 RAG 和 tool trace。

### Phase 2.5：Agent 多维评测

目标：评测 Agent 整体质量。

交付：

- eval_metric_definitions。
- eval_scores。
- final answer evaluator。
- tool correctness evaluator。
- trajectory evaluator。
- RAG groundedness evaluator。
- 报告多指标展示。

### Phase 2.6：Online Eval 与 Annotation Queue

目标：从线上 trace 形成持续质量闭环。

交付：

- sampling rules。
- online evaluator。
- annotation queue。
- corrected output。
- promote to dataset。
- regression dataset。

### Phase 2.7：高并发与生产化

目标：支持大量 Agent 并发上报。

交付：

- SDK batch / retry / local queue。
- ingest gzip / batch / quota。
- ClickHouse rollup / TTL。
- dashboard 查询优化。
- OTel Collector 可选部署文档。

## 风险与取舍

### 风险一：范围过大

二阶段不应一次性交付所有模块。必须从 Agent Control Center 开始，建立在线状态主线，再逐步加 deep trace 和 eval。

### 风险二：字段体系自造过多

Trace 字段应优先兼容 OpenInference / OpenTelemetry GenAI。自定义字段放在 `agentscope.*` namespace，避免未来无法接外部生态。

### 风险三：采集内容带来安全问题

完整 prompt、tool args、retrieval docs、response 都可能包含敏感信息。默认只采摘要，完整内容必须显式开启，并支持 redaction 和项目级禁用。

### 风险四：高并发查询拖垮 raw spans

Dashboard 不应扫 raw attributes。高频页面必须使用结构化宽表和 rollup。

### 风险五：LLM-as-Judge 不稳定

评测系统要优先 deterministic grader，再使用 LLM-as-Judge。LLM judge 输出必须可审计、可复跑、可人工校准。

## 成功标准

二阶段完成后，项目应该能演示：

1. 启动多个使用 SDK 的 Agent，前端能看到在线实例。
2. 某个 Agent 正在执行任务时，Online Runs 页面实时显示状态。
3. 一个 LangGraph RAG Agent run 能展示 LLM call、tool call、retrieval documents 和最终输出。
4. 实验报告不仅有最终答案分数，还能展示 tool correctness、trajectory score 和 RAG groundedness。
5. 线上低分 trace 能进入 annotation queue，并被提升为 regression dataset case。
6. 在较高并发 demo 下，span ingestion 不阻塞 Agent 主流程，ClickHouse dashboard 查询仍可接受。

## 下一步

下一步不直接写全部二阶段代码，而是先写 Phase 2.1 的详细实施计划：

- Agent Control Center 数据库迁移。
- Go service 和 API。
- Python SDK `init()` 和 heartbeat。
- React Agent 页面。
- demo 更新。
- 测试和验证方式。

Phase 2.1 完成后，再进入 Deep Trace 语义模型和 LangGraph RAG Agent 示例。
