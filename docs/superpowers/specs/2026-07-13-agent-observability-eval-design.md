# AgentScope 可观测与评测系统设计

日期：2026-07-13

## 一句话目标

AgentScope 第一版要服务 agent 开发者：团队可以接入 OpenTelemetry 查看一次 agent 运行的完整过程，也可以用 Python runner 跑离线评测，并从失败样本直接跳到对应 trace 做定位。

## 产品边界

第一版做团队共享服务，不做 SaaS 平台。它应该能被一个小团队部署在内网或私有环境里，用一个 Go 服务端、一个 PostgreSQL 和若干用户侧 Python runner 跑起来。

必须完成：

- 接收 OpenTelemetry trace。
- 用统一 `Run` 模型保存真实运行和评测运行。
- 支持项目、用户、项目 token。
- 支持 dataset、experiment、eval job、case result。
- 支持规则 evaluator 和 LLM judge evaluator。
- 提供 Web UI 查看 run、trace、eval 结果和版本对比。
- 提供 Python SDK / runner，让用户在自己的环境执行 agent。

明确不做：

- ClickHouse、Kafka、分布式 trace 后端。
- SaaS 级多租户、复杂 RBAC、审计、计费。
- 在 AgentScope 服务端执行用户代码。
- Prompt 管理平台、模型网关、告警系统、SLO 大盘。
- 完整 OpenTelemetry metrics 后端。

## 技术选型

后端使用 Go。当前仓库已经是 Go module，Go 适合做 OTLP ingestion、API、任务 lease 和并发处理。HTTP 层用 `chi` 这类轻量 router，数据访问用 `pgx + sqlc`，migration 用 `goose`。

数据库只用 PostgreSQL。trace/event 体量先通过项目隔离、索引、分页、保留期和字段截断控制，等真实规模证明需要时再引入 ClickHouse。

前端使用 React + TypeScript + Vite。UI 是开发工具，不做营销页。页面要信息密度高、可扫描、适合反复调试。

Python 侧提供 SDK 和 runner。SDK 负责 target 注册、OTel attribute 注入、规则 evaluator 和 LLM judge 调用；runner 负责从服务端领取 job，在用户自己的环境里执行 agent，并回传结果。

部署形态是 Docker Compose：服务端和 PostgreSQL 在一套 compose 里，runner 独立运行在开发者机器、CI 或私有执行环境。

## 核心架构

系统由四块组成：

1. **Go 单体服务**
   - 用户登录、项目、project token
   - OTLP HTTP trace ingestion
   - runs/spans/events 查询 API
   - datasets/experiments/eval jobs API
   - runner job claim/heartbeat/results/fail API
   - 静态前端资源托管

2. **PostgreSQL**
   - 保存用户、项目、token、runs、spans、events
   - 保存 datasets、cases、experiments、jobs、results
   - JSONB 保存 attributes、metadata 和 evaluator detail
   - 常用查询字段冗余为普通列并加索引

3. **Python SDK / Runner**
   - 用装饰器注册被测 agent target
   - 执行 dataset case
   - 生成或补充 OTel trace
   - 执行规则 evaluator 和 LLM judge
   - 把 output、score、reason、run/trace 关联回传服务端

4. **Web UI**
   - 项目首页：recent runs、recent evals、最近失败
   - run 详情：span tree、时间线、LLM/tool 详情、错误、raw attributes
   - eval 详情：分数、失败样本、evaluator reason、关联 trace
   - 对比视图：两个 experiment 按 case 对比

## 统一 Run 模型

`Run` 是第一版最重要的设计约束。真实运行和评测样本都必须进入 `runs`。

`runs` 需要表达：

- 属于哪个项目
- 来源是 `live` 还是 `eval`
- 当前状态
- trace id
- agent 名称和版本
- 输入/输出摘要
- 开始/结束时间
- 延迟和成本

`spans` 保存 OTel span 的结构化字段：trace id、span id、parent span id、名称、kind、状态、时间、attributes。

`span_events` 保存工具调用、LLM token usage、retrieval hit、错误、阶段性日志等事件。

评测结果通过 `eval_case_results.run_id` 关联到 run。这样 UI 不需要在 eval 和 trace 之间做脆弱匹配，失败样本可以直接跳转到 trace。

## 真实运行数据流

1. agent 应用通过 OpenTelemetry SDK 创建 trace。
2. Python SDK 或用户代码在 span attributes 中写入 AgentScope 约定字段，例如 run name、agent name、agent version。
3. agent 把 trace 通过 OTLP HTTP 发送到 `/v1/traces`。
4. AgentScope 用 project token 鉴权。
5. ingestion 层解析 trace id 和 attributes。
6. 如果已有 run id，就把 spans 归到该 run；否则按 trace id 自动创建一个 `live` run。
7. UI 查询 run、span tree、events 和错误详情。

## 离线评测数据流

1. 用户在 UI/API 创建 dataset。
2. 用户基于 dataset、target、agent version、evaluator set 创建 experiment。
3. 服务端按 case 生成 eval jobs。
4. Python runner 用 project token 拉取 job。
5. runner 在用户环境调用 SDK 注册的 target。
6. target 执行时上报 OTel trace。
7. runner 执行规则 evaluator 和 LLM judge evaluator。
8. runner 回传 actual output、score、pass/fail、reason、trace/run 关联。
9. 服务端更新 experiment 聚合指标。
10. UI 展示失败样本，并链接到对应 run trace。

## Python SDK 和 Runner

用户侧代码的目标形态：

```python
@agentscope.target(name="support-agent", version="2026-07-13")
def run_agent(case_input, context):
    return my_agent.invoke(case_input)
```

runner 启动方式：

```bash
agentscope-runner start \
  --project-token "$AGENTSCOPE_PROJECT_TOKEN" \
  --server-url "https://agentscope.example.com"
```

runner 协议：

- `GET /runner/jobs/claim`：领取 job。
- `POST /runner/jobs/{id}/heartbeat`：续租。
- `POST /runner/jobs/{id}/results`：提交输出和评测结果。
- `POST /runner/jobs/{id}/fail`：提交 runner 或 target 异常。

job 必须有 lease。runner 掉线后，lease 过期的 job 可以重新领取。case 执行失败只影响该 case，不应该让整个 experiment 不可读。

## Evaluator 设计

规则 evaluator 第一版支持：

- exact match
- regex match
- contains / not contains
- JSONPath assertion
- numeric threshold
- tool call sequence assertion
- latency / cost threshold

每个 evaluator 输出统一结构：

- `score`
- `pass`
- `reason`
- `details`

LLM judge 使用统一 provider 接口。第一版实现：

- OpenAI-compatible provider
- Anthropic-compatible provider

judge 配置包含 provider、model、rubric、score scale、pass threshold、temperature、max tokens、retry policy。temperature 默认 0。

LLM judge 必须返回结构化 JSON。解析失败是 evaluator error，不能静默当成普通失败样本。系统需要保存 prompt 模板版本、模型、请求参数、响应摘要、token usage、score 和 reason。API key 不直接入库，只保存环境变量名。

## UI 信息架构

登录后直接进入工具界面。

页面包括：

- Projects：项目列表、创建项目、成员、token。
- Project Home：recent live runs、recent evals、最近失败、简要 latency/cost/error 指标。
- Runs：run 列表，按状态、来源、agent、版本、耗时、错误筛选。
- Run Detail：span tree、时间线、LLM 调用、tool 调用、events、raw attributes。
- Datasets：dataset 列表、case 列表、case 编辑、JSONL 导入。
- Experiments：创建 experiment、查看聚合指标、case 结果、对比两个 experiment。
- Runner：runner 连接说明、heartbeat、claimed jobs、失败信息。
- Settings：用户、成员、tokens、judge provider 引用。

UI 的关键体验是“从失败到证据”：eval case 失败原因旁边必须能直接打开对应 run trace。

## 错误处理

- 无效 project token 返回 401。
- OTLP 数据缺少 run id 时，用 trace id 自动创建 live run。
- 超大 attributes/event payload 截断，并记录 `truncated=true`。
- runner job 用 lease 和 heartbeat 防重复执行。
- runner 失联后 job 可重试。
- target 抛错记录为 case error。
- 规则 evaluator 配置错误记录为 evaluator error。
- LLM judge 调用失败按 retry policy 重试，最终失败保留错误摘要。
- retention 可以清理旧 spans/events，但 eval summary 保留。

## 测试策略

Go 后端：

- repository 集成测试
- OTLP payload 到 run/span/event 的映射测试
- runner job 并发 claim 测试
- auth/project token 测试
- experiment 聚合测试

Python：

- target 注册和调用测试
- evaluator 单元测试
- runner 协议 mock server 测试
- OTel attribute 注入测试

前端：

- API client 和关键状态测试
- Playwright 覆盖登录、run 详情、dataset、experiment、失败样本跳 trace

端到端：

- Docker Compose 启动服务和 PostgreSQL
- 示例 Python agent 跑一组 dataset
- 验证 experiment result 和 linked trace 都能通过 API/UI 查到

## MVP 验收

第一版完成时，一个团队应能：

1. 创建用户、项目和 project token。
2. 通过 Python SDK 或 OTel 上报一次 agent run。
3. 在 UI 里查看 trace、LLM 调用、tool 调用和错误。
4. 创建 dataset 和 experiment。
5. 启动远程 Python runner。
6. 同时运行规则 evaluator 和 LLM judge。
7. 查看失败原因，并跳转到对应 trace。
8. 对比两个 experiment 在 case 级别的变化。
