# OpenHands 接入 AgentScope

本接入面向 OpenHands 官方 Software Agent SDK，不修改 OpenHands 源码，也不依赖
私有属性。AgentScope Adapter 仅注册到 `Conversation(callbacks=...,
token_callbacks=...)` 的公开回调。

## 固定版本

- 官方仓库：`https://github.com/OpenHands/software-agent-sdk`
- 固定提交：`a375c66aaf29c8d3b6553c9164e65ed917149e81`
- 对应 SDK 版本：`1.37.0`
- 本地源码：`.external/openhands`

安装脚本会验证提交与上游工作区清洁度。上游源码不会进入 AgentScope Git 仓库。

## 事件映射

| OpenHands 公开事件 | AgentScope 记录 |
| --- | --- |
| `MessageEvent` | 用户输入、LLM 响应、最终答案、Skill 激活 |
| `ActionEvent` | Thought、LLM 响应组、Tool 开始 |
| `ObservationEvent` | Tool 结果、退出码与失败状态 |
| `AgentErrorEvent` | 可恢复 Tool 失败 |
| `ConversationErrorEvent` | Run 失败与错误 Span |
| `ConversationStateUpdateEvent` | 实时 Run 状态 |
| `PauseEvent` / `InterruptEvent` | `waiting_user` |
| `SystemPromptEvent` | 提示词哈希、工具清单；内容按采集策略决定 |
| `Condensation*` | 上下文压缩 Thought |
| Token Callback | 首 Token 时间与流式输出 |
| 未知事件 | `openhands.event.<ClassName>` 兼容 Span |

事件由 `id` 去重，Action/Observation 使用 `tool_call_id` 关联，并行 Tool Call 不会
互相覆盖。Adapter 内部异常只写 debug 日志，不会改变 OpenHands 的执行。

## 运行

先启动完整平台：

```bash
make dev
```

配置 GLM：

```bash
export GLM_API_KEY="..."
export GLM_MODEL="glm-4.7-flash"
make demo-openhands-agentops
```

或配置 DeepSeek：

```bash
export DEEPSEEK_API_KEY="..."
export DEEPSEEK_MODEL="deepseek-chat"
make demo-openhands-agentops
```

也可直接设置 `OPENHANDS_LLM_API_KEY`、`OPENHANDS_LLM_BASE_URL`、
`OPENHANDS_LLM_MODEL` 与 `OPENHANDS_LLM_PROVIDER`。模型名未带 LiteLLM provider
前缀时，脚本会自动补为 `openai/<model>`。

Demo 使用真实 OpenHands `Agent`、`TerminalTool` 与 `FileEditorTool` 完成工作区
检查、一次可恢复失败、文件创建和命令验证。结束时按精确 Run ID 验证 PostgreSQL、
ClickHouse、Redis Stream、SSE 与不可变 Snapshot；任意一项缺失都会以非零状态退出。

## 采集边界

系统提示默认只保存 SHA-256 与工具清单。只有显式设置
`capture_system_prompt=True` 才采集提示词摘要。API Key 不会写入 Snapshot、Span、
SSE 或结果文件。公开的模型思考内容是否采集由 `capture_reasoning` 控制。
