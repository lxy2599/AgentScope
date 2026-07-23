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
`captureContent: true` 才会保存 prompt、最终回答与工具摘要的内容，否则只保存长度或结构摘要。

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

## 验收

运行一轮 OpenClaw Agent 后，项目页面应出现在线 Agent 和运行中的 Run；完成后 Run 状态变为
`completed` 或 `failed`。ClickHouse 中对应 `trace_spans` 至少应包含 `agent.run`、`llm.call`
和实际工具的 `tool.*` span。平台的端到端脚本会额外检查 PostgreSQL Run 与 ClickHouse Trace
的关联。
