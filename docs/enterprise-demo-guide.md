# AgentScope 企业演示与验收指南

## 1. 演示原则

当前主演示只有一个：`make demo-phase4-agentops`。

它使用同一账号、同一项目和同一组资源串联平台全部核心能力，避免过去多个 Demo 各自
创建账号和项目、无法形成完整故事的问题。旧 Demo 仍用于兼容性回归，但不能证明
Phase 4 完成。

主演示必须满足：

1. 使用固定提交的真实 OpenHands 上游源码，且运行后源码工作区保持干净；
2. 通过公开 callback 和 Python SDK 接入，不在平台内重新实现一个假 Agent；
3. Run 执行期间通过 SSE 展示非终态状态和语义事件；
4. 语义 Trace 包含思考、LLM、工具、终端、文件、恢复和最终答案；
5. 同一 Run 关联 Snapshot、Rubric、多 Judge、Gold、校准和实验对比；
6. PostgreSQL、ClickHouse、Redis、API 和页面使用精确 ID 相互印证；
7. 任一必要证据缺失时命令以非零状态退出。

## 2. 启动平台

环境要求：Go 1.23+、Python 3.10+、Node.js 20.19+、Docker 和 Docker Compose。

```bash
make dev
```

默认地址：

| 组件 | 地址 |
|---|---|
| 前端 | `http://127.0.0.1:5173` |
| 后端 | `http://127.0.0.1:8080` |
| 健康检查 | `http://127.0.0.1:8080/healthz` |

`make dev` 会启动 PostgreSQL、ClickHouse 和 Phase 4 Redis overlay。发生端口冲突时，
开发脚本会选择可用宿主机端口。

## 3. 配置真实模型

统一演示需要 OpenAI 兼容模型，并要求显式提供每百万 Token 单价：

```bash
export OPENHANDS_LLM_PROVIDER=openai-compatible
export OPENHANDS_LLM_BASE_URL=https://example.com/v1
export OPENHANDS_LLM_API_KEY=你的密钥
export OPENHANDS_LLM_MODEL=你的模型
export OPENHANDS_LLM_INPUT_COST_PER_MILLION=输入单价
export OPENHANDS_LLM_OUTPUT_COST_PER_MILLION=输出单价
```

单价用于生成 `estimated_cost`，记录同时带有 `cost_source=estimated`。该值不是供应商
账单。API Key 仅存在于演示进程环境中，不写入结果文件、数据库或日志。

## 4. 运行统一 Demo

```bash
make demo-phase4-agentops
```

首次运行会将固定版本 OpenHands 下载到 `.external/` 并建立隔离虚拟环境。脚本随后：

1. 创建用户、项目和 Project Token；
2. 注册 OpenHands Agent 和能力，启动心跳及 SSE 监听；
3. 执行真实软件工程任务并持续上报 Runtime 与语义 Trace；
4. 验证运行中的事件先于完成事件到达；
5. 验证不可变 Snapshot 和 SHA-256；
6. 创建 Dataset、版本化 Rubric、Baseline 和 Candidate；
7. 保存多 Judge 投票、聚合置信度、分歧和安全 veto；
8. 写入人工 Gold 并生成校准报告；
9. 计算实验差异、效应量、统计显著性和回归门禁；
10. 生成配额、SDK 背压、成本性能、Rollup 和异常证据；
11. 执行 OTLP、Runtime 和 SSE 小规模并发流量；
12. 强校验 PostgreSQL、ClickHouse、Redis 和 OpenHands 工作区。

结果保存在 `.external/phase4-agentops-result.json`。该文件不会进入 Git，也不包含
Project Token 或模型密钥。

## 5. 前端讲解顺序

使用 Demo 输出的唯一账号登录。推荐按以下顺序演示：

1. **Agent 控制中心**：实例在线状态、心跳、能力和当前 Run；
2. **Active Runs**：SSE 连接状态、运行阶段和实时事件；
3. **Run Detail**：思考、LLM、工具、终端、文件、恢复和最终答案时间线；
4. **Run Snapshot**：模型、Prompt、工具、代码和环境摘要及哈希；
5. **Candidate 实验**：逐 Judge 投票、错误票、分歧、置信度和 veto；
6. **Judge 校准**：人工 Gold、准确率和校准结果；
7. **实验对比**：质量差异、显著性和回归门禁；
8. **采集健康**：配额、背压、Token、TTFT、成本、Rollup 和异常。

页面路由和账号由结果文件提供，不要手工把 `ingest` 等标签当作 `project_id` 拼接 URL。

## 6. 自动化页面验收

```bash
cd web
AGENTSCOPE_API_URL=http://127.0.0.1:8080 \
PHASE4_RESULT_FILE=../.external/phase4-agentops-result.json \
npm run test:e2e
```

Playwright 同时验证桌面端 `1440x900` 和移动端 `390x844`：

- 登录及项目权限正确；
- 关键页面能加载；
- 不出现 API 403/500；
- 实时、评测和治理信息存在；
- 页面没有横向溢出。

## 7. 数据强校验

统一 Demo 会自动执行：

```bash
scripts/verify-phase4-data.sh \
  --result-file .external/phase4-agentops-result.json
```

校验证据：

| 系统 | 证据 |
|---|---|
| PostgreSQL | Run、状态历史、Snapshot、Outbox、Rubric、Judge 投票与聚合、Gold、Calibration、Comparison、Quota、Anomaly |
| ClickHouse | Span、Thought、LLM、Tool、Final Answer、正耗时、Token、TTFT、估算成本、Rollup、SDK 背压 |
| Redis | 项目 Stream 包含精确 Run，Outbox 无积压 |
| OpenHands | 提交固定，上游源码无本地修改 |

使用非默认库时：

```bash
export AGENTSCOPE_POSTGRES_DATABASE=你的数据库
export AGENTSCOPE_CLICKHOUSE_DATABASE=你的数据库
```

脚本仅按结果文件中的精确资源 ID 查询，不接受“库中存在任意旧数据”作为通过。

## 8. 容量与恢复

```bash
make phase4-load-smoke
make phase4-recovery-test
```

容量报告包含吞吐、P50/P95/P99、错误率、丢弃率和测试配置。恢复测试证明 Redis
不可用期间权威写入继续成功，恢复后 Outbox 被重放，SSE 投影最终一致。详细参数见
[Phase 4 容量与恢复测试](phase4-capacity-testing.md)。

## 9. 旧 Demo 定位

以下命令保留用于局部能力和向后兼容回归：

| 命令 | 范围 |
|---|---|
| `make demo-real-agent` | 简单真实 LLM + Runner |
| `make demo-support-agent` | 客服工具、检索和 Skill |
| `make demo-multi-eval` | 基础多维评测 |
| `make demo-hermes-agentops` | Hermes Adapter |
| `make demo-openhands-agentops` | OpenHands Adapter 局部验证 |
| `make demo-ingest-health` | 采集成功/失败统计 |

这些 Demo 会创建独立账号和项目，适合回归单项功能，不适合完整产品演示。

## 10. 故障定位

### 页面请求出现 403

- 确认使用当前 Demo 输出的账号登录；
- 确认 URL 中是实际 UUID，不是 `ingest`、`runs` 等路由片段；
- 确认资源属于该账号所在项目；
- Playwright 会将任何 403 视为失败。

### 缺少耗时、Token 或成本

- Adapter 必须使用上游公开 metrics 或 callback 数据；
- LLM duration 必须为正，内部事件不能伪装成 LLM；
- 成本单价缺失时统一 Demo 会提前失败；
- 使用强校验脚本按本次 Run 查询 ClickHouse。

### 页面完成后才出现 Trace

- 检查 Redis、Outbox Relay 和 SSE 连接状态；
- 检查客户端 Cursor、reset 和轮询降级；
- 确认语义事件在 ClickHouse 成功写入后才发出；
- 运行 `make phase4-recovery-test` 验证最终一致性。

### 评测结果缺少投票或校准

- 确认使用版本化 Rubric；
- 检查每个 Judge 的 vote、confidence、error 和 veto；
- 检查 Gold 与 Calibration 是否属于同一项目和 Rubric；
- 统一 Demo 不允许用单一综合分替代逐 Judge 证据。
