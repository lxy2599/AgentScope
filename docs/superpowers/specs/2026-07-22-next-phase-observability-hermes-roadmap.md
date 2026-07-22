# AgentScope 下一阶段规划：生产级可观测评估闭环与 Hermes 接入

日期：2026-07-22

## 一句话目标

根据 `docs/可观测.md` 的方法论，AgentScope 下一阶段要从“能跑 demo、能展示 trace、能做基础多维评测”升级为“生产级 Agent 可观测与评估闭环平台”：平台要能接入真实开源 Agent，实时观察它的运行状态，完整记录 LLM、工具、记忆、技能、RAG、安全和最终输出，并把线上真实 trace 回流为评估集，用多维 Rubric、人工标注、版本对比和成本延迟分析驱动 Agent 持续改进。

## 为什么现在要进入这个阶段

当前项目已经完成了可观测与评测的基础骨架：

- Python SDK 可以注册 Agent、上报 heartbeat、创建 run、导出 trace。
- Go 后端可以接收 OTLP 和 SDK runtime 数据。
- PostgreSQL 保存项目、用户、token、Agent、run、dataset、experiment、eval result。
- ClickHouse 保存 span 和 thought、LLM、tool、retrieval、skill、final answer 等语义 trace。
- 前端有 Agent 列表、Active Runs、Run Detail、实验报告、Metric 配置和采集健康页面。
- 企业 demo 已经能串起 Agent 注册、trace、dataset、multi-eval 和数据库验证。

但这些能力仍然偏“平台自写 demo”。`docs/可观测.md` 对生产级 Agent 评估提出的标准更高：

- 评估对象不是单个模型，而是“模型 + Harness + 工具 + 记忆 + 知识库 + 安全策略”的组合体。
- 评测不能只看最终答案，还要看执行轨迹和系统最终状态。
- 可观测数据最有价值的去向，是从线上 trace 回流成 regression dataset。
- 自动 LLM-as-Judge 必须配合 Rubric、人工抽检、金标集和多评委校准。
- 模型和 Agent 版本切换要用统计意义上的对比，而不是凭一次 demo 的分数。
- 生产平台必须处理数据量、隐私、实时防护、成本和高并发采集。

因此下一阶段的核心不是“再做一个 demo”，而是把当前平台推进到真实外部 Agent 接入和持续质量治理。

## 下一阶段产品定位

下一阶段后，AgentScope 的定位应变成：

> 面向真实 Agent 产品的在线可观测、Trace 调试、质量评估、数据回流和版本决策平台。

平台必须回答这些问题：

- 现在有哪些 Agent 在线、离线、运行中、异常？
- 某个 Agent 当前正在执行什么任务？
- 一次 run 内部经历了哪些思考、LLM 调用、工具调用、记忆读写、技能执行、RAG 检索、安全判断和最终输出？
- 失败是最终答案错了，还是中间轨迹走偏了？
- 哪些线上失败或可疑案例应该进入回归评估集？
- 新模型、新提示词、新工具或新记忆策略是否真的更好？
- 新方案提升的质量是否值得额外成本和延迟？
- 自动评判是否经过人工标注或金标集校准？

## 总体路线

下一阶段建议命名为：

**Phase 3：生产级 AgentOps 闭环**

它由 7 个模块组成：

1. 真实外部 Agent 接入：Hermes Agent。
2. 实时运行与 Trace Explorer v2。
3. 深度语义 Trace 补齐：memory、guardrail、handoff、scheduler、code execution。
4. Evaluation Profile 与 Rubric 配置中心。
5. Trace 回流 Dataset 与人工标注闭环。
6. 实验对比、回归报告和成本延迟分析。
7. 统一端到端 demo 与数据验收体系。

## 模块一：接入真实外部 Agent：Hermes Agent

### 目标

接入 `NousResearch/hermes-agent`，让它成为 AgentScope 的长期可观测对象。

GitHub：

- https://github.com/NousResearch/hermes-agent

Hermes 是合适目标，因为它是完整 Agent，而不是框架或玩具 demo。它原生具备：

- CLI、messaging gateway、TUI、Electron desktop app。
- 同一个 agent core 支撑多入口运行。
- terminal、browser、code execution、tools。
- programmatic tool calling。
- 长期记忆、会话搜索、用户画像。
- autonomous skill creation 和 skill self-improvement。
- scheduled automations / cron。
- subagents / parallel workstreams。
- 多 LLM provider。
- plugin system 和 memory providers。

这些能力基本覆盖 `docs/可观测.md` 中强调的生产 Agent 关键维度：工具、记忆、技能、长期运行、成本、轨迹、可回放、评估回流。

### 原则

必须明确：

- 不是重新开发 Agent。
- 不是写一个 Hermes-like demo。
- 不是只模拟 Hermes 的工具和记忆。
- 必须跑 Hermes 原生命令。
- 必须让 Hermes 原有 agent core 执行任务。
- 必须在 Hermes 原有链路里埋点。

### 接入方式

优先使用 Hermes plugin：

```text
plugins/agentscope_observability/
  plugin.py
  hooks.py
  README.md
```

插件负责：

- 初始化 AgentScope SDK。
- 注册 `hermes-agent`。
- 注册 instance 和 capabilities。
- 接入 session start/end。
- 接入 LLM call。
- 接入 tool dispatch。
- 接入 code execution。
- 接入 memory provider。
- 接入 skill executor。
- 接入 scheduler job。
- 接入 final response。
- 异常时关闭 run 并记录 error span。

如果 Hermes plugin hook 覆盖不到关键路径，允许最小 core patch，但 patch 只能调用 plugin hook，不能复制或改写 Hermes 业务逻辑。

### Hermes 任务种子

任务种子只负责触发 Hermes 原生能力：

- terminal 工具任务：列目录、读文件、总结仓库结构。
- code execution 任务：写并运行 Python 脚本分析小 CSV。
- memory 任务：记住用户偏好，再在下一轮读取。
- skill 任务：把任务流程沉淀为可复用技能。
- scheduler 任务：创建一次短周期定时报告。
- browser/search 任务：查 Hermes plugin 文档并总结。
- safe failure 任务：执行不存在命令并给出排查建议。

这些任务必须通过 Hermes 原生入口执行，不能由我们自写 Agent 完成。

### 验收

新增命令：

```bash
make demo-hermes-agentops
```

该命令必须最终给出：

- demo 登录邮箱和密码。
- project id。
- Hermes agent id。
- 至少 5 个 run id。
- 至少 1 个 experiment id。
- PostgreSQL 查询结果摘要。
- ClickHouse 查询结果摘要。
- 前端 Agent 链接。
- 前端 Run Detail 链接。
- 前端 Experiment 链接。

## 模块二：实时运行与 Trace Explorer v2

### 目标

把当前 Run Detail 从“运行后查看”升级为“运行中实时观察 + 运行后深度分析”。

`docs/可观测.md` 强调，完整轨迹的价值首先是问题诊断。开发者应该能回放全过程，而不是靠猜测。

### 必须做

- Active Runs 页面持续显示正在运行的 run。
- Run Detail 支持运行中刷新。
- Timeline 支持类型过滤：
  - thought
  - LLM
  - tool
  - code execution
  - retrieval
  - memory
  - skill
  - scheduler
  - guardrail
  - handoff
  - final answer
- 支持错误优先视图。
- 支持 raw span attributes 展开。
- 支持按耗时排序，定位 slowest span。
- 支持 token、cost、duration 汇总。

### 实时方式

短期可以继续用 polling，但接口和前端结构要为 SSE/WebSocket 留好边界。

建议顺序：

1. 先做稳定 polling。
2. 再新增 SSE：
   - `/api/projects/{project_id}/active-runs/events`
   - `/api/runs/{run_id}/events`
3. 前端 Run Detail 订阅事件并增量更新 timeline。

### 验收

运行 Hermes 长任务时：

- Agent 列表能看到 `hermes-agent` running。
- Active Runs 能看到当前任务。
- Run Detail 能看到 timeline 逐步增长。
- 任务完成后 status 变成 completed 或 failed。

## 模块三：深度语义 Trace 补齐

### 目标

当前平台已有 thought、LLM、tool、retrieval、skill、final answer，但还不足以覆盖真实 Agent。下一阶段要补齐：

- memory
- guardrail
- handoff / subagent
- scheduler
- code execution

### ClickHouse 语义表

建议新增：

```text
trace_memory_ops
trace_guardrail_checks
trace_handoffs
trace_scheduler_jobs
trace_code_executions
```

如果为了降低第一步复杂度，可以先把这些字段写入 `trace_spans.attributes`，但最终必须进入语义表，便于查询和评测。

### 字段要求

`trace_memory_ops`：

- run_id
- trace_id
- span_id
- operation: prefetch / inject / read / write / sync / extract
- provider
- key_summary
- value_summary
- hit_count
- success
- error
- start_time
- duration_ms

`trace_guardrail_checks`：

- policy_name
- decision: allow / block / warn
- risk_level
- reason
- action_summary
- success

`trace_handoffs`：

- parent_agent
- child_agent
- reason
- input_summary
- output_summary
- status

`trace_scheduler_jobs`：

- schedule_id
- cron_expr
- job_name
- trigger_time
- status
- output_summary
- error

`trace_code_executions`：

- language
- generated_code_summary
- stdout_summary
- stderr_summary
- exit_code
- duration_ms
- success

### 验收

Hermes demo 后 ClickHouse 必须能查到：

```sql
SELECT run_id, count() FROM trace_memory_ops GROUP BY run_id ORDER BY count() DESC LIMIT 10;
SELECT run_id, count() FROM trace_scheduler_jobs GROUP BY run_id ORDER BY count() DESC LIMIT 10;
SELECT run_id, count() FROM trace_code_executions GROUP BY run_id ORDER BY count() DESC LIMIT 10;
```

如果某个表在本地 demo 没有数据，最终报告必须解释是 Hermes 功能未启用，还是平台采集失败。

## 模块四：Evaluation Profile 与 Rubric 配置中心

### 目标

把当前 Metric Definition 升级为用户可配置的 Evaluation Profile。

`docs/可观测.md` 明确指出，LLM-as-a-Judge 不能只有一个泛化分数，必须有结构化 Rubric，包含专家指导、全面覆盖、权重、否决项和自包含评估标准。

### Evaluation Profile

一个 profile 包含：

- name
- description
- target agent / dataset scope
- dimensions
- weights
- thresholds
- veto rules
- judge provider
- deterministic rules
- rubric text
- examples
- edge cases
- required trace fields

### Hermes 默认 Profile

新增一个内置 profile：

`hermes-agentops-default`

维度：

- final_answer：最终回答是否满足任务。
- trajectory：执行步骤是否合理。
- tool_correctness：是否调用正确工具。
- code_execution：代码是否执行成功，错误是否暴露。
- memory：是否正确写入、预取、读取用户偏好。
- skill：是否创建、复用或改进合适技能。
- scheduler：定时任务是否按预期触发和停止。
- safety：是否拒绝或隔离高风险动作。
- efficiency：工具次数、token、耗时是否在阈值内。

### 否决项

必须支持 veto：

- 安全违规直接失败。
- 编造工具结果直接失败。
- 隐藏工具错误直接失败。
- 未经授权执行高风险操作直接失败。

### 验收

- 前端能创建和查看 Evaluation Profile。
- 创建实验时能选择 profile。
- 实验报告展示每个维度分数。
- veto 触发时综合结果失败，并展示触发原因。

## 模块五：Trace 回流 Dataset 与人工标注闭环

### 目标

实现 `docs/可观测.md` 中最关键的闭环：

```text
线上 trace → 失败/可疑样本筛选 → 脱敏 → 人工标注 → dataset case → regression experiment
```

### 必须做

- Run Detail 增加“加入评估集”。
- 自动生成 dataset case：
  - input
  - actual_output
  - source_run_id
  - source_trace_id
  - agent_name
  - model_name
  - channel
  - tags
  - trace_snapshot
- 人工补：
  - expected_output
  - rubric
  - failure_reason
  - labels
- 支持标记为 gold case。

### Annotation Queue

标注队列要展示：

- run 输入输出。
- trace 摘要。
- 自动评分。
- 失败维度。
- 人工判定。
- 标注理由。
- 是否进入回归集。

### 验收

从 Hermes 某条 run 进入 Run Detail：

1. 点击“加入评估集”。
2. 选择 dataset。
3. 编辑 expected output 和标签。
4. 保存。
5. 在 Dataset 页面看到该 case。
6. 用该 case 创建 regression experiment。
7. 实验结果能关联回原始 run/trace。

## 模块六：实验对比、回归报告与成本延迟分析

### 目标

把实验报告从“单次实验结果”升级为“版本决策报告”。

`docs/可观测.md` 强调，模型切换和 Harness 改动必须基于数据，不应只看一次分数。还要关注成本、延迟、鲁棒性和统计噪声。

### 实验对比

支持同一 dataset 下比较：

- Hermes 不同 git sha。
- 不同 LLM provider。
- 不同 model。
- 不同 prompt / system instruction。
- 不同 memory provider。
- 不同 tool 配置。
- 不同 scheduler / skill 策略。

### 报告内容

报告至少包含：

- baseline vs candidate 总分。
- 各维度平均分。
- pass_count / fail_count。
- regression cases：baseline 通过、candidate 失败。
- improvement cases：baseline 失败、candidate 通过。
- p50 / p95 / p99 latency。
- input tokens / output tokens / total tokens。
- estimated cost。
- tool count。
- memory hit count。
- failed tool count。

### 统计口径

第一版不必实现完整 McNemar 检验，但报告必须展示：

- case 数量。
- pass rate。
- 近似置信区间。
- 分差是否小于噪声带宽的提示。
- 多次运行均值和波动范围的字段预留。

### 验收

Hermes demo 至少跑两个配置：

- baseline：默认配置。
- candidate：切换一个 provider/model 或关闭某个 memory/skill 功能。

页面能展示两者差异，并列出退化 case。

## 模块七：统一端到端 Demo 与数据验收

### 目标

将下一阶段能力收敛成一个可展示、可验证、可复跑的 demo。

命令：

```bash
make demo-hermes-agentops
```

### 流程

1. 启动 AgentScope 依赖和服务。
2. 创建 demo 用户。
3. 创建项目。
4. 创建 project token。
5. 检查 Hermes 原生命令。
6. 启用 Hermes observability plugin。
7. 启动 Hermes 持续运行。
8. 执行至少 5 个 Hermes 原生任务。
9. 创建 dataset。
10. 创建 Hermes Evaluation Profile。
11. 创建 experiment。
12. 运行评测。
13. 从一条 run 回流 dataset case。
14. 再跑 regression experiment。
15. 输出前端链接。
16. 输出 PostgreSQL 和 ClickHouse 验证摘要。

### 必须验证

PostgreSQL：

```sql
SELECT name, environment, status
FROM agents
ORDER BY created_at DESC
LIMIT 5;

SELECT agent_name, status, trace_id, input_summary, output_summary
FROM runs
WHERE agent_name = 'hermes-agent'
ORDER BY created_at DESC
LIMIT 10;

SELECT run_id, score, passed, reason
FROM eval_case_results
ORDER BY created_at DESC
LIMIT 10;
```

ClickHouse：

```sql
SELECT run_id, count()
FROM trace_spans
GROUP BY run_id
ORDER BY count() DESC
LIMIT 10;

SELECT run_id, count()
FROM trace_llm_calls
GROUP BY run_id
ORDER BY count() DESC
LIMIT 10;

SELECT run_id, count()
FROM trace_tool_calls
GROUP BY run_id
ORDER BY count() DESC
LIMIT 10;

SELECT run_id, count()
FROM trace_memory_ops
GROUP BY run_id
ORDER BY count() DESC
LIMIT 10;

SELECT run_id, count()
FROM trace_skill_calls
GROUP BY run_id
ORDER BY count() DESC
LIMIT 10;

SELECT run_id, count()
FROM trace_final_answers
GROUP BY run_id
ORDER BY count() DESC
LIMIT 10;
```

前端：

- Agent 列表能看到 Hermes 在线。
- Active Runs 能看到正在运行。
- Run Detail 能看到完整 timeline。
- Dataset 能看到从 trace 回流的 case。
- Experiment 能看到多维评测结果。
- Comparison Report 能看到 baseline vs candidate。

## 推荐实施顺序

### 第一批：先打通真实 Agent 接入

1. 修复 `tool_policy expected_tool=""` 语义问题。
2. clone/fork Hermes。
3. 跑通 Hermes 原生 CLI。
4. 新增 Hermes observability plugin。
5. 接入 AgentScope SDK 注册、心跳、run lifecycle。
6. 埋点 LLM、tool、final answer。
7. 新增 `make demo-hermes-agentops` 的最小版本。

第一批验收：Hermes 在线，至少 3 个 run 能在前端和数据库看到。

### 第二批：补齐深度语义 Trace

1. 补 memory semantic record。
2. 补 skill semantic record。
3. 补 scheduler semantic record。
4. 补 code execution semantic record。
5. 补 guardrail / handoff 记录。
6. Run Detail 展示这些新类型。

第二批验收：Hermes Run Detail 能看到 memory、skill、scheduler、code execution。

### 第三批：评测闭环

1. 新增 Evaluation Profile。
2. 新增 Hermes 默认 Rubric。
3. 支持 trajectory + outcome 双层评测。
4. 支持 trace promote dataset。
5. 支持 annotation queue。
6. 支持 regression experiment。

第三批验收：Hermes 线上 run 可以进入 dataset 并参与多维评测。

### 第四批：决策报告

1. 新增 experiment comparison。
2. 新增成本延迟分析。
3. 新增 pass rate 置信区间提示。
4. 新增 regression/improvement case 列表。

第四批验收：同一 dataset 下能比较 Hermes 两个配置。

## 给实现 Agent 的提示词要求

后续给另一个 agent 的实现提示词必须包含：

- 必须使用 worktree。
- 必须先跑通 Hermes 原生功能。
- 禁止重新开发 Agent。
- 优先使用 Hermes plugin。
- plugin 不够时才允许最小 core patch。
- 每个阶段分 commit。
- 提交信息使用中文。
- 实现后必须跑完整 demo。
- 必须验证 PostgreSQL 和 ClickHouse 数据。
- 必须打开前端链接确认展示正确。
- 最终报告必须包含 run id、experiment id、SQL 结果摘要和前端链接。

## 阶段完成标准

下一阶段完成后，项目状态应达到：

- 平台接入了一个真实外部开源 Agent，而不是自写 demo。
- Hermes 能长期在线，持续产生 run。
- 每次 run 能完整观察 LLM、工具、代码执行、记忆、技能、定时任务和最终输出。
- 线上 trace 能回流为 dataset case。
- 用户能配置 Rubric 和多维评测。
- 实验报告能区分最终答案失败和轨迹失败。
- 平台能对比两个 Hermes 配置，输出质量、成本、延迟差异。
- 所有结论都有 PostgreSQL、ClickHouse 和前端页面验证。

这才符合 `docs/可观测.md` 中“观察 → 假设 → 实验 → 验证 → 新认识 → 新假设”的生产级 Agent 评估闭环。
