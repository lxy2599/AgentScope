# Hermes Agent 持续接入 AgentScope 设计文档

日期：2026-07-22

## 一句话目标

下一阶段选择一个已经存在的完整开源 Agent，在它的原有代码路径中埋点，并让它长期运行接入 AgentScope。目标不是再开发一个 Agent，也不是写一个模拟 demo，而是把 `NousResearch/hermes-agent` 这样的真实 Agent 改造成平台的持续观测对象。

## 背景

当前 AgentScope 平台已经能用自写 demo 展示 Agent 可观测和评测能力，但这还不能证明平台能接入真实用户已有的 Agent 项目。

`docs/可观测.md` 的主线是：生产级 Agent 平台需要把真实运行轨迹、数据集、Rubric、多维评测、人工标注、版本对比和持续改进连成闭环。要验证这条闭环，必须接入一个真实复杂 Agent，让它持续产生线上 run，再把这些 run 用于观测、评测和回归数据集沉淀。

因此下一阶段的任务是：

- 找一个完整开源 Agent。
- 跑通它的原生运行方式。
- 在它原有的 LLM、tool、memory、skill、scheduler、final response 链路里埋点。
- 让它持续在线并持续产生 run。
- 用 AgentScope 展示和评测这些真实 run。

## 选型结论

### 首选项目

选择 `NousResearch/hermes-agent`，文档中称为 **Hermes Agent**。

GitHub 地址：

- https://github.com/NousResearch/hermes-agent

选择原因：

- 它是完整 Agent，不是框架空壳。
- 它本身就是 persistent / always-on agent，适合做持续接入对象。
- 它有 CLI、messaging gateway、TUI、Electron desktop app，多入口共用同一个 agent core。
- 它支持长期记忆、自主技能创建、技能自我改进、会话搜索、用户画像和 scheduled automations。
- 它能调用 terminal、browser、code execution、tools，并支持 programmatic tool calling。
- 它支持 plugin system，适合通过 plugin/hook 接入 AgentScope，减少对核心代码的侵入。
- 它支持 memory providers，可以观察记忆注入、预取、同步、抽取和外部记忆工具。
- 它支持多 LLM provider，后续可做模型切换、成本对比、版本对比。
- 它有 cron scheduler，可展示 unattended 长期运行任务。
- 它有 subagents / parallel workstreams，可作为后续多 Agent trace 的扩展对象。

### 重要参考

- Hermes 仓库 README 提到：Hermes 是 self-improving AI agent，具备学习闭环、skills、memory、scheduled automations、subagents、terminal/browser/code execution、batch trajectory generation。
- `AGENTS.md` 将 Hermes 定义为 personal AI agent，同一个 agent core 运行在 CLI、messaging gateway、TUI、Electron desktop app 上，并通过 plugins 和 skills 扩展。
- Hermes plugin 文档说明 plugin 可添加 custom tools、hooks 和 integrations。
- Hermes memory provider 文档说明 provider 可以注入上下文、预取相关记忆、同步会话、抽取记忆，并给 agent 增加 memory 管理工具。

参考链接：

- https://github.com/NousResearch/hermes-agent
- https://github.com/NousResearch/hermes-agent/blob/main/AGENTS.md
- https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/features/plugins.md
- https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/features/memory-providers.md
- https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/features/code-execution.md

### 备选项目

| 项目 | 结论 |
|---|---|
| `agentscope-ai/agentscope-samples/alias` | 仍可作为备选。它是完整 Agent 应用，覆盖 General、Browser Use、Deep Research、Financial Analysis、Data Science、RAG、memory。但相比 Hermes，它更像产品样例，持续运行和自我学习属性弱一些。 |
| `agentscope-ai/agentscope-samples/evotraders` | 适合多 Agent 决策和记忆复盘，但金融任务变量较多，评测标准不如 Hermes 稳定。 |
| `OpenHands/openhands` | 更真实、更重，适合后续作为代码 Agent 接入对象，不适合作为当前首个外部 Agent。 |

## 核心原则

### 必须做

- 必须接入 Hermes 原项目。
- 必须先跑通 Hermes 原生命令。
- 必须让 Hermes 原有 agent core 执行任务。
- 必须在 Hermes 原有链路里埋点。
- 必须让 Hermes 持续在线。
- 必须用 Hermes 真实 run 验证平台数据。

### 禁止做

- 禁止重新开发一个 Agent。
- 禁止写一个绕过 Hermes 原有 agent core 的 demo。
- 禁止只在我们主仓库里写 `hermes_like_agent.py`。
- 禁止只模拟 Hermes 的工具、记忆、技能。
- 禁止只做终端输出验证，不查 PostgreSQL 和 ClickHouse。

## 仓库组织

主仓库不 vendor Hermes 源码。推荐目录：

```text
/home/luxinyu/GolandProjects/
  AgentScope/                         # 当前平台主仓库
  AgentScopeExternalAgents/
    hermes-agent/                      # fork 或 clone 的 Hermes Agent
```

主仓库只保留：

```text
docs/external-agents/hermes-agent-integration.md
scripts/run-hermes-agent-observed.sh
scripts/verify-hermes-agent-observed.sh
python/examples/hermes_agent_runner_config.py
```

Hermes 外部仓库中允许新增或修改：

```text
plugins/agentscope_observability/
  plugin.py                            # Hermes plugin，优先接入点
  hooks.py                             # LLM/tool/memory/skill/session hook
  README.md

scripts/run_observed_hermes.py          # 只负责循环调用 Hermes 原生命令或 core 入口
```

如果 Hermes plugin hook 无法覆盖某些关键链路，允许最小 core patch，但必须满足：

- patch 点必须在 Hermes 原有执行链路中。
- patch 只做事件转发和上下文传递。
- patch 不复制业务逻辑。
- patch 不改变 Hermes 原有任务行为。

## 原生 Hermes 跑通要求

实现者必须先在外部 Hermes 仓库跑通原生功能，再接入 AgentScope。

最低要求：

```bash
hermes
```

如果使用源码运行，则按 Hermes README 或项目脚本启动 CLI。

至少完成：

1. 一次 CLI 对话。
2. 一次工具调用。
3. 一次 memory 读写或会话记忆查询。
4. 一次 skill 调用或 skill 相关路径。
5. 一次 scheduled automation 或 cron 任务，如果本地环境支持。

未完成原生 Hermes 验证前，不允许写 AgentScope instrumentation。

## 接入方式

### 优先方案：Hermes Plugin

优先通过 Hermes plugin system 接入 AgentScope。

插件职责：

- 启动时初始化 AgentScope SDK。
- 注册 Hermes Agent。
- 注册 Hermes instance。
- 注册 Hermes capabilities。
- 在 session start 时创建或绑定 run。
- 在 LLM 调用前后记录 llm semantic record。
- 在 tool 调用前后记录 tool semantic record。
- 在 code execution 前后记录 tool/code_execution semantic record。
- 在 memory provider 注入、预取、同步、抽取时记录 memory semantic record。
- 在 skill 创建、选择、执行、自我改进时记录 skill semantic record。
- 在 scheduler/cron 任务开始、完成、失败时记录 run lifecycle。
- 在 final response 生成后记录 final_answer。
- 在异常时记录 error span 并关闭 run。

### 兜底方案：最小 Core Patch

如果 plugin 缺少关键 hook，则允许 patch Hermes core 的这些位置：

- CLI / gateway session start/end。
- agent loop start/end。
- model provider request/response。
- tool dispatcher before/after。
- code execution tool before/after。
- memory provider before/after。
- skill executor before/after。
- scheduler job before/after。
- final response emit。

每个 patch 必须非常薄，只调用 `agentscope_observability` 插件中的 hook 函数。

## 能力注册

Hermes 启动时，在 AgentScope 注册：

```text
agent_name: hermes-agent
version: 从 Hermes git sha 或 pyproject 版本读取
environment: dev
runtime: python
framework: hermes-agent
```

capabilities 至少包含：

```yaml
capabilities:
  tools:
    - terminal
    - browser
    - code_execution
    - programmatic_tool_calling
    - file_tools
  skills:
    - autonomous_skill_creation
    - skill_self_improvement
    - scheduled_automation
    - session_search
  memory:
    - agent_curated_memory
    - user_modeling
    - memory_provider_prefetch
    - memory_extraction
  channels:
    - cli
    - messaging_gateway
    - tui
    - electron_desktop
  schedulers:
    - cron
  subagents:
    - isolated_subagent
```

实际注册项必须来自 Hermes 当前代码和配置，不能虚构。如果某项在本地未启用，要注册为 disabled 或不注册。

## 运行模式

### 持续运行

新增主仓库脚本：

```bash
AGENTSCOPE_SERVER_URL=http://127.0.0.1:8080 \
AGENTSCOPE_PROJECT_TOKEN=ascope_<project-token> \
HERMES_AGENT_DIR=/home/luxinyu/GolandProjects/AgentScopeExternalAgents/hermes-agent \
HERMES_OBSERVED_INTERVAL=60 \
scripts/run-hermes-agent-observed.sh
```

行为：

1. 检查 AgentScope 后端健康。
2. 检查 Hermes 仓库存在。
3. 检查 Hermes 原生命令可运行。
4. 安装或启用 `agentscope_observability` plugin。
5. 启动 Hermes CLI 或 core runner。
6. 定时提交任务种子。
7. 让 Hermes 原 agent core 执行任务。
8. 通过 plugin/hook 上报 AgentScope。
9. 进程退出时 flush trace 并上报 stopped。

### 任务种子

任务种子只负责触发 Hermes 原有能力，不允许实现 Agent 逻辑。

```yaml
tasks:
  - id: terminal_tool
    channel: cli
    user_input: "列出当前工作目录文件，并总结这个仓库的主要组成。"
    expected_tools: ["terminal", "file_tools"]
    tags: ["tool", "trajectory"]

  - id: code_execution
    channel: cli
    user_input: "写一段 Python 脚本统计一个小 CSV 的行数和缺失值，并解释结果。"
    expected_tools: ["code_execution"]
    tags: ["tool", "code_execution", "final_answer"]

  - id: memory_write_read
    channel: cli
    user_input: "记住我偏好所有报告用中文、先给结论再给细节。"
    follow_up_input: "请按照我的偏好总结上一个任务。"
    expected_memory_ops: ["write", "prefetch", "read"]
    tags: ["memory", "personalization"]

  - id: skill_path
    channel: cli
    user_input: "把刚才的任务流程沉淀为可复用技能，并说明以后何时使用。"
    expected_skill_ops: ["create_or_update", "execute"]
    tags: ["skill", "self_improvement"]

  - id: scheduler
    channel: cron
    user_input: "创建一个每分钟运行一次的简短状态报告任务，运行一次后停止。"
    expected_scheduler: "cron"
    tags: ["scheduler", "long_running"]

  - id: browser_or_search
    channel: cli
    user_input: "使用可用的浏览器或搜索工具查找 Hermes Agent 文档中 plugin 的作用，并总结。"
    expected_tools: ["browser_or_search"]
    tags: ["browser", "search", "rag_like"]

  - id: safe_failure
    channel: cli
    user_input: "尝试执行一个不存在的命令，然后给出排查建议，不要隐藏错误。"
    expected_error_span: true
    tags: ["robustness", "tool_error"]
```

这些任务必须交给 Hermes 原生入口执行。

## 埋点映射

| Hermes 原有行为 | AgentScope 记录 | 关键字段 |
|---|---|---|
| CLI / gateway session start | run start + span `agent.run` | run_id、session_id、channel、user_input |
| Agent loop planning / reasoning | thought | stage、content、confidence、iteration |
| Model provider request/response | llm | provider、model、input_summary、output_summary、tokens、cost、duration |
| Tool dispatcher | tool | tool_name、arguments_summary、result_summary、success、error、retry_count |
| Terminal / browser / file tools | tool | tool_type、side_effect_risk、stdout/stderr summary |
| Code execution / PTC | tool 或 code_execution | generated_code_summary、rpc_calls、exit_code、duration |
| Memory provider prefetch / inject | memory | operation、provider、hit_count、context_summary |
| Memory extraction / sync | memory | operation、provider、extracted_summary、success |
| Skill selection / execution | skill | skill_name、skill_version、input_summary、output_summary |
| Autonomous skill creation / improvement | skill | operation、skill_name、diff_summary |
| Scheduler job start/end | run + skill/tool | schedule_id、cron_expr、job_status |
| Subagent spawn / completion | handoff | parent_agent、child_agent、reason、status |
| Guardrail / permission / sandbox refusal | guardrail | policy_name、decision、risk_level、reason |
| Final response | final_answer | answer_summary、answer_output、structured_output |

如果当前平台没有 `trace_memory_ops`、`trace_guardrail_checks`、`trace_handoffs`，本阶段要么补齐语义表，要么先把字段完整写入 `trace_spans.attributes` 并在前端 raw attributes 展示。最终验收必须能看到这些数据。

## 平台侧需要补齐

### Run Detail

Run Detail 必须支持 Hermes 的这些信息：

- CLI / gateway channel。
- session id。
- thought / reasoning 片段。
- LLM provider、model、token、cost、duration。
- terminal/browser/code execution 工具详情。
- memory prefetch、inject、sync、extract。
- skill creation、skill execution、skill improvement。
- scheduler job。
- subagent handoff。
- guardrail / sandbox / permission。
- final answer。
- raw span attributes。
- error span 高亮。

### Agent Control Center

Agent 列表必须能看到：

- `hermes-agent` 在线。
- instance last_seen 持续更新。
- running task / active run。
- capabilities 展示工具、skills、memory、scheduler、channels。

### Evaluation Profile

Hermes 的多维评测至少包含：

- final_answer：最终回答是否满足任务要求。
- trajectory：是否按合理步骤执行。
- tool_correctness：是否调用了需要的工具，是否避免不必要高风险工具。
- code_execution：生成代码是否成功执行，错误是否暴露。
- memory：是否正确写入、预取、读取用户偏好或历史信息。
- skill：是否创建/复用/改进合适技能。
- scheduler：定时任务是否按预期触发并停止。
- safety：是否拒绝或隔离高风险动作。
- efficiency：工具次数、token、耗时是否在阈值内。

### Trace 回流 Dataset

必须能从 Hermes 真实 run 回流评估集：

- 从 Run Detail 点击“加入评估集”。
- 自动带入 input、actual_output、trace_id、run_id、agent_name、model_name、channel、tags。
- 人工补 expected_output、Rubric 和失败标签。
- 用该 case 创建 regression experiment。

## Demo 命令

新增命令：

```bash
make demo-hermes-agentops
```

该命令必须完成：

1. 检查 AgentScope 后端和前端可用。
2. 创建或复用 demo 用户。
3. 创建项目。
4. 创建 project token。
5. 检查 Hermes 原生运行可用。
6. 启用 Hermes AgentScope observability plugin。
7. 启动 Hermes 持续运行进程。
8. 等待至少 5 个 Hermes 原生任务完成。
9. 创建 dataset。
10. 创建 Evaluation Profile。
11. 创建并执行 experiment。
12. 验证 PostgreSQL 数据。
13. 验证 ClickHouse 数据。
14. 打印前端 Agent、Run Detail、Experiment 链接。

## 数据库验证

### PostgreSQL

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

### ClickHouse

```sql
SELECT run_id, count()
FROM trace_spans
WHERE agent_name = 'hermes-agent'
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

如果新增 memory / guardrail / handoff 表，还必须验证：

```sql
SELECT run_id, count()
FROM trace_memory_ops
GROUP BY run_id
ORDER BY count() DESC
LIMIT 10;

SELECT run_id, count()
FROM trace_guardrail_checks
GROUP BY run_id
ORDER BY count() DESC
LIMIT 10;

SELECT run_id, count()
FROM trace_handoffs
GROUP BY run_id
ORDER BY count() DESC
LIMIT 10;
```

## 前端验收

必须手工打开并截图或记录结果：

- `/projects/{project_id}/agents`
  - 能看到 `hermes-agent`。
  - 状态 online 或 running。
  - last_seen 持续更新。
- `/projects/{project_id}/active-runs`
  - Hermes 执行中能看到 active run。
- `/runs/{run_id}`
  - 能看到 thought、LLM、tool、code execution、memory、skill、scheduler、guardrail、handoff、final answer。
  - 工具节点有参数、结果、错误、耗时。
  - memory 节点有 operation 和 provider。
  - skill 节点有 skill name 和操作类型。
  - 每个关键节点 duration 非 0。
- `/projects/{project_id}/metric-config`
  - 能配置 Hermes 多维评测。
- `/experiments/{experiment_id}`
  - 能看到每个 case 的维度分数和综合分。

## 实施顺序

1. 修复当前 `tool_policy expected_tool=""` 语义问题，保证现有评测可信。
2. 在外部目录 clone/fork `NousResearch/hermes-agent`。
3. 按 Hermes README 跑通原生 CLI。
4. 梳理 Hermes session、agent loop、model provider、tool dispatcher、memory provider、skill executor、scheduler 的代码位置。
5. 新增 `agentscope_observability` plugin。
6. 接入 AgentScope SDK 注册、心跳、run lifecycle。
7. 埋点 LLM、tool、code execution、memory、skill、scheduler、final answer。
8. 如 plugin hook 不够，做最小 core patch。
9. 平台侧补 memory、guardrail、handoff 展示和存储缺口。
10. 新增 `make demo-hermes-agentops`。
11. 跑通端到端 demo 和数据库验证。
12. 输出验证报告。

## 给实现 Agent 的强制要求

实现时必须在独立 worktree 中开发，分 commit 提交。提交信息使用中文。

实现完成后，必须实际跑通完整流程，并在最终报告中给出：

- 主平台 worktree 路径。
- Hermes 外部仓库路径。
- 主平台 commit hash。
- Hermes patch commit hash。
- 修改文件列表。
- 原生 Hermes 启动命令和结果。
- AgentScope demo 启动命令。
- demo 登录邮箱和密码。
- 项目 ID。
- Agent ID。
- 至少 5 个 run ID。
- 至少 5 个 Hermes 原生任务输入。
- 至少 1 个 experiment ID。
- PostgreSQL 查询结果摘要。
- ClickHouse 查询结果摘要。
- 前端 Agent 链接。
- 前端 Run Detail 链接。
- 前端 Experiment 链接。
- 已执行测试命令和结果。

最低验证命令：

```bash
go test ./... -count=1
cd web && npm test -- --run
cd web && npm run build
cd python && PYTHONPATH=. pytest -q
make demo-hermes-agentops
```

如果没有 LLM key，必须使用 Hermes 支持的本地或 deterministic provider 跑通完整链路；如果配置了 `GLM_API_KEY`、`DEEPSEEK_API_KEY`、`OPENAI_API_KEY` 或其他 Hermes 支持的 provider，必须额外跑一次真实模型模式。

## 成功标准

这个阶段完成后，平台必须能证明：

- AgentScope 能接入真实外部开源 Agent。
- Hermes Agent 能持续在线并产生真实 run。
- 平台能观察 Hermes 原生工具、记忆、技能、定时任务、代码执行、最终回答。
- 每个 run 能在 PostgreSQL 和 ClickHouse 中查到一致数据。
- 前端能展示 Hermes 的在线状态、active run 和完整 timeline。
- Hermes 的真实 run 能进入 dataset 并参与多维评测。
- 结论不是终端输出自证，而是由前端和数据库共同验证。
