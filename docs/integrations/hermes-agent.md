# Hermes Agent 接入

本接入以 [NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent) 的固定提交
`d9165d7a678d4105f42921a7fc1886df3804531b` 为基准。适配器不修改 Hermes 源码，也不依赖
Hermes 私有实现；优先使用其公开的 observer hook，旧版本再退回到公开的 Agent callback。

## 采集范围

- `agent.run`：运行输入、输出、状态、会话与运行关联。
- LLM 请求：模型、输入/输出摘要、令牌数、错误和实际耗时。
- 工具：调用参数摘要、结果摘要、状态与耗时。
- Memory：读写操作及目标。
- Skill：技能加载和调用。
- 审批与工具风险：作为 guardrail 事件。
- 推理文本：仅在 `capture_reasoning=True` 且追踪内容采集开启时保存。

所有 span 都包含 AgentScope 的 `run_id`、`trace_id`、agent 实例和适配器元数据，因此 Run
详情、实时运行列表、PostgreSQL 的运行状态与 ClickHouse 的语义表可相互追溯。

## 运行演示

先启动平台依赖、后端和前端，再从 ccswitch 以环境变量提供 OpenAI 兼容连接信息。密钥不要写入
脚本、仓库或结果文件。

```bash
export HERMES_LLM_BASE_URL='https://.../v1'
export HERMES_LLM_API_KEY='...'
export HERMES_LLM_MODEL='deepseek-chat'
export HERMES_LLM_PROVIDER='openai-compatible'
make demo-hermes-agentops
```

脚本会：固定检出上游源码、创建独立虚拟环境、注册 Agent 和心跳、执行一次真实 Hermes 的
memory/skill/LLM 流程、制造一次真实网络失败、创建失败 Trace 的标注项，并验证：

1. API 中同一 Agent 的 online 状态与成功/失败 Run 状态；
2. PostgreSQL 中 Run 与 Annotation 的 `run_id`/`trace_id` 关联；
3. ClickHouse 中该成功 Run 的 `trace_spans`、`trace_llm_calls`、`trace_final_answers` 至少各一条。

结果仅保存项目和运行标识到 `.cache/hermes-agentops-result.json`；登录密码仅用于本地演示。

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
