# Hermes Agent 接入

Hermes 有两条受支持的接入路径；它们共享 AgentScope 的 Runtime、OTLP、脱敏、背压和 Trace
语义协议，但不能混用为同一 Run 的事件源。

| 路径 | 当前状态 | 源码与证据 |
| --- | --- | --- |
| 外置 `HermesAdapter` 兼容路径 | 已实现；当前 `make demo-hermes-agentops` 验证此路径 | 官方 Hermes `d9165d7a678d4105f42921a7fc1886df3804531b`；公开 observer hook / callback |
| 原生 SDK fork 路径 | 已实现；在 fork 内有 bridge 与真实生命周期回归测试 | 受控 fork `hermes-agent-agentscope`，当前 fork commit `02a438eb5b04009bebfc3c23c60d03b347e6b133`，上游基线 `015718066ab8e9499c3caea3cda9f7ea469036fc`，源码 `integrations/hermes-agent/` |

## Hermes 原生 SDK 路径（已实现）

原生实现已随受控 fork 纳入 `integrations/hermes-agent/`，bridge 位于 `integrations/hermes-agent/hermes_agentscope/bridge.py`。它按 conversation/session key 管理独立的
`RuntimeTraceSession`，并由以下真实执行点直接调用：

- `agent/agent_init.py`：初始化可选、lazy 的 fail-open bridge；
- `agent/conversation_loop.py`：用户输入创建 Run，真实 decision loop 创建/关闭 Turn，LLM 边界与最终回答/异常关闭 Run；
- `agent/tool_executor.py`：真实 Tool dispatch 的开始、完成和失败；
- `integrations/hermes-agent/tests/test_agentscope_bridge.py`、`integrations/hermes-agent/tests/agent/test_agentscope_native_lifecycle.py`：NoOp、并发隔离、真实 loop、LLM、工具、错误和并行工具回归。

原生 bridge 不调用 `HermesAdapter.run()`，也不以 callback 或 observer hook 作为原生事件源。它默认关闭、
lazy import、fail-open；SDK 未安装、配置无效或后端不可达时只能丢失观测，不能改变 Hermes 的调度、工具
执行或回答。Run 结束默认不进行同步 flush。

当前 AgentScope 仓库的真实平台 E2E 命令仍覆盖**外置 Adapter**。因此可以断言 native fork 的源码级实现和
fork 回归已经完成；除非另行提供 native fork 的平台结果文件和数据库证据，不应把 `make demo-hermes-agentops`
表述为 native fork 的平台 E2E。

## 统一 Trace 层级

两条路径都必须表达以下层级：

```text
agent.run
└── agent.turn
    ├── llm.call
    ├── tool.call（可多个、可重试、可并行）
    └── agent.answer
```

Turn 是一次 Agent decision loop，而不是单次 LLM 请求。Tool 必须显式挂在活动 Turn 下；所有 span 均带有
`run_id`、`trace_id`、agent 实例、`framework=hermes` 和 `integration=native` 或 `integration=adapter`，使
Run 详情、实时列表、PostgreSQL 和 ClickHouse 可相互追溯。

## 外置 Adapter 真实 E2E：观测、在线评测与人工回流

`make demo-hermes-agentops` 当前验证的是**外置 Adapter 兼容路径**：运行真实上游 Hermes、真实 OpenAI-compatible 模型和真实
AgentScope 服务。它会创建一个独立项目，因此不会污染已有项目中的采样规则、Run 或标注队列。

先启动平台依赖和后端；再从 ccswitch（或等价的凭据管理器）提供连接信息。密钥绝不能写入脚本、
仓库、结果文件或日志。

```bash
export HERMES_LLM_BASE_URL='https://.../v1'
export HERMES_LLM_API_KEY='...'
export HERMES_LLM_MODEL='deepseek-v4-flash'
export HERMES_LLM_PROVIDER='openai-compatible'
make demo-hermes-agentops
```

完整链路如下：

1. 固定检出 Hermes 上游源码并创建隔离虚拟环境；安装本仓库 AgentScope SDK 与 Adapter。
2. 注册临时用户、项目、OTLP/Runtime Token 和 `hermes-agent`，并等待多次心跳。
3. 创建项目级、`sample_rate=1.0`、无预算上限的 Sampling Rule。该项目只包含本次 Hermes，
   因此项目级规则既不会影响其他 Agent，又能保证两个测试 Run 都被在线评测 worker 选中。
4. 执行真实 Hermes 的 memory/skill/LLM 流程，并额外制造一次确定的本地网络失败；二者分别形成
   `completed` 与 `failed` 的 Runtime Run。
5. OTLP 将 Trace 写入 ClickHouse；Run 终态创建 PostgreSQL 权威的 `online_eval_jobs`。worker 用
   TraceAware evaluator 写入 `eval_scores`，失败 Run 自动创建“线上评测待标注”队列项，关联
   `online_eval_job_id`，而不是由演示脚本手工创建标注项。
6. 脚本轮询两个 Job 至 `succeeded`，并从 API、PostgreSQL、ClickHouse 交叉校验整个闭环。

当前 TraceAware evaluator 提供的是**执行健康启发式**（完成状态、最终答案、工具执行、span 错误等），
不应把它表述为语义正确性、事实性或安全性判断；这些需再接 Rule、Rubric + LLM Judge 或人工 Gold。

## 结果、凭据与复验

默认生成两个本地文件：

- `.cache/hermes-agentops-result.json`：无敏感信息，仅含 project/agent/rule/run/job/annotation
  标识和证据计数；
- `.cache/hermes-agentops-result.json.credentials.json`：仅含演示用户的 email/password，用于重新
  登录验证。

两个文件均以 `0600` 原子写入。凭据文件不得提交、共享或上传；结果文件也不要假设可公开，因为它
包含内部资源标识。演示输出不会打印密码或模型 API Key。

无需再次调用模型即可复验已有证据：

```bash
# 使用默认结果和凭据位置
./scripts/demo-hermes-agentops.sh --verify-only

# 或指定另一份结果文件（凭据文件由 HERMES_CREDENTIALS_FILE 指定）
./scripts/demo-hermes-agentops.sh --verify-only /path/to/hermes-agentops-result.json
```

`--verify-only` 会使用私有凭据重新登录，并验证：

- API：Agent 在线、成功/失败 Run、两个 `succeeded` 的 Online Eval Job，以及失败 Run 的自动标注项；
- PostgreSQL：Sampling Rule、Run、Job、`eval_scores` 和 `annotation_items.online_eval_job_id` 的关联；
- ClickHouse：成功 Run 至少有 `trace_spans`、`trace_thoughts`、`trace_llm_calls`、`trace_tool_calls`、
  memory 工具调用、`trace_skill_calls` 和 `trace_final_answers`。

若模型 API Key 曾直接暴露在终端、聊天记录或其他不可信介质中，完成验证后应在供应商控制台轮换该 Key。
