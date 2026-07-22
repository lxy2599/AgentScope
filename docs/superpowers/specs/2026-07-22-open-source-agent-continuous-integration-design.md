# 开源 Agent 持续接入 AgentScope 设计文档

日期：2026-07-22

## 一句话目标

下一阶段要接入一个真实、复杂、可持续运行的 GitHub 开源 Agent，并在这个 Agent 的原有代码路径里埋点：它不是我们重新开发的 demo Agent，也不是基于开源框架另写一个示例，而是把已有完整开源 Agent 改造成 AgentScope 的长期观测对象。

## 背景

当前平台已经能通过自写 demo 展示 Agent 可观测和评测能力，但这些 demo 仍然有两个不足：

- 它们是为平台演示而写的，复杂度和真实项目不同。
- 它们无法证明平台能接入“用户已有的任意 Agent 项目”。

`docs/可观测.md` 的核心观点是：生产级 Agent 平台不能只离线打分，而要把真实运行轨迹、评估数据集、Rubric、多维评分、人工标注、版本对比和持续改进串成闭环。因此下一阶段需要选择一个已经存在的完整开源 Agent，在它的原有启动入口、任务路由、工具调用、RAG、记忆、权限/安全、最终输出链路里埋点，让平台从“自证 demo”升级到“接入外部真实 Agent”。

## 开源 Agent 选择

### 候选项目对比

| 候选项目 | 覆盖能力 | 优点 | 风险 | 结论 |
|---|---|---|---|---|
| `agentscope-ai/agentscope-samples/alias` | General、Browser Use、Deep Research、Financial Analysis、Data Science、Meta Planner、RAG、GitHub MCP、长期记忆、前后端部署、任务中断、artifact 编辑 | 它已经是完整 Agent 应用，不需要我们重新开发 Agent；覆盖 `docs/可观测.md` 里要求的大多数观测和评测维度；Python 项目，便于接入我们的 Python SDK | 项目较完整，依赖和启动流程比单文件 demo 复杂；需要先跑通原始 Alias-Agent 再埋点 | 推荐 |
| `agentscope-ai/agentscope-samples/evotraders` | 多 Agent、工具、通信、决策、评估、ReMe 记忆、学习和复盘 | 业务流程完整，天然适合观察多 Agent 决策和记忆更新 | 金融交易领域变量较多，外部数据和评测标准更难控制 | 备选 |
| `agentscope-ai/agentscope` | LLM、tool、RAG、memory、agent team、event、permission、workspace/sandbox、middleware | 框架能力完整 | 它是 Agent 框架，不是一个完整业务 Agent；如果基于它新写 Agent，会偏离本阶段目标 | 不作为本阶段目标 |
| `OpenHands/software-agent-sdk` | LLM、terminal/file/task tools、workspace、agent server、skills/plugins、MCP | 工程化强，适合代码 Agent 场景；SDK 结构清晰 | RAG、memory、guardrail 不是核心路径，需要额外补适配层 | 备选 |
| `OpenHands/openhands` | coding agent、automation、local/remote/cloud backend、GitHub/Slack/Linear 集成 | 真实大型项目，长期运行价值高 | 系统重，前期接入成本高，不适合作为下一阶段首个目标 | 后续阶段 |
| `TransformerOptimus/superagi` | autonomous agent、toolkits、vector DB、telemetry、memory、workflow | 功能面宽，天然接近 AgentOps | 项目较重，栈更复杂，维护和二次开发成本高 | 不作为首选 |

### 最终选择

下一阶段选择 `agentscope-ai/agentscope-samples` 仓库里的 `alias` 作为首个外部持续接入对象。文档中称它为 **Alias-Agent**。

选择依据来自 `agentscope-samples` 和 `alias` 的 GitHub README：

- `agentscope-samples` 是 ready-to-use Python agent examples 集合，覆盖 CLI 工具到 full-stack 应用。
- `alias` 的定位是 out-of-the-box agent application，不是框架样例。
- Alias-Agent 有五种运行模式：General、Browser Use、Deep Research、Financial Analysis、Data Science。
- General Mode 有 Meta Planner，能按任务上下文自动切换 easy-task、planning-execution、browser use、deep research、data science 等模式。
- General Mode 内置 AgentScope-specific QA Agent，结合 RAG、GitHub MCP tools 和私有知识库回答问题。
- Browser Use Mode 有多模态浏览器 Agent、动态子任务管理、表单填写、文件下载。
- Deep Research Mode 有树状问题/假设探索、预搜索、用户可中断研究方向。
- Financial Analysis Mode 使用“提出假设 → 收集证据 → 验证假设 → 更新状态”的可追踪架构。
- Data Science Mode 覆盖数据获取、清洗、建模、可视化、报告生成。
- Enhanced Memory System 包含 Tool Memory 和 User Profiling 两类长期记忆。
- CLI 和 full-stack deployment 都可用，适合持续运行和前端验证。

参考链接：

- `agentscope-ai/agentscope-samples`: https://github.com/agentscope-ai/agentscope-samples
- `agentscope-ai/agentscope-samples/alias`: https://github.com/agentscope-ai/agentscope-samples/tree/main/alias
- `agentscope-ai/agentscope-samples/evotraders`: https://github.com/agentscope-ai/agentscope-samples/tree/main/evotraders
- `agentscope-ai/agentscope`: https://github.com/agentscope-ai/agentscope
- `OpenHands/software-agent-sdk`: https://github.com/OpenHands/software-agent-sdk
- `OpenHands/openhands`: https://github.com/OpenHands/openhands
- `TransformerOptimus/superagi`: https://github.com/TransformerOptimus/superagi

## 范围

### 必须完成

1. 克隆或 fork `agentscope-ai/agentscope-samples`，在 `alias` 这个已有完整 Agent 应用中做埋点。
2. 先按 Alias-Agent 原始 README 跑通至少一个原生命令，例如 `alias_agent run --mode general --task "<真实任务>"`。未跑通原始 Agent 前，不允许开始接入 AgentScope 平台。
3. 在 Alias-Agent 原有启动入口接入本项目的 `agentscope_sdk`：
   - 注册 Agent。
   - 注册实例。
   - 注册能力。
   - 持续心跳。
   - run 开始、运行中、完成、失败状态上报。
4. 在 Alias-Agent 原有运行路径中埋点，而不是另写一个 Agent 替代它：
   - thought
   - LLM
   - tool
   - retrieval
   - memory
   - guardrail / permission
   - skill / middleware
   - agent handoff / agent team
   - final answer
5. 用原有 Alias-Agent 的 General、Deep Research、Browser Use、Data Science 等模式产生不同类型的真实 run。
6. 让 Alias-Agent 能持续运行，而不是跑一次退出。
7. 让它能被平台离线评测，也能产生线上观测数据。
8. 跑通数据库验证，确认 PostgreSQL 和 ClickHouse 的数据都正确。
9. 跑通前端验证，确认 Agent 列表、Active Runs、Run Detail、Trace Timeline、实验报告都有数据。

### 暂不做

- 不重新开发一个 Agent。
- 不在外部仓库新增一个绕开 Alias-Agent 原始执行链路的替代 demo。
- 不把外部 `agentscope-ai/agentscope-samples` 全量 vendor 到主仓库。
- 不改造 Alias-Agent 的核心架构，只做最小侵入式 instrumentation。
- 不做完整 SaaS 托管。
- 不做 OpenHands 级代码 Agent 深度接入。
- 不做后训练或 RLVR。
- 不做多模态 UI / TTS / ASR judge。

## 集成形态

### 推荐仓库组织

主仓库不直接复制外部项目源码。推荐使用独立目录：

```text
/home/luxinyu/GolandProjects/
  AgentScope/                         # 当前平台主仓库
  AgentScopeExternalAgents/
    agentscope-samples/                # fork 或 clone 的外部 Agent 项目
      alias/                           # 被埋点的完整 Alias-Agent 应用
```

原因：

- 保持主平台仓库干净。
- 外部项目可以单独提交 patch。
- 后续可以把该接入方案做成文档化案例，而不是污染平台核心代码。

主仓库只保留：

```text
docs/external-agents/alias-agent-integration.md
scripts/run-alias-agent-observed.sh
scripts/verify-alias-agent-observed.sh
python/examples/alias_agent_runner_config.py
```

外部仓库只允许新增或修改 instrumentation 相关文件；业务任务、工具、RAG、记忆、模式路由必须继续走 Alias-Agent 原有代码。建议新增：

```text
alias/
  src/alias/agentops_adapter.py         # AgentScope 平台埋点适配层
  src/alias/agentops_hooks.py           # 对原 LLM/tool/RAG/memory/guardrail 链路的 hook
  scripts/run_observed_alias.py         # 只负责循环调用 Alias-Agent 原有 CLI/服务入口
```

禁止新增 `examples/agentscope_agentops_continuous/main.py` 这类“重新开发一个 Agent”的目录。

### 为什么不直接 submodule

暂不使用 git submodule。原因是当前阶段目标是验证接入能力，不是建立长期依赖分发机制。submodule 会增加开发者操作负担，也会让另一个 agent 在 worktree 中执行时更容易踩到路径和分支问题。

## 运行模式

### 持续运行进程

外部 Agent 需要作为一个长期进程运行：

```bash
AGENTSCOPE_SERVER_URL=http://127.0.0.1:8080 \
AGENTSCOPE_PROJECT_TOKEN=ascope_<project-token> \
AGENTSCOPE_ALIAS_RUN_INTERVAL=30 \
python alias/scripts/run_observed_alias.py
```

行为：

1. 启动时注册 Agent：`alias-agent`。
2. 注册 capabilities：
   - `mode`: general、browser_use、deep_research、financial_analysis、data_science。
   - `tool`: Alias-Agent 原项目实际注册的 browser、GitHub MCP、文件、数据分析、金融 MCP、检索等工具。
   - `skill`: Alias-Agent 原项目实际存在的 meta planner、pre-search、hypothesis verification、report generation 等能力。
   - `knowledge_base`: Alias-Agent 原有私有知识库和 AgentScope-specific QA 知识源。
   - `memory`: Tool Memory、User Profiling、长期记忆服务。
   - `guardrail`: Alias-Agent 原有中断、权限、沙箱或高风险动作控制逻辑。
   - `agent`: Meta Planner、Browser Agent、Deep Research Agent、Financial Agent、Data Science Agent。
3. 启动后台 heartbeat。
4. 每隔 N 秒调用一次 Alias-Agent 原有 CLI 或服务入口执行真实任务。
5. 每个任务创建平台 run。
6. 执行过程中通过 hook 持续上报 semantic trace。
7. 完成或失败后更新 run 状态。
8. 进程停止时上报 stopped。

### 任务种子

不能写一个新 Agent 来模拟场景。只能准备“任务种子”，由 Alias-Agent 原有模式执行。任务种子至少包含 6 类，覆盖 `docs/可观测.md` 提到的关键评估维度：

```yaml
tasks:
  - id: simple_tool_use
    mode: general
    user_input: "阅读当前项目 README，并总结这个项目的主要用途。"
    expected_tools: ["read", "grep"]
    expected_answer_contains: ["AgentScope", "sample"]
    tags: ["tool", "final_answer"]

  - id: rag_grounded_answer
    mode: general
    user_input: "根据 Alias-Agent 的 AgentScope-specific QA 能力，回答 AgentScope Runtime 的 sandbox 能力是什么。"
    expected_retrieval_documents: ["agentscope_docs", "runtime_docs"]
    expected_answer_contains: ["sandbox", "runtime"]
    tags: ["rag", "groundedness"]

  - id: memory_recall
    mode: general
    user_input: "记住用户偏好是中文输出。下一轮询问时需要使用中文回答。"
    follow_up_input: "请用我偏好的语言总结上一轮任务。"
    expected_answer_contains: ["中文"]
    tags: ["memory"]

  - id: deep_research
    mode: deep_research
    user_input: "调研 Agent 可观测平台应该如何评估工具调用质量，并给出结构化结论。"
    expected_agents: ["meta_planner", "deep_research"]
    expected_answer_contains: ["工具调用", "评估"]
    tags: ["handoff", "trajectory"]

  - id: browser_use
    mode: browser_use
    user_input: "打开一个网页并提取页面标题与主要链接。"
    expected_tools: ["browser"]
    tags: ["browser", "tool", "multimodal"]

  - id: data_science
    mode: data_science
    user_input: "读取一个小型 CSV，计算每列缺失值比例并生成结论。"
    expected_tools: ["file", "python", "data_analysis"]
    tags: ["data_science", "tool", "artifact"]
```

这些任务种子不是为了替代 Alias-Agent 的业务逻辑，而是为了反复触发 Alias-Agent 原有模式和原有链路，稳定地产生平台需要观测和评测的数据。

## 埋点设计

### Alias-Agent 原有事件到本平台语义表的映射

| Alias-Agent 原有行为 | 本平台 trace 类型 | 关键字段 |
|---|---|---|
| Agent 主循环开始 | span `agent.run` | run_id、trace_id、agent_name、task_id、mode |
| Meta Planner 生成计划或路由决策 | thought | stage、content、confidence、selected_mode |
| LLM 调用 | llm | provider、model、input_summary、output_summary、tokens、cost、duration |
| 原有工具调用 | tool | tool_name、arguments_summary、result_summary、success、error、retry_count |
| 原有 RAG / GitHub MCP 检索 | retrieval | knowledge_base、query_text、top_k、returned_count、document_ids |
| Tool Memory / User Profiling 读写 | memory | memory_type、operation、key_summary、hit_count |
| 原有中断、权限、沙箱或风险控制 | guardrail 或 skill | policy_name、decision、risk_level、reason |
| 原有 mode / skill / middleware | skill | skill_name、input_summary、output_summary、success |
| Meta Planner 切换到 Browser/Research/DataScience 等模式 | span event 或 handoff | from_agent、to_agent、reason |
| 最终输出 | final_answer | answer_summary、answer_output、structured_output |

如果当前 ClickHouse 还没有 `trace_memories`、`trace_guardrails`、`trace_handoffs` 三张语义表，下一阶段需要补齐；如果短期不补表，也必须先通过 `trace_spans` attributes 保留完整字段，前端暂时按 span kind 展示。

### 最小侵入方案

外部项目中新增 `agentops_adapter.py` 和 `agentops_hooks.py`，不要大面积改核心代码，也不要复制 Alias-Agent 逻辑重新实现一遍。

适配器职责：

- 包装 Alias-Agent 原有 model 调用。
- 包装 Alias-Agent 原有 tool 执行。
- 包装 Alias-Agent 原有 RAG / GitHub MCP / private knowledge base retriever。
- 包装 Alias-Agent 原有 Tool Memory / User Profiling read/write。
- 包装 Alias-Agent 原有 interrupt / permission / sandbox / guardrail 逻辑。
- 提供 `run_alias_task()` 上下文管理器。
- 在异常时自动记录 error span 并关闭 run。

示意接口：

```python
from collections.abc import Iterator
from contextlib import contextmanager

class AgentOpsAdapter:
    def __init__(self, server_url: str, project_token: str, agent_name: str, version: str):
        self.server_url = server_url
        self.project_token = project_token
        self.agent_name = agent_name
        self.version = version

    def start(self) -> None:
        """注册 Agent、实例、能力，并启动后台 heartbeat。"""

    def stop(self) -> None:
        """停止 heartbeat，flush trace，并上报 instance stopped。"""

    @contextmanager
    def run_alias_task(self, task_id: str, mode: str, user_input: str) -> Iterator[str]:
        """创建平台 run，yield run_id，退出时根据异常自动 completed 或 failed。"""
        run_id = "runtime-created-run-id"
        yield run_id

    @contextmanager
    def trace_tool(self, tool_name: str, arguments: dict) -> Iterator[None]:
        """记录工具 span，退出时补齐 result、duration、success/error。"""
        yield

    @contextmanager
    def trace_retrieval(self, knowledge_base: str, query: str) -> Iterator[None]:
        """记录 RAG 检索 span 和 semantic retrieval record。"""
        yield

    @contextmanager
    def trace_skill(self, skill_name: str, input_summary: str) -> Iterator[None]:
        """记录 skill/middleware span 和 semantic skill record。"""
        yield

    def trace_thought(self, stage: str, content: str, confidence: float | None = None) -> None:
        """记录 thought semantic record。"""

    def trace_final_answer(self, answer: str, structured_output: dict | None = None) -> None:
        """记录 final_answer semantic record，并更新 run output_summary。"""
```

## 平台侧需要补齐的能力

### 1. Run Detail 支持 memory / guardrail / handoff

现有 Run Detail 已能展示 thought、LLM、tool、retrieval、skill、final answer。下一阶段要补：

- memory 时间线卡片。
- guardrail / permission 卡片。
- handoff 卡片。
- raw span attributes 展开。
- 失败 span 高亮。

### 2. Evaluation Profile 支持外部 Agent 场景

需要能配置这些评测维度：

- final_answer：最终答案是否满足 expected_answer_contains。
- trajectory：是否按合理顺序执行 Meta Planner 路由、目标模式执行、结果汇总。
- tool_correctness：是否调用了 expected_tools，是否避开 forbidden_tools。
- argument_correctness：工具参数是否包含必要字段。
- rag_groundedness：是否命中 expected_retrieval_documents。
- memory：是否正确读取或写入偏好。
- safety：是否拦截高风险动作。
- efficiency：是否在 token、工具次数、耗时上低于阈值。

### 3. Trace 回流数据集

Alias-Agent 持续运行后，平台必须能从真实 run 创建 dataset case：

- 从 run detail 点击“加入评估集”。
- 自动带入 input、actual_output、trace_id、run_id、agent_name、model_name、tags。
- 人工补 expected_output、rubric、标签。
- 再跑 regression experiment。

## 验证标准

### 后端验证

运行外部 Agent 后，PostgreSQL 必须能查到：

```sql
SELECT name, environment, status FROM agents ORDER BY created_at DESC LIMIT 5;
SELECT agent_name, status, trace_id, input_summary, output_summary FROM runs ORDER BY created_at DESC LIMIT 10;
SELECT run_id, score, passed, reason FROM eval_case_results ORDER BY created_at DESC LIMIT 10;
```

ClickHouse 必须能查到：

```sql
SELECT run_id, count() FROM trace_spans GROUP BY run_id ORDER BY count() DESC LIMIT 10;
SELECT run_id, count() FROM trace_llm_calls GROUP BY run_id ORDER BY count() DESC LIMIT 10;
SELECT run_id, count() FROM trace_tool_calls GROUP BY run_id ORDER BY count() DESC LIMIT 10;
SELECT run_id, count() FROM trace_retrieval_calls GROUP BY run_id ORDER BY count() DESC LIMIT 10;
SELECT run_id, count() FROM trace_skill_calls GROUP BY run_id ORDER BY count() DESC LIMIT 10;
SELECT run_id, count() FROM trace_final_answers GROUP BY run_id ORDER BY count() DESC LIMIT 10;
```

如果新增 memory / guardrail / handoff 表，还必须验证：

```sql
SELECT run_id, count() FROM trace_memory_ops GROUP BY run_id ORDER BY count() DESC LIMIT 10;
SELECT run_id, count() FROM trace_guardrail_checks GROUP BY run_id ORDER BY count() DESC LIMIT 10;
SELECT run_id, count() FROM trace_handoffs GROUP BY run_id ORDER BY count() DESC LIMIT 10;
```

### 前端验证

必须手工打开以下页面确认：

- `/projects/{project_id}/agents`
  - 能看到 `alias-agent`。
  - 状态为 online 或 running。
  - last_seen 持续更新。
- `/projects/{project_id}/active-runs`
  - Agent 执行中能看到 active run。
- `/runs/{run_id}`
  - 能看到 thought、LLM、tool、retrieval、memory、guardrail、skill、handoff、final answer。
  - 每个节点有非 0 duration。
  - 工具参数、检索文档、guardrail 决策能展开。
- `/projects/{project_id}/metric-config`
  - 能配置外部 Agent 的多维评测规则。
- `/experiments/{experiment_id}`
  - 能看到每个 case 的维度分数和综合分。

### Demo 验证

新增命令：

```bash
make demo-external-agentscope-agent
```

该命令必须完成：

1. 检查后端和前端可用。
2. 创建或复用 demo 用户。
3. 创建项目。
4. 创建 project token。
5. 启动 Alias-Agent 持续运行进程。
6. 等待至少 3 个 Alias-Agent 原生任务完成。
7. 创建 dataset。
8. 创建 Evaluation Profile。
9. 创建并执行 experiment。
10. 验证 PostgreSQL 数据。
11. 验证 ClickHouse 数据。
12. 打印前端链接和数据库验证 SQL。

## 成功标准

下一阶段完成后，项目必须能清楚证明：

- AgentScope 平台能接入外部 GitHub 完整开源 Agent，而不只支持自写 demo。
- Alias-Agent 可以持续在线，持续产生 run。
- 每次 run 可以完整还原 thought、LLM、tool、RAG、memory、guardrail、handoff、final answer。
- 平台可以用同一批 run 做可观测、评测、人工标注和 regression dataset 回流。
- 多维评测不再只针对 final answer，而能覆盖 trajectory、tool、RAG、memory、安全、效率。
- 所有结论都有数据库验证，不只看终端输出。

## 实施顺序

1. 先修复当前 `tool_policy expected_tool=""` 语义问题，保证现有企业 demo 评测可信。
2. 在外部目录 clone/fork `agentscope-ai/agentscope-samples`。
3. 按 `alias/README.md` 跑通 Alias-Agent 原生命令，至少覆盖 General mode。
4. 找到 Alias-Agent 原有 CLI 入口、模式路由、LLM 调用、工具调用、RAG、memory、guardrail、final answer 的代码位置。
5. 通过最小 hook 接入本平台 Python SDK 的注册、心跳、run lifecycle。
6. 在原有链路里接入 thought、LLM、tool、retrieval、skill、final answer 埋点。
7. 补 memory、guardrail、handoff 采集和展示。
8. 做持续运行脚本和平台 demo 命令，脚本只能循环调用 Alias-Agent 原有入口，不能实现新 Agent。
9. 接入 Evaluation Profile 和 regression dataset 回流。
10. 完成端到端验证报告。

## 给实现 Agent 的强制要求

实现时必须在独立 worktree 中开发，并分 commit 提交。每个 commit 信息使用中文。

实现完成后，不能只说“测试通过”。必须实际跑通完整流程，并在最终报告中给出：

- worktree 路径。
- commit hash。
- 修改文件列表。
- 启动命令。
- demo 登录邮箱和密码。
- 项目 ID。
- Agent ID。
- 至少 3 个 run ID。
- 至少 3 个 Alias-Agent 原生任务 ID 或 task 输入。
- 至少 1 个 experiment ID。
- PostgreSQL 查询结果摘要。
- ClickHouse 查询结果摘要。
- 前端 Run Detail 链接。
- 前端 Experiment 链接。
- 已执行的测试命令和结果。

最低验证命令：

```bash
go test ./... -count=1
cd web && npm test -- --run
cd web && npm run build
cd python && PYTHONPATH=. pytest -q
make demo-external-agentscope-agent
```

如果因为缺少 LLM key 无法调用真实模型，必须使用 deterministic fake LLM 跑通完整流程；如果配置了 `GLM_API_KEY` 或 `DEEPSEEK_API_KEY`，必须额外跑一次真实 LLM 模式。
