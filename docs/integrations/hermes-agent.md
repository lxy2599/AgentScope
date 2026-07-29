# Hermes Agent 接入

本接入以 [NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent) 的固定提交
`d9165d7a678d4105f42921a7fc1886df3804531b` 为基准。适配器不修改 Hermes 源码，也不依赖
Hermes 私有实现；优先使用其公开的 observer hook，旧版本再退回到公开的 Agent callback。

## 采集范围

- `agent.run`：运行输入、输出、状态、会话与运行关联。
- LLM 请求：模型、输入/输出摘要、令牌数、错误和实际耗时。
- 工具：调用参数摘要、结果摘要、状态与耗时。
- Memory：读写操作及目标；在 ClickHouse 中以 `trace_tool_calls.tool_name = 'memory'` 校验。
- Skill：技能加载和调用，写入 `trace_skill_calls`。
- 审批与工具风险：作为 guardrail 事件。
- 推理文本：仅在 `capture_reasoning=True` 且追踪内容采集开启时保存，写入 `trace_thoughts`。

所有 span 都包含 AgentScope 的 `run_id`、`trace_id`、agent 实例和适配器元数据，因此 Run
详情、实时运行列表、PostgreSQL 的运行状态与 ClickHouse 的语义表可相互追溯。

## 真实 E2E：观测、在线评测与人工回流

`make demo-hermes-agentops` 运行的是**真实上游 Hermes**、真实 OpenAI-compatible 模型和真实
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

1. 固定检出 Hermes 上游源码并创建隔离虚拟环境；SDK 与该 worktree 的 `python/` 一起安装。
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

## 嵌入已有 Hermes 代码

```python
from agentscope_sdk.adapters import AdapterSession, HermesAdapter

session = AdapterSession(runtime, tracer, metadata={"framework": "hermes"})
adapter = HermesAdapter(session, capture_reasoning=False)
answer = adapter.run(agent, user_prompt, task_id="support-123")
run_id = session.last_run_id
```

`HermesAdapter` 会保留宿主已有回调。若 Hermes 提供 plugin manager，则通过其 observer hook
注册；每次回调错误都会被隔离，不会改变 Agent 的原有执行结果。
