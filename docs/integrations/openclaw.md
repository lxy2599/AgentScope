# OpenClaw 接入

本仓库提供 `integrations/openclaw/agentscope-observability`，它是一个基于 OpenClaw 原生
Plugin SDK 的可安装插件。实现基准为 OpenClaw 提交
`ee07bb8e66f86579ab113dfe4ba2b856b076c494`，只使用公开的 `api.on(...)` hook。

## 采集范围

- `before_agent_run` 和 `agent_end`：创建并结束 AgentScope Run。
- `model_call_started`/`model_call_ended` 与 `llm_input`/`llm_output`：LLM 模型、耗时、
  usage、输入输出摘要。
- `before_tool_call`/`after_tool_call`：工具参数、结果、错误和耗时。
- 名称包含 `memory` 或 `skill` 的工具额外投影为 MEMORY/SKILL 语义 span。
- `gateway_start`/`gateway_stop`：注册 Agent、报告在线和离线状态、刷新 OTLP 缓冲。

插件不会伪造思考链；OpenClaw 没有公开推理文本 hook 时只记录其公开的模型输出。设置
`captureContent: true` 才会保存 prompt、最终回答与工具摘要的内容。默认的
`captureContent: false` 会在控制面 API 中返回 `[REDACTED]`，并省略可能携带敏感内容的
LLM/tool span 摘要；错误和日志中的内容也会脱敏。

## 安装

OpenClaw 当前上游版本要求 Node `>=22.22.3`。本机可使用已安装的 Node 24：

```bash
source ~/.nvm/nvm.sh
nvm use 24.15.0
scripts/setup-openclaw-agent.sh
```

在 OpenClaw 配置中启用该插件，并将以下值放在插件私有配置中。Project Token 必须包含
`otlp:write`、`agent:register`、`agent:runtime` 三个 scope，不要提交密钥。

```json
{
  "plugins": {
    "entries": {
      "agentscope-observability": {
        "enabled": true,
        "hooks": {
          "allowConversationAccess": true
        },
        "config": {
          "serverUrl": "http://127.0.0.1:8080",
          "projectToken": "ascope_...",
          "agentName": "openclaw-gateway",
          "captureContent": false
        }
      }
    }
  }
}
```

OpenClaw 本地开发插件目录应指向
`integrations/openclaw/agentscope-observability`。插件用标准 OTLP/protobuf HTTP exporter 向
`/v1/traces` 发送 Trace，同时通过 SDK runtime API 写入实时 Run 状态；两个存储面通过相同的
`trace_id` 和 AgentScope `run_id` 关联。

如果 OpenClaw 所在环境不能直连模型供应商，可在 OpenClaw 配置顶层设置官方支持的代理：

```json
{
  "proxy": { "proxyUrl": "http://127.0.0.1:7897" }
}
```

插件对 AgentScope 的控制面请求有 5 秒超时，网络异常只会记录告警，不会阻塞 Agent 本身。
最终答案优先使用 OpenClaw 的 `agent_end.messages`，因此即使模型输出 hook 未携带完整文本，Run
仍会以非空 output 完成入库。

## Benchmark 关联（可选）

运行 benchmark 时，可在启动 OpenClaw Gateway 的进程环境中设置以下变量，以将 AgentScope Run
和 OpenTelemetry `agent.run` 根 span 关联到一次评测：

```bash
export AGENTSCOPE_EXPERIMENT_ID="..."
export AGENTSCOPE_EVAL_JOB_ID="..."
export AGENTSCOPE_EVAL_TRIAL_ID="..."
export AGENTSCOPE_BENCHMARK_KEY="tau2-bench"
export AGENTSCOPE_BENCHMARK_TASK_ID="retail-001"
export AGENTSCOPE_TRIAL_INDEX="0"
```

插件只读取非空、长度受限的标识符；`AGENTSCOPE_TRIAL_INDEX` 还必须是非负整数。它会将可用字段
写入 Run 的 `metadata`（`experiment_id`、`eval_job_id`、`eval_trial_id`、`benchmark_key`、
`benchmark_task_id`、`trial_index`）以及根 span 的等价 `agentscope.*` 属性。未设置或无效时，插件
行为与普通 OpenClaw Run 完全一致。该关联仅携带评测标识，不会开启或扩大 prompt、回答或工具内容采集。

## 验收

运行一轮 OpenClaw Agent 后，项目页面应出现在线 Agent 和运行中的 Run；完成后 Run 状态变为
`completed` 或 `failed`。ClickHouse 中对应 `trace_spans` 至少应包含 `agent.run`、`llm.call`
和实际工具的 `tool.*` span。平台的端到端脚本会额外检查 PostgreSQL Run 与 ClickHouse Trace
的关联。
