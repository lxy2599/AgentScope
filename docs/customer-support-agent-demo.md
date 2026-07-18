# 客服订单处理 Agent 演示

本文档说明如何运行 AgentScope 系统的**复杂真实 Agent 示例**——客服订单处理 Agent。

## 概述

这个演示展示了一个更复杂的 Agent 场景：客服订单处理。

不同于简单的 QA 评测 demo，这个 Agent 包含完整的业务逻辑流程：

```text
用户输入（如"我的订单 ORD-1001 可以退款吗？"）
     ↓
1. 意图分类 (classify_intent)
   → 识别用户意图：退款、物流查询、售后政策
     ↓
2. 订单查询 (lookup_order)
   → 从内置数据中查找订单
     ↓
3. 政策检索 (retrieve_policy)
   → 从内置知识库检索相关售后/退货/物流政策
     ↓
4. 退款资格判断 (check_refund_eligibility)
   → 根据订单状态、签收时间、政策判断是否可退款
     ↓
5. LLM 生成回复 (generate_final_answer)
   → 综合以上信息，用中文生成专业客服回复
```

Agent 执行时通过 AgentScope SDK 上报：
- Agent 注册和心跳
- Run 生命周期：start → running → waiting_llm → waiting_tool → waiting_retrieval → completed
- 深度 Trace：LLM 调用、工具调用、知识库检索、Skill 执行

## 前置条件

1. 后端服务正在运行：`make dev`
2. 可选：配置 LLM API key（不配置时自动使用 Fake LLM 模式）

## 配置 LLM（可选）

### DeepSeek

```bash
export AGENTSCOPE_LLM_PROVIDER=deepseek
export AGENTSCOPE_LLM_BASE_URL=https://api.deepseek.com/v1
export AGENTSCOPE_LLM_API_KEY=sk-你的API密钥
export AGENTSCOPE_LLM_MODEL=deepseek-chat
```

### GLM（智谱）

```bash
export AGENTSCOPE_LLM_PROVIDER=glm
export AGENTSCOPE_LLM_BASE_URL=https://open.bigmodel.cn/api/paas/v4
export AGENTSCOPE_LLM_API_KEY=你的API密钥
export AGENTSCOPE_LLM_MODEL=glm-4.7-flash
```

### 验证配置

```bash
echo $AGENTSCOPE_LLM_PROVIDER
echo $AGENTSCOPE_LLM_BASE_URL
echo ${AGENTSCOPE_LLM_API_KEY:0:8}...
echo $AGENTSCOPE_LLM_MODEL
```

## 运行演示

```bash
make demo-support-agent
```

或直接执行脚本：

```bash
./scripts/demo-support-agent.sh
```

## 不配置 API key 时的行为

演示不需要 API key 即可运行。未配置 key 时：

- 自动使用 `FakeLLMClient`，不产生真实 API 调用
- Agent 注册、心跳、Run 生命周期、深度 Trace 完全正常
- 后端正常接收 OTLP span，前端可查看完整 Deep Trace
- LLM 调用会显示为 Fake LLM 的摘要信息

如需真实 LLM 调用，配置对应的 API key 环境变量即可。

## 预期输出

### Fake LLM 模式

```
============================================================
AgentScope 客服订单处理 Agent 演示
============================================================

[1/6] 用户注册/登录
  [user] 注册成功: cs-demo-xxxxxxxx@example.com (id=uuid-xxx)
[2/6] 创建项目
  [project] 创建成功: cs-agent-demo-xxxxxx (id=uuid-xxx)
[3/6] 创建 Project Token
  [token] 创建成功: prefix=ascope_xxx
[4/6] 配置 LLM
  [llm] 使用 Fake LLM（不调用真实模型）
[5/6] 注册 Agent 到控制中心（启动心跳）
  [agent] 注册成功: id=uuid-xxx
  [tracing] OpenTelemetry 追踪已配置
[6/6] 运行客服 Agent 演示

  ── 案例 1/4 ──
  [用户]: 我的订单 ORD-1001 可以退款吗？
  [run]: uuid-xxx
  [状态]: running
  [状态]: completed
  [客服]: 您好！关于您订单 ORD-1001（无线蓝牙耳机 Pro）的退款问题...

  ...

[runtime] 关闭 Agent 运行时

============================================================
演示完成！以下是可访问的资源：
============================================================

  登录邮箱:     cs-demo-xxxxxxxx@example.com
  登录密码:     demo-password-123
  前端地址:     http://127.0.0.1:5173
  项目:         http://127.0.0.1:5173/projects/<project-uuid>
  Agent 列表:   http://127.0.0.1:5173/projects/<project-uuid>/agents
```

## 前端查看 Deep Trace

1. 打开 **http://localhost:5173**
2. 使用演示输出中的邮箱和密码登录
3. 在项目主页点击 **Agent 列表**，可看到注册的 `customer-support-agent`
4. Agent 应显示 **在线** 状态（可能很快变为离线，因为演示运行完就 shutdown 了）
5. 在项目主页点击 **运行**，可看到 4 条 Active Runs
6. 点击某条 Run 进入 **Run Detail 页面**，可看到：

| 统计项 | 预期值 | 说明 |
|--------|--------|------|
| LLM 调用 | > 0 | 每次回复生成都有 LLM 调用 |
| 工具调用 | > 0 | `lookup_order` 工具调用记录 |
| 知识库检索 | > 0 | 政策检索有 document_ids 和 scores |
| Skill 调用 | > 0 | `check_refund_eligibility` skill 执行 |

7. Run Detail 的调用概览标签页显示四类调用统计卡片
8. 点击各标签页可查看结构化记录：
   - **LLM 调用**：显示 provider、model、输入摘要、输出摘要
   - **工具调用**：显示工具名称、参数摘要、结果摘要、成功状态
   - **知识库检索**：显示知识库名称、查询、document_ids、scores
   - **Skill 调用**：显示 skill 名称、输入摘要、输出摘要
9. **原始 Span 标签页**仍保留完整的 span 树

## 和离线评测 demo 的区别

| 维度 | 离线评测 demo (make demo) | 复杂 Agent 演示 (make demo-support-agent) |
|------|--------------------------|-------------------------------------------|
| LLM 依赖 | 无（mock target） | 可选（Fake LLM 或真实 LLM） |
| Agent 注册 | 无 | 注册客服 Agent 并上报心跳 |
| 运行状态 | 无 | 通过 SDK runtime 上报状态变化 |
| Trace | 不产生 OTLP trace | 自动采集 LLM/tool/retrieval/skill 四类结构化 span |
| 业务逻辑 | 简单的 QA 问答 | 意图分类、订单查询、政策检索、退款判断 |
| 知识库检索 | 无 | 从内置知识库检索政策，返回 document_ids 和 scores |
| 工具调用 | 无（模拟 tool span） | 真实的 `lookup_order` 本地查询工具 |
| Skill 执行 | 无（模拟 skill span） | 真实的 `check_refund_eligibility` 业务规则 Skill |
| 角色 | 验证评测管线 | 验证完整 AgentOps 闭环 |
| 前端展示 | 实验报告 | Agent 在线、Run 状态、Deep Trace 四类调用 |

## 测试执行

```bash
cd python && python -m pytest tests/test_customer_support_agent.py -v
```

测试覆盖：
- 意图分类（退款、物流、政策、未知）
- 订单查询（命中、未命中、大小写不敏感）
- 知识库检索（document_ids、scores、top_k 限制）
- 退款资格判断（退货期内、超期、未发货、质量问题）
- FakeLLMClient（默认回复、自定义回复、子串匹配）
- 完整流程（退款、物流、未知订单、退款拒绝）
- 数据完整性验证

## 内置测试案例

演示使用 4 个客服场景：

| # | 用户输入 | 意图 | 订单 |
|---|---------|------|------|
| 1 | 我的订单 ORD-1001 可以退款吗？ | refund | ORD-1001（在退货期内） |
| 2 | ORD-1002 什么时候送达？ | delivery | ORD-1002（运输中） |
| 3 | 我买的耳机坏了，能退吗？ | refund | 无具体订单，模糊查询 |
| 4 | ORD-1004 退不了了怎么办？ | refund | ORD-1004（超期） |

## 安全说明

- **不要提交 `.env` 文件**到版本控制
- API key 仅通过环境变量传递，不记录到 span attributes、日志或错误信息中
- Trace attributes 默认只采集摘要和元数据，不采集完整 prompt/response
- 所有 HTTP 请求的 Authorization header 在错误信息中被清洗
