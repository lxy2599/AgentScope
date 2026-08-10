# Benchmark Contract v1

`benchmarks/` 是 AgentLab 中独立版本化的 catalog、Harbor Worker 与官方评分边界。它不在任务执行时下载 Agent 源码或安装 OpenHands/Hermes；每个 Agent sidecar 必须从 Catalog 解析出的不可变镜像 digest 启动。

## Catalog

支持六类 YAML：`BenchmarkSuite`、`AgentArtifact`、`ModelProfile`、`AgentProfile`、`ExecutionProfile`、`BenchmarkRunTemplate`。所有文件必须使用 `schema_version: v1`，并在对应目录中保存。未知字段、缺失引用、能力不匹配、非 `sha256` image digest 和敏感字段都会被拒绝。

有效执行配置：

```text
Suite.defaults < ExecutionProfile.execution < RunTemplate.execution_overrides
```

Artifact、model profile 与 agent profile 不可通过隐藏环境变量覆盖。凭据只经 Worker 的运行时环境或 Docker Secret 注入，绝不进入 YAML、manifest、CLI 参数、trace、日志或 Harbor result artifact。

`ModelProfile.model` 是**目标 Agent runtime 接收的字面模型标识**，不是跨框架统一格式：例如 OpenHands 可以使用 provider-prefixed 标识，而 Hermes 连接显式 Ark OpenAI-compatible endpoint 时使用 Ark bare deployment id。Worker 必须从不可变 execution snapshot 写入 sidecar，不能回退到宿主 `LLM_MODEL` 默认值。

### Harbor harness 配置

`ExecutionProfile.execution` 必须为 Harbor suite 包含一个版本化 `harbor` 映射。该不可变映射会进入 manifest、catalog-sync payload、AgentScope experiment 和 eval job execution snapshot。

```yaml
execution:
  harbor:
    version: "0.20.0"
    dataset: terminal-bench@2.0
    agent_import_path: benchmark_worker.harbor_agent:TaskRunnerAgent
    jobs_root: harbor-jobs
    # 可选：稳定、非秘密的命名/配置路径。
    job_name: terminal-bench-smoke
    config_path: configs/terminal-bench.yaml
```

- `version` 是无 `v` 前缀的 SemVer，声明**已构建进 Worker image** 的 Harbor harness 身份；不是运行时安装指令。
- `dataset` 是受限字符集的 dataset reference；`agent_import_path` 必须是 Python `module:attribute`。
- `jobs_root` 与可选 `config_path` 都必须是无 `.` / `..` 段的安全相对 POSIX 路径；可选 `job_name` 必须是 catalog identifier。
- 该 block 不允许未知字段、任意 command、environment map 或 free-form CLI args。
- 所有 catalog 层级递归拒绝 `token`、`api_key`、`password`、`secret`、`credential`、`authorization` 等 secret-like key。

`harbor` 只能位于 `ExecutionProfile.execution`，不能在 `Suite.defaults` 或 `RunTemplate.execution_overrides` 中覆盖。`harness: harbor` 的 suite 缺少该 block 时，catalog 编译失败。

## Task Runner HTTP v1

OpenHands / Hermes 的 owned image 必须暴露同一私有协议：

```text
GET  /healthz
POST /v1/task-runs
GET  /v1/task-runs/{taskRunID}
POST /v1/task-runs/{taskRunID}/cancel
```

公共状态严格为：

```text
queued | running | succeeded | failed | cancelled | timed_out
```

Task request 只含任务、workspace、tool gateway endpoint、profile ID 与 experiment/job correlation；模型密钥、AgentScope credential 与 Tool Gateway capability 均不属于 HTTP body。每个 Task Runner 的 `/healthz` 必须声明 `tool_gateway_protocols` 并包含 `agentlab.harbor-tool-gateway.v1`；Worker 否则拒绝该 sidecar。`TaskRunnerHTTPAdapter` 不回显 sidecar error body。

## Worker 与 Harbor 生命周期

`benchmark-worker` 是常驻、queue-driven 的真实执行器：

```text
register worker
→ claim durable Harbor eval_job
→ independent heartbeat loop
→ 创建单题私有工作目录
→ 用 image@sha256 启动一个 Docker Task Runner sidecar
→ healthcheck
→ 调 Harbor 官方 CLI / custom BaseAgent
→ 读取 Harbor official verifier reward
→ 只提交 passed / failed verifier result
→ 生成脱敏 verifier artifact
→ 停止 sidecar，清理单题 workspace
```

Worker API：

```text
POST /runner/benchmark-workers/register
GET  /runner/jobs/claim?runner_name=<id>&execution_kind=harbor
POST /runner/jobs/{jobID}/heartbeat
POST /runner/jobs/{jobID}/results
POST /runner/jobs/{jobID}/fail
```

`AGENTSCOPE_PROJECT_TOKEN` 只以 `Authorization: Bearer` 的运行时 header 发给平台；不会写入 Catalog 或 Harbor CLI 参数。Worker 对任务 lease 独立 heartbeat，Harbor 等待期间不依赖 sidecar 轮询来续租。

### 成功、失败与证据

**唯一允许写入 `/results` 的成功/失败判据是 Harbor 官方 verifier reward。** Agent final text、sidecar HTTP 200、Task Runner `succeeded`、Harbor process exit code 都不能单独判定 benchmark pass。

- `reward >= 1.0`：`verifier_status=passed`；否则 `verifier_status=failed`。
- Harbor 运行失败而没有官方 result：`harness_error`；Harness timeout：`timeout`。
- sidecar 不健康、不可访问或无 task state：`agent_error`。
- 缺少/损坏官方 verifier evidence：`verifier_error`。
- 结果 artifact 是 Worker 写出的最小脱敏 `agentlab.harbor-official-verdict.v1` JSON；它不会复制 Harbor 原始 `result.json`、task output 或日志。

Docker sidecar 约束：只挂载当前单题 workspace、只发布 loopback 随机端口、使用 `--rm --init`、无 shell、无 Docker socket、无运行时 `pip/npm` 安装，并只接受 `image@sha256:<64 hex>`。

### Sandbox / Tool Gateway 边界

Harbor 的 Docker sandbox 与 Agent sidecar 不是天然共享文件系统。每个 `TaskRunnerAgent.run()` 在 Harbor 已启动的真实 `BaseEnvironment` 上创建一次性 `agentlab.harbor-tool-gateway.v1` session，Gateway 的 terminal 请求**只**调用该 `BaseEnvironment.exec()`；它不持有 Worker host shell、Docker socket、Harbor verifier API 或 compose service selector。Harbor agent phase 返回时 Gateway 关闭，因此 verifier phase 不能继续使用旧 capability。

```text
immutable OpenHands/Hermes sidecar
  → authenticated POST /v1/terminal/exec
  → per-trial Tool Gateway
  → Harbor BaseEnvironment.exec (main task service)
  → Harbor official verifier
```

- Gateway URL 仅使用每题临时端口；capability 是每题随机值，只经 Harbor child-process 与 sidecar 运行时环境传递，绝不进入 YAML、Task Runner body、CLI 参数、trace、artifact 或日志。
- Compose Worker 下 sidecar 加入 `agentlab_default` control network，通过 `benchmarks-worker:<ephemeral-port>` 访问 Gateway；Gateway 端口不发布到宿主机。Harbor task sandbox 不加入该 control network。
- Task Runner 缺少 protocol declaration、endpoint、capability 或远程工具调用失败时必须 `fail-closed`，不得退回其容器 `/workspace`、Worker job directory 或宿主文件系统。
- 当前 `/workspace` bind mount 只是 sidecar 进程启动所需的单题 inert 目录；它不是 Terminal-Bench 的 task state，也不能成为判分依据。

Harbor verifier reward 仍是唯一 passed/failed/score 权威；Gateway HTTP 成功和 Agent final text 都不是分数。

## AgentScope Template Snapshot

`benchmarkctl catalog-sync` 将 artifact capability 和 run template execution overrides 同步到 AgentScope。创建 experiment 时推荐：

```json
{"run_template_id":"openhands-terminal-bench-smoke"}
```

AgentScope 解析 project-scoped catalog、选择数据库 case、应用执行优先级，并把完整不可变 `agentlab.benchmark-execution.v1` 写入 experiment 与每个 `eval_job`：

```text
Catalog revision + Run Template + Artifact digest + profiles
+ suite + selected case IDs + task selection + effective execution
```

Worker 必须只使用 claim 返回的 snapshot，不能重新读取可变 YAML。Catalog task selection ID 对应 `dataset_case.metadata.task_id`；历史 case 没有该值时可退回数据库 case ID。
