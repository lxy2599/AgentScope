# AgentScope 统一 Agent Benchmark 接入设计与交付计划

## 1. 目标

在不新建第二套评测系统或消息队列的前提下，使 AgentScope 能以产品化方式创建、运行、观测、比较和回归真实 Agent benchmark。首批必须真实跑通：

- Harbor 驱动的终端/容器任务，分别运行 OpenHands 和 OpenClaw；
- τ²-bench 的官方环境、用户模拟器和 verifier；
- 现有 function evaluation 的完全兼容；
- 从 UI 创建实验、观察每个 trial、访问 trace/artifact、理解失败原因的完整闭环。

后续扩展 BFCL、AgentDojo/Inspect、BrowserGym/WebArena、AppWorld、OlaBench、私有 golden set、OSWorld 和 AndroidWorld 时，应只增加 Harness Driver，而不是复制队列、权限、结果模型或前端信息架构。

## 2. 已冻结的架构决策

### 2.1 复用一条评测链路

```
Dataset / Benchmark Suite
        ↓
Experiment
        ↓
eval_jobs（PostgreSQL claim / lease / heartbeat）
        ↓
Python Runner Executor Registry
        ↓
Official Harness + real Agent
        ↓
Official verifier / artifacts / AgentScope trace
        ↓
eval_case_results + experiment aggregation + UI
```

下列现有能力继续是唯一权威实现：

- `experiments`：实验生命周期、聚合、比较；
- `eval_jobs`：耐久任务队列、租约、心跳、基础设施重试；
- `/runner`：claim、heartbeat、submit、fail 协议；
- `eval_case_results`：单次执行的分数、输出、原因和 trace 关联；
- Go：控制面、鉴权、配额、数据库、API、产品界面所需数据；
- Python：执行不受信任的 Agent/Harness 进程。

**不新增 Kafka、RabbitMQ、Redis 队列或独立 benchmark worker 服务。** Redis 可以继续承担既有的实时推送/限流，不承担 benchmark 的事实队列。

### 2.2 一个 `eval_job` 是一个独立 trial

同一 case 的独立重复运行通过 `trial_index` 区分：

```
case A × trials_per_case=3
  → job(A, trial_index=0)
  → job(A, trial_index=1)
  → job(A, trial_index=2)
```

`eval_jobs.attempts` 只表示同一个 trial 的基础设施重试；`trial_index` 才是 pass@1、pass^k、方差和稳定性统计的样本维度。第一版不引入 `eval_trials` 表，避免两份运行状态失真。

### 2.3 官方 Harness 是环境与评分的权威

AgentScope 不复制以下实现：

- Harbor 的 Docker 环境、任务安装、任务 reset 和 verifier；
- τ²-bench 的领域数据库、工具服务、用户模拟器、reward 组合；
- BrowserGym/WebArena、AgentDojo、OSWorld 等项目的环境和 verifier。

AgentScope 的 Harness Driver 仅负责：

1. 将一个已 claim 的 job 转换为官方 harness 的最小运行配置；
2. 注入 AgentScope benchmark correlation context；
3. 启动并监视官方进程；
4. 解析官方结果、日志和 artifact manifest；
5. 将正式 verdict 标准化后回写现有结果模型。

## 3. 执行模型

### 3.1 运行类型

`eval_jobs.execution_kind` 决定 Python Runner 使用哪个 Executor：

| execution_kind | Executor | 用途 |
| --- | --- | --- |
| `function` | `FunctionExecutor` | 现有 Python target callable，保持原行为 |
| `harbor` | `HarborExecutor` | 官方 Harbor（Terminal-Bench 等） |
| `tau2` | `Tau2Executor` | 官方 τ²-bench |
| `browsergym` | `BrowserGymExecutor` | 后续浏览器环境 |
| `inspect` | `InspectExecutor` | 后续安全/治理评测 |
| `native_benchmark` | `NativeBenchmarkExecutor` | 内部 contract/golden-set driver |

Executor Registry 未注册的类型必须以 `unsupported_execution_kind` 失败，而不能悄悄降级为普通函数评测。

### 3.2 单个 job 的端到端过程

1. 用户在 UI 选择一个 `benchmark_suite`、Agent target、运行配置和 `trials_per_case`。
2. Go 控制面验证 suite 与 dataset 的项目归属，创建 experiment；每个 case/trial 生成一个 `eval_job`，固化 `execution_kind`、`execution_config`、`seed`、`trial_index`。
3. 项目管理员在实验详情页显式确认“本机启动”后，Go 控制面启动并监管相应执行类型的 Python Runner；Runner 从已有 `/runner/claim` 获得 job、case、target、suite snapshot 与执行配置，并持续 heartbeat。
4. Executor 创建隔离的本地工作目录和 artifact manifest。
5. Executor 以官方 CLI/API 启动 Harness；Harness 自己创建任务环境、调用真实 Agent、运行 verifier 并清理容器/领域环境。
6. Agent 进程获得可选的 `AGENTSCOPE_*` correlation 环境变量。OpenHands/OpenClaw integration 将其写入 Run、root trace 和 Snapshot metadata。
7. Executor 解析官方结果：有效且通过/不通过都 `submit_result`；环境、Harness、Agent 启动或 verifier 协议错误则 `fail_job`。
8. Go 聚合 experiment，前端显示任务与 trial 状态、official metrics、pass@k、trace、artifact 和 error taxonomy。

### 3.3 结果语义

| 状态 | 代表的含义 |
| --- | --- |
| job `succeeded` + `passed=true` | 官方 verifier 已运行，Agent 通过任务 |
| job `succeeded` + `passed=false` | 官方 verifier 已运行，Agent 未通过任务 |
| job `failed` | 环境、Harness、Agent 启动、结果协议或不可恢复的执行错误 |
| telemetry 降级 | 只影响可观测性；不能改写官方分数或把通过判为失败 |

结构化 `details.benchmark` 至少包含 `valid`、`official_score`、`official_metrics`、`artifacts`，无效运行还包含 `error_type`、`error_message` 和可脱敏的 `error_details`。

## 4. 数据与协议演进

### 4.1 `benchmark_suites`

新表以项目为边界，保存 benchmark 的可复现定义：

- `benchmark_key`、`name`、`description`、`version`；
- `dataset_id`、`harness_kind`、`execution_kind`；
- `source_uri`、`source_commit`；
- `default_execution_config`、`required_capabilities`、`metadata`。

suite 是可浏览的 catalog 实体；创建 experiment 时把 key/version/config snapshot 固化到 experiment，以避免 suite 后续变化污染历史结果。

### 4.2 现有表的加性字段

`experiments` 增加 `benchmark_suite_id`、`benchmark_key`、`benchmark_version`、`benchmark_config`、`trials_per_case`。

`eval_jobs` 增加 `execution_kind`、`execution_config`、`trial_index`、`seed`，并以 `(experiment_id, dataset_case_id, trial_index)` 唯一约束保证重复调度不会产生重复 trial。

原来的 Experiment/Job/Runner 客户端字段必须持续可用；新字段均为可选或有兼容默认值。

### 4.3 Runner API

不另起 `Runner API v2`。在当前 claim response 中加性返回：

```json
{
  "job": {
    "execution_kind": "harbor",
    "execution_config": {"dataset": "terminal-bench@2.0"},
    "trial_index": 0,
    "seed": 42
  },
  "benchmark": {
    "key": "terminal-bench",
    "version": "2.0",
    "harness_kind": "harbor"
  }
}
```

现有 submit result 的 `details` 继续承载扩展的 benchmark verdict；主分数使用 `official_score`，主 passed 使用 `official_passed`。保留 `eval_scores` 用于官方的多维指标。

## 5. 可观测性与 Artifact

### 5.1 Correlation Context

Runner 为 Agent 子进程传入以下可选环境变量：

```text
AGENTSCOPE_EXPERIMENT_ID
AGENTSCOPE_EVAL_JOB_ID
AGENTSCOPE_EVAL_TRIAL_ID        # 当前版本等于 job id，保留兼容名称
AGENTSCOPE_BENCHMARK_KEY
AGENTSCOPE_BENCHMARK_TASK_ID
AGENTSCOPE_TRIAL_INDEX
```

缺失时 Agent integration 必须按普通 Run 工作。字段只能是标识符/索引，不得携带 prompt、工具参数、用户数据或密钥。

OpenHands 与 OpenClaw 都应把有效 context 写入：

- AgentScope Runtime Run metadata；
- agent root span 的 `agentscope.*` attributes；
- Runtime Snapshot metadata。

不要求 Harness、Agent 和 Runner 强行使用同一个 OTel trace；稳定的 job id/run id/trace id 关联优先于跨进程 trace 拼接。

### 5.2 Artifact

初始实现将每个 trial 的官方日志、结果 JSON、trajectory 与必要诊断文件写入 Runner 配置的本地 artifact root，并在 `details.benchmark.artifacts` 保存 kind、URI、大小、校验摘要（可用时）。

artifact 后端以 URI 抽象，后续可以迁移到 MinIO/S3，而不改变 Job、Result 和 UI 协议。禁止把密钥、原始环境快照或超出项目访问权限的文件暴露给浏览器。

## 6. 首批官方 Harness

### 6.1 Harbor（首个真实 E2E）

固定 Harbor 源码 commit 和配置；在 Docker 可用的 Runner 主机运行。官方 Harbor 已提供 `openhands`、`openhands-sdk` 与 `openclaw` Agent adapter，因此 AgentScope 优先调用它们，而不维护平行的 Agent 实现。

每个 AgentScope job 启动一个有明确 task filter、seed、输出目录和最大时限的 Harbor run。Driver 读取 Harbor 的 trial/job result，使用 verifier reward 作为 official score，并保存 Harbor 日志、官方 trial result 与 trajectory。

首次 E2E 采用低成本、小任务切片、单并发、单 trial；OpenHands 和 OpenClaw 在同一个切片上分别运行，以验证：环境启动、Agent 执行、官方 verifier、Run/trace 关联、数据库回写和 UI 渲染。

### 6.2 τ²-bench（第二个真实 E2E）

固定 τ²-bench commit 和 Python 版本。Driver 调用官方 `tau2 run` 或等价官方 Python entrypoint，必须保留官方 user simulator、domain environment 和 evaluator。首批使用单 domain、小 task 数、`num_trials=1`；从 `results.json` 解析每个 simulation 的 reward、reward components、task id 和 transcript artifact。

OpenHands/OpenClaw 的 τ² adapter 只负责把 τ² agent interface 转接到对应真实 Agent，绝不重写领域工具或奖励函数。

## 7. 产品界面

界面不应只是一个 JSON 管理页，而是面向评测操作者的任务流：

1. **Benchmark Catalog**：展示 suite、Harness、版本、要求能力、数据规模、可用状态和来源 commit。
2. **Create Experiment**：选择 suite、Agent、模型/target version、trials、seed、预算与 task slice；在提交前显示兼容性和不可运行原因。
3. **Experiment Overview**：总体通过率、official score、pass@k、运行状态、成本、延迟及与基线的变化。
4. **Trial Detail**：任务、trial index、官方 verdict、指标、错误分类、AgentScope Run/Trace、官方日志/trajectory/artifact。
5. **Compare**：按同 suite、task slice、seed policy 进行可解释比较，明确标记不可比配置。

所有页面需支持 loading、empty、permission denied 和 partial telemetry 状态。失败信息要区分 Agent 能力失败与基础设施失败，避免误导用户。

## 8. 分阶段交付

### Phase 0：协议与兼容基线

- [x] 冻结“单队列、Go 控制面、Python 执行面、官方 Harness”边界。
- [x] 明确 retry 与 independent trial 的语义。
- [x] 定义统一 error taxonomy、benchmark details 和 correlation context。

### Phase 1：控制面与 Runner Executor

- [x] 新增 suite/experiment/job 的加性 schema、sqlc 查询、服务/API 和测试。
- [x] 扩展 claim/submit 协议，保留旧 Runner 行为。
- [x] 建立 Executor Registry、FunctionExecutor、NativeBenchmark contract 和错误处理测试。
- [x] 完成 OpenHands/OpenClaw benchmark metadata 关联与测试。

### Phase 2：Harbor 真实运行

- [x] 增加 HarborExecutor、超时/清理、官方结果解析和 artifact manifest。
- [x] 增加 OpenHands/OpenClaw target bindings 与安全的环境变量传递。
- [x] 通过 Catalog 预检、详情页确认与服务端受监管 Runner 提供启动和结果验证闭环；不提供 Benchmark 执行脚本。
- [ ] 在有已轮换模型凭证的 Docker 主机实际运行两条 Agent E2E。

### Phase 3：τ² 真实运行

- [x] 增加 Tau2Executor、官方结果解析和 transcript artifact。
- [ ] 增加一个可复现的 domain/task slice 和 OpenHands/OpenClaw adapter contract。
- [ ] 运行最小真实 τ² E2E 并验证 reward 回写。

### Phase 4：可视化、可用性与运维

- [x] 完成 Catalog、创建向导、实验总览、trial detail、artifact/trace 跳转和比较入口。
- [x] 加入 harness/agent/credential/Docker compatibility preflight。
- [x] 支持超时、资源上限、脱敏与可行动的错误提示；取消与多机配额仍待后续实施。

### Phase 5：后续 benchmark 适配器

- [ ] L0：BFCL、自定义 tool contract、ToolSandbox（依赖维护状态可用时）。
- [ ] L2：BrowserGym/WebArena/WorkArena；VM/模拟器成熟后再接 OSWorld/AndroidWorld。
- [ ] L3：Inspect/AgentDojo/AgentHarm 和企业 prompt-injection/越权测试。
- [ ] L4：AppWorld、OlaBench、私有 Golden Set、线上采样与人工标注。

每个新适配器都必须有：固定版本、输入 capability/preflight、官方 verifier 映射、artifact 规则、错误 taxonomy 映射、最小 deterministic smoke test 和 UI 可视化。

## 9. 验收标准

### 自动化

- Go 单元/服务/API 测试通过，现有 function evaluation 回归通过；
- Python Executor、Runner Client、Harness result parser 的单测通过；
- OpenHands bridge 与 OpenClaw plugin 的关联 metadata 测试通过；
- Web 的创建、目录、结果页组件测试、lint 和 build 通过；
- 页面预检会在无凭证、无 Docker 或官方 Harness 不可用时给出可行动的阻断原因；Benchmark 执行不依赖部署或演示脚本。

### 真实 E2E

在一台有 Docker、官方 Harness 依赖和有效模型凭证的机器上，分别完成 OpenHands 与 OpenClaw 的最小 Harbor run，并确认：

1. 官方 Harness 创建/销毁环境；
2. official verifier 输出通过或未通过的正式 verdict；
3. AgentScope `eval_job` 完成并写入分数/metrics/artifacts；
4. AgentScope Run、Trace 和 Snapshot 带有相同 benchmark context；
5. UI 可浏览该 experiment 与每个 trial；
6. 失败任务不会被误标为基础设施成功，也不会污染 pass@k。

同样流程在 τ² 的一个小 domain slice 上重复，奖励来自官方 `results.json`。

## 10. 安全与资源边界

- Harbor/τ²/Agent 代码只在 Python Runner 主机运行，Go server 不执行用户代码；
- 项目 token 和模型密钥通过受控环境变量或 secret manager 注入，不写入数据库、trace、artifact 或前端；
- 每个 job 设置 timeout、并发、磁盘和 Docker cleanup 策略；
- 只允许经过 allowlist 的 Harness、dataset、Agent binding 和 artifact URI；
- 对外展示日志、trace 和 artifact 前执行项目鉴权与内容脱敏；
- 官方 Harness 版本、数据集 commit、Agent version、模型、seed 和 config 必须写入 experiment snapshot，保证结果可复现和可比较。

## 11. 不确定性与默认处理

当前不阻塞开发的默认选择：单机 Docker Runner、local filesystem artifact backend、Harbor 先行、每 case 默认 1 trial、凭证由运行环境提供。

只有以下情况需要产品/平台负责人进一步决策：

- 多租户并发下的 Docker/VM 隔离与配额；
- S3/MinIO 保留策略和 artifact 的审计需求；
- 对真实外部模型调用的单次/日成本上限；
- 是否把 OpenClaw 长寿命 Gateway 作为专用 benchmark worker，而非每 trial 短进程启动。
