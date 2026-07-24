# 真实 LLM Agent 演示

本文档说明如何运行 AgentScope 系统的**真实 LLM Agent 评测演示**。

## 概述

真实 Agent 演示将普通评测管线升级为完整闭环：

```text
make demo-real-agent
    ↓
检查 LLM 环境变量配置
    ↓
创建用户 / 项目 / Project Token / 数据集 / 实验
    ↓
Python Runner claim job
    ↓
真实 Agent 执行 case（调用 DeepSeek / GLM）
    ↓
Python SDK 生成 OpenTelemetry spans
    ↓
OTLP HTTP 发送到后端 /v1/traces
    ↓
后端写入 ClickHouse（trace_spans + trace_span_events）
    ↓
Runner 通过 trace_id 查询后端 PostgreSQL run_id
    ↓
Runner 提交 eval result，携带 run_id
    ↓
前端实验结果可跳转到 Run Detail
    ↓
Run Detail 显示 span tree / events / attributes
```

## 前置条件

1. 后端服务正在运行：`make dev`
2. 配置了 LLM API key（DeepSeek 或 GLM）

## 配置 LLM

### DeepSeek

```bash
export AGENTSCOPE_LLM_PROVIDER=deepseek
export AGENTSCOPE_LLM_BASE_URL=https://api.deepseek.com/v1
export AGENTSCOPE_LLM_API_KEY=sk-你的API密钥
export AGENTSCOPE_LLM_MODEL=deepseek-chat
```

### GLM (智谱)

```bash
export AGENTSCOPE_LLM_PROVIDER=glm
export AGENTSCOPE_LLM_BASE_URL=https://open.bigmodel.cn/api/paas/v4
export AGENTSCOPE_LLM_API_KEY=你的API密钥
export AGENTSCOPE_LLM_MODEL=glm-4.7-flash
```

> GLM-4.7-Flash 支持 reasoning/thinking 模式。Demo 会在 GLM 调用时自动启用 `thinking: {"type": "enabled"}`，并设置 `max_tokens: 65536`。


### 验证配置

```bash
echo $AGENTSCOPE_LLM_PROVIDER
echo $AGENTSCOPE_LLM_BASE_URL
test -n "${AGENTSCOPE_LLM_API_KEY:-}" && echo "AGENTSCOPE_LLM_API_KEY 已设置"
echo $AGENTSCOPE_LLM_MODEL
```

## 运行演示

```bash
make demo-real-agent
```

或直接执行脚本：

```bash
./scripts/demo-real-agent.sh
```

## 预期输出

成功运行的输出示例：

```
============================================================
AgentScope 真实 LLM Agent 演示
============================================================

[验证] LLM 配置
  [llm] 配置验证通过

[1/8] 用户注册/登录
  [user] 注册成功: demo-xxxxxxxx@example.com (id=uuid-xxx)

[2/8] 创建项目
  [project] 创建成功: real-agent-demo-xxx (id=uuid-xxx)

[3/8] 创建 Project Token
  [token] 创建成功: prefix=ascope_xxx

[4/8] 创建数据集和测试用例
  [case] 添加成功: ...
  [case] 添加成功: ...
  [case] 添加成功: ...
  [case] 添加成功: ...
  [cases] 共 4 个用例

[5/8] 创建实验
  [experiment] 创建成功: target=real-agent (id=uuid-xxx)

[6/8] 注册真实 Agent
[7/8] 执行评测（真实 LLM 调用中…）
  [runner] 完成 1/4
  [runner] 完成 2/4
  [runner] 完成 3/4
  [runner] 完成 4/4
  [runner] 评测完成，处理了 4 个任务

[8/8] 查看结果
  实验状态: completed (通过: N/4)
  实验结果数量: 4
    ✓ score=1.00 Exact match: 'Paris' == 'Paris' → /runs/<run-id>
    ✓ score=1.00 Exact match: '4' == '4' → /runs/<run-id>
    ✓ score=1.00 Exact match: 'blue' == 'blue' → /runs/<run-id>
    ✓ score=1.00 Exact match: 'Shakespeare' == 'Shakespeare' → /runs/<run-id>

============================================================
演示完成！以下是可访问的资源：
============================================================
```

## 缺失 API key 时的行为

如果 `AGENTSCOPE_LLM_API_KEY` 未设置，演示会立即失败：

```text
[demo-real-agent] 检查 LLM 配置…
[demo-real-agent]   MISSING: AGENTSCOPE_LLM_API_KEY
[demo-real-agent] 错误:
LLM 环境变量未完全配置。请设置：

  export AGENTSCOPE_LLM_PROVIDER=deepseek
  export AGENTSCOPE_LLM_BASE_URL=https://api.deepseek.com/v1
  export AGENTSCOPE_LLM_API_KEY=你的API密钥
  export AGENTSCOPE_LLM_MODEL=deepseek-chat

...
注意：本演示不提供 mock fallback，缺失 API key 将直接失败。
```

- 命令以非零状态码退出
- 不出现 panic
- 不泄漏 API key

## 前端查看

1. 打开 **http://localhost:5173**
2. 使用演示输出中的邮箱和密码登录
3. 在项目主页点击实验进入实验详情页
4. 在实验结果表格中，每条结果都有 Trace 按钮
5. 点击 Trace 按钮跳转到 Run Detail 页面
6. Run Detail 显示 span 树，应包含以下 spans：

| Span 名称 | 说明 |
|---|---|
| `agent.run` | 根 span，包含 agentscope.* 属性 |
| `agent.prepare_input` | 准备 prompt 和 system message |
| `llm.generate_response` | 调用 Chat Completions API |
| `evaluator.compare_answer` | 对比输出与预期答案 |
| `agent.finish` | Agent 完成，记录输出摘要 |

`llm.generate_response` span 的 attributes：

| Attribute | 说明 |
|---|---|
| `llm.provider` | Provider 名称（deepseek / glm） |
| `llm.model` | 模型名称 |
| `llm.base_url` | API base URL |
| `llm.prompt_preview` | 输入摘要（前 200 字符） |
| `llm.output_preview` | 输出摘要（前 500 字符） |
| `llm.latency_ms` | 请求耗时（毫秒） |
| `llm.prompt_tokens` | Prompt token 数（如果 provider 返回） |
| `llm.completion_tokens` | 输出 token 数 |
| `llm.total_tokens` | 总 token 数 |

## 安全说明

- **不要提交 `.env` 文件**到版本控制。
- API key 仅通过环境变量传递，**不记录到 span attributes、日志或错误信息中**。
- trace attributes 包含的是 LLM 调用的摘要和元数据，不是完整的 prompt/response。
- 所有 HTTP 请求的 Authorization header 会在错误信息中被清洗，不会泄漏 key。

## 与普通演示的区别

| 维度 | 普通演示 (make demo) | 真实 Agent 演示 (make demo-real-agent) |
|---|---|---|
| LLM 依赖 | 无（mock target） | 需要 DeepSeek / GLM key |
| Trace 采集 | 不产生 OTLP trace | 自动采集 span → ClickHouse |
| Run Trace 关联 | run_id 为空 | 每个结果都有 run_id |
| 适用场景 | 验证评测管线 | 验证完整闭环可观测 |
| 失败模式 | 无 LLM 相关错误 | 缺 key 直接报错 |

## 客服订单处理 Agent 演示

另有更复杂的 **客服订单处理 Agent 演示** (`make demo-support-agent`)，展示包含意图
分类、订单查询、政策检索和退款判断的完整业务流程。

详见 [docs/customer-support-agent-demo.md](customer-support-agent-demo.md)。
