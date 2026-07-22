# Phase 3 AgentOps 平台化与开源 Agent 接入预留实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `using-git-worktrees` before implementation, then use `test-driven-development` for every code task and `verification-before-completion` before claiming completion. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 根据 `docs/可观测.md` 的 Agent 评估与可观测方法论，把 AgentScope 从“可跑 demo 的 Agent 可观测/评测系统”升级为“可接入任意生产 Agent 的通用 AgentOps 平台”，并为后续接入 OpenClaw、OpenHands、Hermes 这类完整开源 Agent 预留稳定协议、适配层和验收路径。

**Architecture:** 平台主线继续沿用现有 Go API + PostgreSQL + ClickHouse + Python SDK + React/Vite/Ant Design 架构，不引入新的重型基础设施。下一阶段先补齐通用 Agent 接入协议、实时运行观测、深度语义 trace、可配置 Rubric 评测、trace 到 dataset 的闭环和生产治理能力；开源 Agent 接入只作为平台通用性的验证手段，不把平台功能写死到某一个 demo Agent。

**Tech Stack:** Go、PostgreSQL/sqlc、ClickHouse、Python SDK/OpenTelemetry、React/Vite/Ant Design、React Query、Vitest、Go test、pytest、Docker Compose。

## Global Constraints

- 所有提交信息必须使用中文。
- 开发必须在独立 git worktree 中完成，不直接污染主工作区。
- 每个任务至少一个独立 commit，避免“大而全”提交。
- 每个实现任务必须先写失败测试，再实现，再运行验证。
- 实现完成后必须跑通完整流程，并验证数据库数据是否正确，不能只看页面或命令成功。
- 平台能力必须面向任意 Agent，不允许只针对客服 demo、enterprise demo 或某一个开源 Agent 写死逻辑。
- SDK 默认不采集完整 prompt、工具参数、用户隐私内容；完整内容采集必须显式开启并经过脱敏。
- 本阶段实时能力优先用轮询补齐；SSE/WebSocket 可预留接口但不作为第一批强依赖。
- 开源 Agent 接入优先作为验收和反向约束：Hermes、OpenHands、OpenClaw 都应能通过同一套 runtime contract 接入。

---

## 1. `docs/可观测.md` 对平台的核心要求

`docs/可观测.md` 不是单纯讲 Trace UI，它给的是一套完整 Agent 工程方法论。下一阶段平台必须围绕这些要求补齐能力。

### 1.1 评估对象不是模型，而是 Agent + Harness

平台不能只保存“模型回答是否正确”。一个 Agent 的质量来自模型、提示词、工具、skill、RAG、memory、guardrail、执行协议和环境状态的组合。

平台需要支持：

- 同一个 Agent 不同版本的对比。
- 同一个 Harness 换模型的 model swap 对比。
- 同一个模型关闭某个 Harness 组件的 ablation 对比。
- 每次评测都能追溯到 Agent 版本、模型、工具集合、prompt 版本、配置和数据集版本。

### 1.2 Trace 是可观测、调试和评测资产

Trace 不只是页面上的 span tree。它应该成为三类资产：

- 调试资产：复盘一次 Agent 运行的完整路径。
- 评测资产：从 trace 中抽取最终答案、工具轨迹、RAG 命中、安全风险和成本。
- 数据资产：线上失败或可疑 trace 脱敏后沉淀为回归数据集。

平台需要支持：

- trace 到 annotation queue。
- annotation 到 dataset case。
- dataset case 到 regression experiment。
- experiment 再关联回原始 trace。

### 1.3 评测要覆盖轨迹和结果

`docs/可观测.md` 明确区分 trajectory 和 outcome。只看最终答案会漏掉“说对但工具乱用”，只看轨迹会漏掉“做到了但表达不好”。

平台需要支持：

- 最终答案正确性。
- 工具选择正确性。
- 工具参数正确性。
- RAG groundedness。
- 轨迹合理性。
- 安全合规和一票否决项。
- 成本、延迟、步数、重试、冗余动作。
- Pass@k、Pass^k、Best@k。

### 1.4 LLM-as-Judge 必须 Rubric 化和可校准

平台不能只提供一个自由文本 judge prompt。Rubric 应该结构化，包含维度、权重、评分档、陷阱项、否决项、边界案例和评判理由。

平台需要支持：

- Rubric 模板。
- 结构化 Rubric 配置。
- LLM Judge 运行记录。
- 多 Judge 聚合。
- 人工金标校准。
- 分歧样本进入人工标注队列。

### 1.5 生产级平台必须支持内部评估基础设施

`docs/可观测.md` 提到的 OpenClaw 内部评估基础设施给了明确方向：消融、AB、特性开关、提示词版本、隐私感知分析。

平台需要支持：

- feature flag / ablation metadata。
- prompt version snapshot。
- model/harness/config compare。
- 基线统计和显著性提示。
- 隐私感知采集和脱敏审计。

---

## 2. 当前平台已有能力

根据当前代码，AgentScope 已经具备二阶段基础：

- Python SDK 支持 `init`、agent 注册、心跳、run lifecycle。
- Python SDK 有 `trace_llm`、`trace_tool`、`trace_retrieval`、`trace_skill`、`trace_thought`、`record_final_answer` 等语义 helper。
- 后端有 `/api/sdk/runs/start`、state、finish、fail。
- 后端有 `/v1/traces` OTLP ingest。
- PostgreSQL 保存项目、token、agent、instance、run、dataset、experiment、metric、annotation、sampling 等元数据。
- ClickHouse 保存 raw spans、span events、LLM/tool/retrieval/skill/thought/final answer 等语义表。
- 前端已有 Agent 列表、Active Runs、Run Detail、Ingest Dashboard、Metric Config、Annotation Queues、Experiment Detail。
- demo 已覆盖 real agent、support agent、multi eval、generic observability、enterprise agentops。

这说明下一阶段不需要重启架构，重点是把已有模块从“能用”补成“可信、通用、可配置、可复用”。

---

## 3. 当前主要缺口

### 3.1 通用 Agent 接入契约还不够清晰

当前 SDK 能注册 Agent，但平台还没有一份稳定的 runtime contract 来约束所有 Agent：

- run、trace、session、conversation、task 的身份边界不够产品化。
- capabilities 缺少统一 schema。
- framework/runtime/source 缺少标准字段。
- 外部 Agent 事件映射到 AgentScope 语义 trace 的规则还没有固化。

这会影响后续接入 OpenClaw、OpenHands、Hermes。不同 Agent 事件模型不同，如果没有统一 contract，很容易每接一个项目就写一套临时逻辑。

### 3.2 实时可观测还偏轮询结果展示

已有 active runs 和 runtime detail，但用户真正想看的包括：

- 这个 Agent 当前是否在线。
- 当前正在处理哪个任务。
- 当前卡在 LLM、工具、检索、等待用户、重试还是错误恢复。
- 当前耗时、成本、token、工具调用数。
- 最近一次状态变更是什么。

这些信息需要形成统一的“实时运行视图”，而不只是分散在 Agent、Run、Trace 页面。

### 3.3 Trace 展示缺少诊断视角

当前能展示 span tree 和语义块，但还不够像调试平台：

- 缺少按时间线统一串起 thought、LLM、tool、retrieval、skill、guardrail、final answer。
- 缺少错误、重试、耗时异常、成本异常的突出展示。
- 缺少从 trace 直接进入 annotation/evaluation 的操作。
- 缺少对工具轨迹、RAG 命中、prompt/output 摘要的结构化对比。

### 3.4 评测配置还不够 Rubric 化

已有 metric definitions、eval scores、evaluator，但是：

- MetricDefinition 类型信息偏薄，前端类型里也没有完整暴露 weight、threshold、rubric、veto 等配置。
- LLM-as-Judge 的 Rubric 还没有产品化编辑和结果解释。
- 多 Judge、人工校准、分歧处理还没有闭环。
- Pass@k、Pass^k、模型替换、消融和 A/B 还没有实验模型。

### 3.5 Trace 到数据集闭环还不够顺滑

已有 annotation queue 和 promote-to-dataset，但产品路径需要更明确：

- 从 Run Detail 选择一个 trace 加入标注队列。
- 标注员修正 expected output、expected tool path、风险标签、失败原因。
- 一键生成 dataset case。
- 新 dataset case 自动关联来源 trace。
- 回归实验能展示“这个 case 来自哪次线上失败”。

### 3.6 生产治理和高并发采集仍需强化

已有 batch、redaction、ingest stats 方向，但下一阶段应继续补：

- 项目级 ingest quota。
- SDK 本地队列可观测。
- drop/retry/fail 统计。
- ClickHouse rollup 和 TTL 策略。
- 采集健康页能解释“为什么没有数据”。
- 内容采集、脱敏、promote dataset 前二次脱敏的明确策略。

---

## 4. Phase 3 目标产品形态

Phase 3 完成后，用户路径应该是：

1. 用户创建项目，生成 Project Token。
2. 用户在任意 Agent 中接入 Python SDK 或 adapter。
3. Agent 启动后在控制中心显示在线、实例、能力、版本、framework、runtime。
4. Agent 处理任务时，Active Runs 页面实时显示状态。
5. 用户进入 Run Detail，看到完整时间线：
   - 用户输入
   - 思考摘要
   - LLM 调用
   - 工具调用
   - RAG 检索
   - memory read/write
   - skill 调用
   - guardrail
   - final answer
   - token、成本、耗时、错误
6. 用户配置 Rubric 和多维评测方案。
7. 用户用 dataset 离线评测，也可以从线上 trace 抽样自动评测。
8. 用户把失败 trace 加入人工标注，修正后沉淀为 dataset。
9. 用户用同一数据集对比 Agent 版本、模型、prompt 或 feature flag。
10. 平台输出质量、轨迹、工具、RAG、安全、成本、延迟的对比报告。

---

## 5. 工作流拆分

### Workstream A: 通用 Agent Runtime Contract

**目标:** 让任何 Agent 都能用同一套字段和生命周期接入平台。

**Files:**

- Modify: `python/agentscope_sdk/runtime.py`
- Modify: `python/agentscope_sdk/client.py`
- Modify: `python/agentscope_sdk/__init__.py`
- Modify: `internal/httpapi/agent_handlers.go`
- Modify: `internal/httpapi/sdk_runtime_handlers.go`
- Modify: `internal/store/agents.sql.go`
- Modify: `queries/agents.sql`
- Modify: `web/src/api/types.ts`
- Test: `python/tests/test_runtime_contract.py`
- Test: `internal/httpapi/agent_handlers_test.go`
- Test: `internal/httpapi/sdk_runtime_handlers_test.go`

**Deliverables:**

- `AgentRuntimeMetadata` 标准字段：
  - `agent_name`
  - `agent_version`
  - `framework`
  - `runtime`
  - `environment`
  - `source`
  - `capabilities`
  - `session_id`
  - `conversation_id`
  - `task_id`
- capabilities 统一 schema：
  - `kind`: `tool`、`skill`、`knowledge_base`、`memory`、`guardrail`、`model`
  - `name`
  - `version`
  - `schema`
  - `metadata`
- run lifecycle 标准状态：
  - `queued`
  - `running`
  - `thinking`
  - `waiting_llm`
  - `waiting_tool`
  - `waiting_retrieval`
  - `waiting_memory`
  - `waiting_user`
  - `retrying`
  - `completed`
  - `failed`
  - `cancelled`
  - `timeout`

**Steps:**

- [ ] 写 Python 测试，验证 `agent_init` 可以提交 framework/runtime/source/capabilities/session metadata。
- [ ] 写 Go handler 测试，验证 agent register/upsert 保存这些字段。
- [ ] 写 SDK run 测试，验证 run start payload 带 `session_id`、`task_id`、`conversation_id`。
- [ ] 实现 runtime metadata dataclass。
- [ ] 扩展 client request/response 类型。
- [ ] 扩展后端请求结构和 store 查询。
- [ ] 扩展前端类型和 Agent Detail 展示。
- [ ] 运行 `cd python && python -m pytest`。
- [ ] 运行 `go test ./...`。
- [ ] 运行 demo，验证 `agents`、`agent_instances`、`runs` 表字段正确。
- [ ] 提交：`feat: 标准化 Agent Runtime 接入契约`

---

### Workstream B: 实时 Agent 控制中心

**目标:** 平台能回答“所有 Agent 当前是否在线、正在做什么、哪里异常”。

**Files:**

- Modify: `internal/httpapi/agent_handlers.go`
- Modify: `internal/httpapi/run_handlers.go`
- Modify: `internal/store/agents.sql.go`
- Modify: `internal/store/runs.sql.go`
- Modify: `queries/agents.sql`
- Modify: `queries/runs.sql`
- Modify: `web/src/features/agents/AgentsPage.tsx`
- Modify: `web/src/features/agents/AgentDetailPage.tsx`
- Modify: `web/src/features/runs/ActiveRunsPage.tsx`
- Modify: `web/src/api/hooks.ts`
- Modify: `web/src/api/types.ts`
- Test: `internal/httpapi/agent_handlers_test.go`
- Test: `web/src/features/agents/agents.test.tsx`
- Test: `web/src/features/runs/active-runs.test.tsx`

**Deliverables:**

- Agent 列表展示：
  - 在线状态
  - 实例数
  - running runs
  - 最近心跳
  - p95 latency
  - error rate
  - total cost
- Agent Detail 展示：
  - instances
  - capabilities
  - active runs
  - recent runs
  - recent errors
- Active Runs 展示：
  - 当前阶段
  - 当前 span 摘要
  - runtime duration
  - last state event
  - trace link

**Steps:**

- [ ] 写后端测试，构造多个 agent instance，验证 online/running/offline 推导正确。
- [ ] 写后端测试，验证 Agent summary 能聚合 recent runs、active runs、error count。
- [ ] 写前端测试，验证 Agent 列表能展示在线、运行中、异常。
- [ ] 扩展 SQL 查询和 handler response。
- [ ] 改造 AgentsPage 为控制中心视图。
- [ ] 改造 AgentDetailPage，增加实例、能力、active runs、recent errors。
- [ ] 改造 ActiveRunsPage，突出运行阶段和 trace 跳转。
- [ ] 运行 `go test ./internal/httpapi ./internal/store`。
- [ ] 运行 `cd web && npm test -- --run`。
- [ ] 跑 `make demo-enterprise-agentops`，验证运行时页面能看到 active run。
- [ ] 查询 PostgreSQL 验证 agent instance 和 run 状态一致。
- [ ] 提交：`feat: 完善实时 Agent 控制中心`

---

### Workstream C: Trace Explorer 诊断化

**目标:** Run Detail 从“span tree 展示”升级成“可调试的 Agent 时间线”。

**Files:**

- Modify: `web/src/features/runs/RunDetailPage.tsx`
- Modify: `web/src/features/runs/RunRuntimeDetailPage.tsx`
- Modify: `web/src/features/runs/SpanTree.tsx`
- Create: `web/src/features/runs/AgentTimeline.tsx`
- Create: `web/src/features/runs/TraceDiagnostics.tsx`
- Modify: `web/src/api/types.ts`
- Test: `web/src/features/runs/runs.test.tsx`
- Test: `web/src/features/runs/active-runs.test.tsx`

**Deliverables:**

- 统一时间线卡片：
  - thought
  - llm
  - tool
  - retrieval
  - skill
  - memory
  - guardrail
  - final_answer
- 诊断摘要：
  - 慢 span
  - failed span
  - retry
  - high cost
  - missing final answer
  - missing run_id / trace_id
- 每个语义块显示：
  - 输入摘要
  - 输出摘要
  - duration
  - tokens/cost
  - error
  - content captured 状态

**Steps:**

- [ ] 写前端测试，输入包含 LLM/tool/retrieval/final_answer 的 RunDetail fixture，验证时间线顺序正确。
- [ ] 写前端测试，验证 failed tool 和慢 LLM 会出现在诊断摘要。
- [ ] 新建 `AgentTimeline.tsx`，把不同语义表合并为统一 timeline item。
- [ ] 新建 `TraceDiagnostics.tsx`，基于 run detail 计算诊断信号。
- [ ] 改造 RunDetailPage，默认展示“时间线 + 诊断”，span tree 保留为高级视图。
- [ ] 改造 RunRuntimeDetailPage，运行中也能显示已到达的语义事件。
- [ ] 运行 `cd web && npm test -- --run web/src/features/runs`。
- [ ] 跑 `make demo-enterprise-agentops`，手工验证 Run Detail 时间线包含 thought、LLM、tool、retrieval、skill、final answer。
- [ ] 查询 ClickHouse 验证页面展示数量与语义表一致。
- [ ] 提交：`feat: 将 Trace 详情升级为 Agent 时间线`

---

### Workstream D: Rubric 化多维评测

**目标:** 按 `docs/可观测.md` 要求，把评测从简单 metric 扩展为用户可配置、可解释、可校准的 Rubric 体系。

**Files:**

- Modify: `internal/evals/service.go`
- Modify: `internal/evaluators/service.go`
- Modify: `internal/httpapi/evals_handlers.go`
- Modify: `internal/httpapi/evaluator_handlers.go`
- Modify: `queries/evals.sql`
- Modify: `internal/store/evals.sql.go`
- Modify: `web/src/features/evals/MetricConfigPage.tsx`
- Modify: `web/src/features/experiments/ExperimentDetailPage.tsx`
- Modify: `python/agentscope_sdk/evaluators.py`
- Modify: `python/agentscope_sdk/judges.py`
- Modify: `python/agentscope_sdk/multi_judges.py`
- Test: `internal/evals/service_test.go`
- Test: `internal/evaluators/service_test.go`
- Test: `web/src/features/experiments/experiments.test.tsx`
- Test: `python/tests/test_rubric_evaluators.py`

**Deliverables:**

- MetricDefinition 支持：
  - `weight`
  - `pass_threshold`
  - `rubric`
  - `veto`
  - `judge_provider`
  - `judge_model`
  - `calibration_required`
- Rubric 结构：
  - dimensions
  - scoring levels
  - essential items
  - optional items
  - pitfall items
  - veto items
  - examples
  - edge cases
- Experiment report 展示：
  - 综合分
  - 维度分
  - veto 命中
  - judge reason
  - per-case 诊断

**Steps:**

- [ ] 写 Go service 测试，验证 weight 必须在 `[0,1]`，threshold 必须在 `[0,1]`。
- [ ] 写 Go service 测试，验证 veto metric failed 时综合结果 failed。
- [ ] 写 Python evaluator 测试，验证 Rubric judge 输出能解析成结构化维度分。
- [ ] 写前端测试，验证 MetricConfigPage 可以创建 Rubric metric。
- [ ] 扩展数据库 migration 和 sqlc query。
- [ ] 扩展 eval service 的结构化配置校验。
- [ ] 扩展 Python evaluator，支持 weighted + veto。
- [ ] 改造 ExperimentDetailPage，展示 Rubric 维度和综合分。
- [ ] 跑 `go test ./internal/evals ./internal/evaluators ./internal/httpapi`。
- [ ] 跑 `cd python && python -m pytest tests/test_rubric_evaluators.py`。
- [ ] 跑 `cd web && npm test -- --run web/src/features/experiments`。
- [ ] 跑 `make demo-enterprise-agentops`，验证评测结果不是固定 1.0，而是来自 Rubric 加权。
- [ ] 查询 PostgreSQL `eval_scores`，验证每个 run 有多维分数。
- [ ] 提交：`feat: 支持 Rubric 化多维评测`

---

### Workstream E: Trace 到 Dataset 的闭环

**目标:** 将线上运行中发现的问题沉淀成回归测试资产。

**Files:**

- Modify: `internal/annotation/service.go`
- Modify: `internal/httpapi/annotation_handlers.go`
- Modify: `internal/httpapi/run_handlers.go`
- Modify: `queries/annotation_queues.sql`
- Modify: `internal/store/annotation_queues.sql.go`
- Modify: `web/src/features/runs/RunDetailPage.tsx`
- Modify: `web/src/features/evals/AnnotationQueuesPage.tsx`
- Modify: `web/src/features/datasets/CasesTable.tsx`
- Modify: `web/src/api/hooks.ts`
- Modify: `web/src/api/types.ts`
- Test: `internal/annotation/service_test.go`
- Test: `internal/httpapi/annotation_handlers_test.go`
- Test: `web/src/features/evals/annotation-queues.test.tsx`

**Deliverables:**

- Run Detail 一键加入标注队列。
- Annotation item 保存：
  - source_run_id
  - source_trace_id
  - input_summary
  - actual_output
  - corrected_output
  - expected_tool_path
  - failure_category
  - privacy_reviewed
- Promote dataset case 时保存 source metadata。
- Dataset case 页面显示来源 trace。

**Steps:**

- [ ] 写 annotation service 测试，验证从 run 创建 annotation item 会复制 input/output/trace metadata。
- [ ] 写 promote 测试，验证 dataset case metadata 包含 source_run_id/source_trace_id。
- [ ] 写前端测试，验证 Run Detail 有“加入标注队列”操作。
- [ ] 扩展 annotation item 数据模型。
- [ ] 增加 `POST /api/runs/{runID}/annotation-items` 或等价 project-scoped API。
- [ ] 改造 AnnotationQueuesPage，支持编辑 expected output、tool path、failure category。
- [ ] 改造 CasesTable，展示来源 trace。
- [ ] 跑 `go test ./internal/annotation ./internal/httpapi`。
- [ ] 跑 `cd web && npm test -- --run annotation`。
- [ ] 手工从 enterprise demo 的一个失败/低分 run 生成 annotation，再 promote 到 dataset。
- [ ] 查询 PostgreSQL 验证 dataset case metadata 可追溯到原 run。
- [ ] 提交：`feat: 打通 Trace 标注到数据集闭环`

---

### Workstream F: 对比实验、消融与 Prompt 版本

**目标:** 对齐 `docs/可观测.md` 中“从 Benchmark 报告到系统改进”和“内部评估基础设施”的要求。

**Files:**

- Create: `internal/experiments/compare.go`
- Modify: `internal/httpapi/experiments_handler.go`
- Modify: `internal/store/models.go`
- Modify: `queries/experiments.sql`
- Modify: `web/src/features/experiments/ExperimentDetailPage.tsx`
- Create: `web/src/features/experiments/ExperimentComparePage.tsx`
- Modify: `python/agentscope_sdk/runtime.py`
- Test: `internal/httpapi/experiment_handlers_test.go`
- Test: `web/src/features/experiments/experiments.test.tsx`
- Test: `python/tests/test_experiment_metadata.py`

**Deliverables:**

- 每个 run/experiment 保存：
  - model provider/name/version
  - prompt version
  - harness version
  - feature flags
  - ablation flags
  - dataset version
- 支持两个 experiment 对比：
  - pass rate
  - avg score
  - dimension score
  - p50/p95 latency
  - avg cost
  - changed cases
- 基础统计提示：
  - 样本量
  - 分差
  - 是否需要复跑

**Steps:**

- [ ] 写 Python 测试，验证 SDK start_run metadata 可包含 prompt/harness/feature flags。
- [ ] 写 Go 测试，验证 experiment compare API 返回维度聚合差异。
- [ ] 写前端测试，验证 compare page 展示两个实验的通过率和维度差异。
- [ ] 扩展 run metadata 和 experiment metadata。
- [ ] 实现 compare service。
- [ ] 增加 compare API。
- [ ] 增加 ExperimentComparePage。
- [ ] 跑 `go test ./internal/httpapi ./internal/store`。
- [ ] 跑 `cd web && npm test -- --run experiments`。
- [ ] 用同一 dataset 跑两个配置，验证 compare API 返回正确。
- [ ] 提交：`feat: 支持实验对比与消融元数据`

---

### Workstream G: 生产采集治理

**目标:** 为大量 Agent 高并发接入做基础治理，避免 SDK/ingest/ClickHouse 成为黑盒。

**Files:**

- Modify: `python/agentscope_sdk/tracing.py`
- Modify: `python/agentscope_sdk/redact.py`
- Modify: `internal/httpapi/ingest_handlers.go`
- Modify: `internal/httpapi/ingest_stats_handlers.go`
- Modify: `internal/tracestore/clickhouse.go`
- Modify: `clickhouse/migrations/00001_trace.sql`
- Modify: `web/src/features/ingest/IngestDashboard.tsx`
- Test: `python/tests/test_tracing_batch.py`
- Test: `internal/httpapi/ingest_handlers_test.go`
- Test: `internal/httpapi/ingest_stats_handlers_test.go`
- Test: `internal/tracestore/clickhouse_test.go`

**Deliverables:**

- SDK 暴露本地采集配置：
  - batch size
  - queue size
  - retry
  - timeout
  - capture content
  - redaction
  - sampling
- Ingest stats 展示：
  - spans/events/semantic records
  - ingest errors
  - dropped spans
  - top agents by error
  - top agents by latency
  - missing run_id/trace_id count
- ClickHouse rollup：
  - 15m / 1h project stats
  - agent stats
  - cost/token stats

**Steps:**

- [ ] 写 Python 测试，验证 SDK 默认 redaction 和 capture_content=false。
- [ ] 写 Go 测试，验证 ingest stats 在 ClickHouse 无数据时返回可解释空状态，不返回 500。
- [ ] 写 ClickHouse 测试，验证 rollup 查询按 project/agent 聚合正确。
- [ ] 扩展 SDK 采集配置。
- [ ] 扩展 ingest error 统计。
- [ ] 增加 ClickHouse rollup 查询。
- [ ] 改造 IngestDashboard，让用户能判断“没数据、上报失败、run_id 缺失、ClickHouse 异常”的区别。
- [ ] 跑 `cd python && python -m pytest tests/test_tracing_batch.py`。
- [ ] 跑 `go test ./internal/httpapi ./internal/tracestore`。
- [ ] 跑 `make demo-enterprise-agentops`，验证 Ingest Dashboard 有数据。
- [ ] 查询 ClickHouse 验证 rollup 和原始表数量一致。
- [ ] 提交：`feat: 完善生产采集治理`

---

### Workstream H: 开源 Agent Adapter 预留

**目标:** 不立即完整接入 OpenClaw/OpenHands/Hermes，但先把平台接口设计成能接这些真实 Agent。

**Files:**

- Create: `python/agentscope_sdk/adapters/__init__.py`
- Create: `python/agentscope_sdk/adapters/base.py`
- Create: `python/agentscope_sdk/adapters/events.py`
- Create: `docs/integrations/open-source-agents.md`
- Test: `python/tests/test_adapter_events.py`

**Deliverables:**

- Adapter base protocol：
  - `on_agent_start`
  - `on_run_start`
  - `on_thought`
  - `on_llm_call`
  - `on_tool_call`
  - `on_retrieval`
  - `on_memory_read`
  - `on_memory_write`
  - `on_skill_call`
  - `on_guardrail`
  - `on_run_finish`
  - `on_run_error`
- Event normalization：
  - 外部 event 转 AgentScope semantic trace。
- 接入说明文档：
  - Hermes：memory、skill、tool、自我改进事件如何映射。
  - OpenHands：event log、runtime action、shell/file/browser action 如何映射。
  - OpenClaw：always-on assistant、multi-channel、tool/sandbox/action 如何映射。

**External References:**

- OpenHands event system: https://docs.openhands.dev/sdk/arch/events
- OpenHands runtime architecture: https://docs.openhands.dev/openhands/usage/architecture/runtime
- Hermes Agent repository: https://github.com/nousresearch/hermes-agent
- Hermes Agent site: https://hermes-agent.nousresearch.com/
- OpenClaw repository: https://github.com/openclaw/openclaw

**Steps:**

- [ ] 写 Python 测试，验证外部 tool event 能 normalize 成 `trace_tool` 参数。
- [ ] 写 Python 测试，验证外部 memory event 能 normalize 成 semantic event。
- [ ] 实现 adapter base protocol。
- [ ] 实现 event normalization dataclasses。
- [ ] 编写 `docs/integrations/open-source-agents.md`，说明三个开源 Agent 的接入策略。
- [ ] 跑 `cd python && python -m pytest tests/test_adapter_events.py`。
- [ ] 提交：`feat: 预留开源 Agent 适配层`

---

## 6. 推荐执行顺序

不要并行乱做。建议按下面顺序推进：

1. Workstream A：先标准化 runtime contract。
2. Workstream B：再补实时控制中心。
3. Workstream C：把 trace detail 变成调试工具。
4. Workstream D：升级 Rubric 多维评测。
5. Workstream E：打通 trace 到 dataset 闭环。
6. Workstream F：做实验对比、消融和 prompt version。
7. Workstream G：补生产采集治理。
8. Workstream H：最后做开源 Agent adapter 预留。

如果要拆给多个 agent 并行：

- A 必须先做，其他任务依赖 A 的 contract。
- B 和 C 可以在 A 后并行。
- D 和 E 可以在 C 稳定后并行。
- F 依赖 D。
- G 可以与 B/C/D 并行，但合并时必须跑完整 demo。
- H 依赖 A/C，不依赖 D/F。

---

## 7. 完整验收标准

Phase 3 不能只看测试通过，必须跑通真实流程。

### 7.1 自动化验证

```bash
go test ./...
cd python && python -m pytest
cd ../web && npm test -- --run
npm run build
```

### 7.2 Demo 验证

```bash
make demo-enterprise-agentops
make demo-generic-observability-agent
make demo-multi-eval
```

每个 demo 完成后必须验证：

- 前端项目页能打开。
- Agent 列表能看到在线/运行状态。
- Run Detail 有 Agent 时间线。
- Trace 语义表有数据。
- Eval scores 有多维数据。
- Experiment report 能展示综合分和维度分。

### 7.3 PostgreSQL 验证

至少查询：

```sql
SELECT id, name, status, updated_at FROM agents ORDER BY updated_at DESC LIMIT 10;
SELECT id, agent_name, status, trace_id, input_summary, output_summary, latency_ms
FROM runs ORDER BY created_at DESC LIMIT 20;
SELECT metric_kind, count(*), avg(score)
FROM eval_scores GROUP BY metric_kind ORDER BY metric_kind;
SELECT id, status, source_run_id, source_trace_id
FROM annotation_items ORDER BY created_at DESC LIMIT 10;
```

期望：

- 本轮 demo 的 Agent 有正确 name/version/runtime。
- 本轮 run 没有异常遗留 `running`。
- 每个完成 run 至少有 trace_id。
- 多维 eval scores 数量和实验用例数量匹配。
- promote dataset 的 annotation item 能追溯原始 run/trace。

### 7.4 ClickHouse 验证

至少查询：

```sql
SELECT run_id, count() FROM trace_spans GROUP BY run_id ORDER BY count() DESC LIMIT 10;
SELECT run_id, count() FROM trace_llm_calls GROUP BY run_id ORDER BY count() DESC LIMIT 10;
SELECT run_id, count() FROM trace_tool_calls GROUP BY run_id ORDER BY count() DESC LIMIT 10;
SELECT run_id, count() FROM trace_retrieval_calls GROUP BY run_id ORDER BY count() DESC LIMIT 10;
SELECT run_id, count() FROM trace_skill_calls GROUP BY run_id ORDER BY count() DESC LIMIT 10;
SELECT run_id, count() FROM trace_final_answers GROUP BY run_id ORDER BY count() DESC LIMIT 10;
```

期望：

- demo run 对应的 semantic trace 表均有合理数据。
- duration 不应大面积为 0，除非确实是同步瞬时逻辑。
- run_id 不应大量为 `00000000-0000-0000-0000-000000000000`。
- trace_id 与 PostgreSQL runs.trace_id 可关联。

---

## 8. 开源 Agent 接入策略

接入开源 Agent 不是为了展示“我们又写了一个 Agent”，而是验证平台通用性。

### 8.1 Hermes 优先级最高

Hermes 适合验证：

- long-running agent。
- memory read/write。
- skill 创建和调用。
- tool 调用。
- 自我改进循环。
- 用户长期会话。

Hermes 接入验收：

- 不重写 Hermes 主逻辑。
- 在 Hermes 原有 tool/skill/memory/LLM 调用位置埋点。
- AgentScope 能看到 Hermes 在线。
- Hermes 每次任务产生 run 和 trace。
- Trace Detail 能展示 memory、skill、tool、LLM、final answer。
- 从 Hermes 失败 trace 能进入 annotation queue。

### 8.2 OpenHands 第二优先

OpenHands 适合验证：

- 代码 Agent。
- Docker runtime。
- shell/file/browser action。
- 长任务事件流。
- 高风险工具调用。

OpenHands 接入验收：

- 通过 adapter 映射 OpenHands event log。
- shell/file action 映射为 tool call。
- runtime/container 信息进入 run metadata。
- 文件变更、命令错误、重试能在 trace 中体现。
- 长任务 active run 不丢状态。

### 8.3 OpenClaw 第三优先

OpenClaw 适合验证：

- always-on personal assistant。
- 多入口 channel。
- 长期在线状态。
- 本地设备/沙盒/权限风险。
- 多任务并发。

OpenClaw 接入验收：

- channel/session 信息进入 metadata。
- 本地工具和权限风险进入 tool/guardrail trace。
- 长期在线心跳稳定。
- 采集健康页能看到持续 ingest。

---

## 9. 不做事项

Phase 3 暂不做：

- SaaS 计费。
- 企业 SSO。
- 完整 RBAC。
- 自研模型网关。
- Kafka/NATS 采集管道。
- 完整告警通知矩阵。
- 自动训练或 RL 仿真环境。
- 完整托管 OpenHands/OpenClaw/Hermes 运行环境。

这些不是不重要，而是会分散当前“通用 AgentOps 平台化”的主线。

---

## 10. 最终交付形态

Phase 3 完成后，AgentScope 应该可以这样介绍：

> AgentScope 是一个面向生产 Agent 的可观测与评测平台。用户通过 Python SDK 或开源 Agent adapter 接入任意 Agent，平台实时展示 Agent 在线状态和运行中任务，记录 LLM、工具、RAG、memory、skill、guardrail 和最终答案的完整语义轨迹；用户可以基于数据集和线上 trace 配置 Rubric 化多维评测，把失败 trace 标注后沉淀为回归集，并对比不同 Agent 版本、模型、prompt 和 Harness 配置的质量、成本与延迟。

这才是 `docs/可观测.md` 中“观察 → 假设 → 实验 → 验证 → 新认识 → 新假设”闭环在当前项目里的落地形态。
