# Phase 4 企业 AgentOps 统一演示

## 演示目标

`make demo-phase4-agentops` 使用一个账号和一个项目串联第四阶段的完整能力，
不再要求分别登录多个互不关联的 Demo 项目。

演示会运行固定版本的真实 OpenHands Software Agent SDK，并通过公开 callback
接入 AgentScope Python SDK。OpenHands 上游源码不会被修改。完整流程包括：

1. 注册账号、创建项目和 Project Token；
2. 注册 OpenHands Agent、上报心跳并建立 SSE 监听；
3. 运行真实软件工程任务，记录思考、LLM、工具和最终答案；
4. 校验运行中的非终态事件先于完成事件到达；
5. 校验不可变 Run Snapshot 及其 SHA-256；
6. 创建 Dataset、版本化 Rubric 和双 Judge 实验；
7. 保存逐 Judge 投票、聚合置信度、人工 Gold 和校准报告；
8. 比较 Baseline/Candidate 并计算统计显著性；
9. 生成 SDK 背压、项目配额、成本性能和运行异常证据；
10. 执行同项目 OTLP、Runtime 和 SSE 小规模并发流量；
11. 使用精确 ID 校验 PostgreSQL、ClickHouse、Redis 和 OpenHands 工作区。

任一环节缺失都会使命令以非零状态退出。

## 前置条件

- Go 1.23+、Python 3.10+、Node.js 20+；
- Docker 和 Docker Compose；
- 可访问 OpenAI 兼容 API 的模型；
- 已通过 `make dev` 启动前后端和 PostgreSQL、ClickHouse、Redis。

OpenHands 固定在提交
`a375c66aaf29c8d3b6553c9164e65ed917149e81`。首次运行会将源码和虚拟环境放在
`.external/`，它们不会进入 Git。

## LLM 与价格配置

演示必须显式配置模型单价。单价用于计算 `estimated_cost`，不会被标记为供应商
实际账单。请使用供应商当前公布的价格，不要把 API Key 或价格写进源码。

```bash
export OPENHANDS_LLM_PROVIDER=openai-compatible
export OPENHANDS_LLM_BASE_URL=https://example.com/v1
export OPENHANDS_LLM_API_KEY=你的密钥
export OPENHANDS_LLM_MODEL=你的模型
export OPENHANDS_LLM_INPUT_COST_PER_MILLION=输入单价
export OPENHANDS_LLM_OUTPUT_COST_PER_MILLION=输出单价
```

也可以继续使用现有 `GLM_API_KEY` 或 `DEEPSEEK_API_KEY` 自动映射鉴权和模型，
但每百万 Token 的输入、输出单价仍必须通过上述两个变量明确提供。

## 运行

```bash
make demo-phase4-agentops
```

结果写入 `.external/phase4-agentops-result.json`。文件只包含本次 Demo 的资源 ID、
演示账号和前端链接，不包含 Project Token 或 LLM Key。

## 页面验收

结果文件中的同一账号可以访问以下页面：

- Agent 控制中心：在线状态、实例、当前 Run 和实时连接状态；
- OpenHands Run：时间线、工具内容、LLM Token/TTFT/成本和运行快照；
- Candidate 实验：多 Judge 投票、错误票、分歧和聚合置信度；
- Judge 校准：Gold 样本及校准报告；
- 实验对比：质量差异、统计显著性和回归门禁；
- 采集健康：配额、背压、成本性能、异常和采集诊断。

自动化浏览器验收：

```bash
cd web
AGENTSCOPE_API_URL=http://127.0.0.1:8080 \
PHASE4_RESULT_FILE=../.external/phase4-agentops-result.json \
npm run test:e2e
```

Playwright 同时验证 1440×900 桌面端和 390×844 移动端，并拒绝 API 403/500、
文档横向溢出和关键视图缺失。

## 数据强校验

统一命令已自动调用：

```bash
scripts/verify-phase4-data.sh \
  --result-file .external/phase4-agentops-result.json
```

校验范围：

| 存储 | 必须存在的证据 |
|---|---|
| PostgreSQL | Run、状态历史、Snapshot、Outbox、Rubric、Judge Vote/Aggregation、Gold、Calibration、Comparison、Quota、Anomaly |
| ClickHouse | Span、Thought、LLM、Tool、Final Answer、正 LLM 耗时、Token、TTFT、估算成本、Rollup、SDK 背压 |
| Redis | 当前项目 Stream 中包含精确 OpenHands Run，且 Outbox 无积压 |
| OpenHands | 上游提交固定且工作区无源码修改 |

校验脚本通过 Docker Compose 服务名访问数据库。使用非默认测试库时，需要同时传入：

```bash
export AGENTSCOPE_POSTGRES_DATABASE=你的数据库
export AGENTSCOPE_CLICKHOUSE_DATABASE=你的数据库
```

## 安全边界

- LLM Key 仅从环境变量读取；
- Project Token 只存在于进程内的临时配置，结果文件不保存；
- Snapshot 和 SSE Payload 经过脱敏；
- OpenHands 使用公开 callback 和 metrics，不 monkey patch、不修改上游源码；
- Redis 只承载实时投影，业务事实仍以 PostgreSQL/ClickHouse 为准；
- `.external/`、`.tmp/`、`web/test-results/` 均不提交。
