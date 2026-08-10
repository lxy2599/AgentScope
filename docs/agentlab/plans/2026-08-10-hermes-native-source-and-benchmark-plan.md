# Hermes 原生源码接入 AgentScope 与 Harbor Benchmark 实施计划

> **Monorepo note (August 10, 2026):** this plan was written before the final monorepo merge. AgentScope now lives at this repository root; `hermes/`, `openhands/`, and `benchmarks/` are first-class directories. Historical path references in the plan are retained as implementation history.


- **状态**：已实施；本文保留实现、验收和收尾证据
- **日期**：2026-08-10
- **工作区**：`/home/luxinyu/GolandProjects/AgentLab`
- **目标读者**：AgentLab 开发者、Benchmark Worker/平台维护者
- **关联计划**：`docs/plans/2026-08-09-agent-lab-architecture-and-delivery-plan.md`

## 1. 决策摘要

本计划把 Hermes 按照 OpenHands 的最终目标接入 AgentLab，但**不复制 OpenHands 的内部实现**。

最终交付形态：

```text
AgentLab/
├── agentscope/       # 平台、Python SDK、控制面与 Web UI
├── openhands/        # 已完成的独立 OpenHands fork
├── hermes/           # 新增：独立 Hermes fork，源码内置 AgentScope SDK
├── benchmarks/       # Harbor、Worker、Task Runner Contract、catalog
├── infra/
└── scripts/
```

Hermes 的最终要求是：

1. Hermes 源码中直接导入并调用 `agentscope_sdk` Python SDK；
2. 不使用插件级集成；
3. 不把 `HermesAdapter`、observer plugin 或 `AgentScopeBridge` 作为最终运行时依赖；
4. Run、Turn、LLM、Tool、Answer、Failure 埋点必须位于 Hermes 的真实执行路径；
5. Interactive mode 与 benchmark mode 使用同一份 Hermes 源码和同一套观测代码；
6. Benchmark 通过与 OpenHands 相同的 Task Runner HTTP v1、Harbor Sandbox 和 Tool Gateway 执行；
7. 唯一的 benchmark 成功标准是 Harbor official verifier 的 `official_score: 1.0`。

> 当前 `agentscope/integrations/hermes-agent/` 中已有的 bridge、测试和 fork 代码只作为迁移参考。它们不是本计划的最终目录或最终抽象边界。


### 当前执行状态（2026-08-10）

| 阶段 | 状态 | 验收证据 |
|---|---|---|
| 阶段一：独立 fork 与依赖边界 | 完成 | `hermes/` 为独立 Git 仓库；SDK 由镜像构建期固定安装；Hermes runtime / Task Runner 本地镜像均已构建并以 digest 固定。 |
| 阶段二：源码直接埋点 | 完成 | Hermes 源码直接调用 `agentscope_sdk`；interactive mode 已验证 registration、heartbeat、LLM/Final Answer Trace；生产路径静态检查无 bridge/adapter 命中。 |
| 阶段三：Task Runner 与 Tool Gateway | 完成 | HTTP v1、native terminal interception 与 fail-closed Harbor Gateway 已由单元测试覆盖；2026-08-10 真实 Hermes Harbor smoke 经 official verifier 返回 `official_score: 1.0`。 |
| 阶段四：Catalog、平台 API、发布收尾 | 完成 | Hermes artifact/profile/template 已同步；catalog validate、Worker 测试、API E2E、服务健康检查、版本锁和运行说明均已更新。 |

这里的“完成”只表示该阶段全部验收证据已具备；代码存在、HTTP 返回成功或 Agent 给出回答均不能替代 Harbor official verifier。

### 最终验收记录（2026-08-10）

- Hermes native lifecycle、Task Runner、interactive service、Harbor Tool Gateway：`21 passed`；
- benchmarks catalog / Worker：`67 passed`，`benchmarkctl validate` 成功；
- API E2E：catalog sync、queue/claim/heartbeat、Hermes/OpenHands presence、cancellation audit 与 official-result fixture 均通过；
- 真实 Harbor official verifier：Hermes `official_score: 1.0`；
- 运行服务：AgentScope、Postgres、ClickHouse、OpenHands、Hermes、benchmarks-worker 健康运行。

Hermes 上游仓库包含大规模、可选组件（例如 ACP）的非交付测试矩阵；本计划的交付判据以本次改动覆盖的原生接入回归、benchmark Worker 全量测试和真实 Harbor official verifier 为准。

## 2. 当前状态审计

### 2.1 已有资产

当前 AgentScope 仓库中已经存在 Hermes 相关资产：

```text
agentscope/integrations/hermes-agent/
├── hermes_agentscope/bridge.py
├── hermes_agentscope/config.py
├── hermes_agentscope/session_registry.py
├── hermes_agentscope/noop.py
├── agent/agent_init.py
├── agent/conversation_loop.py
├── agent/tool_executor.py
└── tests/...
```

已有实现覆盖了以下概念：

- Hermes session/task 到 AgentScope session 的隔离；
- AgentScope RuntimeTraceSession；
- Run / Turn / LLM / Tool / Answer 生命周期；
- fail-open 观测错误处理；
- 并发 session 测试；
- native lifecycle 回归测试。

### 2.2 当前缺口

它还不能直接作为 AgentLab 的 Hermes 产品交付，主要缺口是：

- Hermes 仍位于 AgentScope 仓库内部，没有独立 `AgentLab/hermes` Git 边界；
- `bridge.py` 仍是 SDK 调用的主要封装入口，不符合“源码直接调用 SDK”的要求；
- Hermes 没有 AgentLab 标准 interactive service；
- 没有 Hermes digest-pinned benchmark artifact；
- 没有 Hermes Task Runner HTTP v1；
- 没有 Hermes Harbor Tool Gateway executor；
- 没有 Hermes catalog、agent profile、run template；
- 没有 Hermes 真实 Harbor official verifier E2E；
- 顶层启动和 `versions.lock` 尚未加入 Hermes。

## 3. 范围与非目标

### 3.1 本期范围

本期实现：

- Hermes 独立 fork；
- AgentScope Python SDK 源码级原生埋点；
- Hermes interactive mode；
- Hermes benchmark mode；
- Harbor Task Runner v1；
- Harbor Tool Gateway；
- Hermes catalog 和平台注册；
- API 创建、排队、Worker 执行、官方 verifier、报告和 Trace 关联；
- Hermes smoke、API E2E、核心测试和工作区整理。

### 3.2 明确不做

本期不做以下内容：

- Hermes plugin、外置 observer plugin 或独立 AgentScope Hermes 插件包；
- 仅依赖 `HermesAdapter` 的兼容接入作为最终实现；
- 在 benchmark 任务执行时动态 `pip install`、`git clone` 或下载 Hermes 源码；
- AgentScope 后端 fork 本机 Agent 子进程；
- Harbor 之外的正式 Harness；
- 从 Web 页面输入模型 AK、Project Token 或任何 Secret；
- 用 Agent final answer、HTTP 200 或 Worker exit code 代替官方 verifier；
- 恢复 OpenClaw 的默认启动、构建或验收。

## 4. 目标架构

### 4.1 Interactive mode

```text
Hermes process
  ├── agent/agent_init.py
  │     ├── import agentscope_sdk
  │     ├── configure_tracing(...)
  │     ├── init(...)
  │     └── register agent / start heartbeat
  ├── agent/conversation_loop.py
  │     ├── start_run
  │     ├── start_turn / finish_turn
  │     ├── start_llm / finish_llm
  │     ├── record_final_answer
  │     └── finish_run / fail_run
  └── agent/tool_executor.py
        ├── start_tool
        ├── finish_tool
        └── fail_tool
```

AgentScope SDK 初始化失败或单个 telemetry 操作失败时，Hermes 原有任务仍应继续执行；观测失败只能使当前 session 退化为 no-op，不能改变 Hermes 的模型、工具、调度和回答行为。

### 4.2 直接 SDK 调用边界

最终源码中允许存在 Hermes 自己的 session 状态管理，但不允许用独立 bridge 转发所有事件。

| Hermes 原生位置 | 直接调用的 SDK 能力 | 触发时机 |
|---|---|---|
| `agent/agent_init.py` | `configure_tracing`、`init` | Hermes 进程/Agent 初始化 |
| `agent/agent_init.py` | runtime registration / heartbeat | Agent 上线与退出 |
| `agent/conversation_loop.py` | `RuntimeTraceSession.start_run` | 用户输入或 benchmark task 开始 |
| `agent/conversation_loop.py` | `start_turn` / `finish_turn` | 每次真实 decision loop |
| Hermes LLM transport | `start_llm` / `finish_llm` 或 `record_llm` | 实际 provider 请求边界 |
| `agent/tool_executor.py` | `start_tool` / `finish_tool` | 实际工具 dispatch 边界 |
| `agent/conversation_loop.py` | `record_final_answer` / `finish_run` | 正常回答完成 |
| Hermes cancellation/error path | `fail_run` / `finish_turn` | 取消、异常、超时 |

实际使用的 SDK 方法名称以当前 `agentscope_sdk.RuntimeTraceSession` 公共 API 为准；禁止调用 SDK 私有模块或复制 SDK 内部 exporter 逻辑。

### 4.3 Benchmark mode

```text
benchmarks-worker
    ↓ claim eval_job
Harbor Harness
    ↓ create isolated sandbox
Hermes task-runner sidecar
    ↓ POST /v1/task-runs
Hermes native conversation loop
    ↓ native tool executor
Worker-owned Harbor Tool Gateway
    ↓
Harbor sandbox terminal/filesystem
    ↓
Harbor official verifier
    ↓
AgentScope report + Trace correlation
```

Hermes benchmark sidecar 不得访问：

- 宿主机工程目录；
- AgentLab 根目录；
- Worker 私有管理目录；
- 其他 trial workspace；
- Docker socket；
- 其他 Agent sidecar。

## 5. 阶段实施计划

### 阶段一：独立 Hermes fork 与依赖边界

#### 目标

将 Hermes 从 AgentScope 的集成目录迁移为独立项目，同时保留可审计的 source commit。

#### 工作项

1. 从当前受控 Hermes fork 创建 `AgentLab/hermes/`；
2. 固定 Hermes 上游基线 commit；
3. 把已有 native lifecycle 改动迁移到独立仓库；
4. 在 Hermes `pyproject.toml` 中声明 AgentScope SDK 依赖；
5. 开发期通过本地 workspace source 连接 `../agentscope/python`；
6. 生产/benchmark 镜像内安装固定版本 SDK，不在任务启动时安装；
7. 增加 Hermes 自己的 README、构建命令、运行时配置说明；
8. 增加 Hermes Dockerfile 和 task-runner Dockerfile；
9. 更新顶层 `versions.lock`。

#### 验收

```bash
cd hermes
python -m pytest -q
python -m compileall -q .
docker build --target runtime .
```

并且：

- `hermes/` 是独立 Git 仓库；
- 镜像能够离线启动；
- 镜像内不存在运行时安装路径；
- SDK 依赖版本可从 lock 和镜像 manifest 追溯。

### 阶段二：Hermes 源码直接埋点

#### 目标

删除最终运行时对 `AgentScopeBridge` 的依赖，把 SDK 调用放进 Hermes 原生生命周期。

#### 工作项

1. `agent_init.py`：直接初始化 SDK runtime、tracer 和 Agent registration；
2. `conversation_loop.py`：直接管理 session、Run、Turn、LLM 和 Answer；
3. LLM transport：记录真实 request/response、耗时、finish reason、usage 等安全 metadata；
4. `tool_executor.py`：记录真实工具开始、结束、失败、耗时和工具名；
5. error/cancel/timeout path：确保所有未完成 Run/Turn 被关闭；
6. 为并发 Hermes session 使用 Hermes 自己的 session key 进行隔离；
7. 清理或移除 `bridge.py`、`HermesAdapter` 作为最终运行时依赖；
8. 默认不采集原始 prompt、原始 provider response、AK、Project Token 或 cookie；
9. SDK/OTLP 不可达时 fail-open。

#### 代码审计门槛

以下命令只能在测试/迁移说明中出现，不能成为正式运行时入口：

```bash
rg 'AgentScopeBridge|HermesAdapter|definePluginEntry|plugins.load.paths' hermes
```

正式业务执行路径必须能证明：

```text
Hermes native source → agentscope_sdk public Python API
```

#### 验收

- 禁用观测时 Hermes 行为不变；
- SDK 初始化失败时 Hermes 仍可完成任务；
- LLM 失败、工具失败、取消、超时都能形成正确 Run 状态；
- 并发 session 不串 Run、Turn 或 Trace；
- 单元测试覆盖所有生命周期分支；
- AgentScope 中能看到 Hermes registration 与 heartbeat。

### 阶段三：Hermes Task Runner 与 Harbor Tool Gateway

#### 目标

让 Hermes 使用与 OpenHands 相同的跨框架 Task Runner v1，但内部执行仍完全由 Hermes 原生源码完成。

#### 工作项

1. 实现 Hermes Task Runner：
   ```http
   GET  /healthz
   POST /v1/task-runs
   GET  /v1/task-runs/{task_run_id}
   POST /v1/task-runs/{task_run_id}/cancel
   ```
2. POST 必须先返回 `202`，再异步启动 Hermes native run；
3. 实现 TaskRun 状态机：
   ```text
   queued → running → succeeded / failed / cancelled / timed_out
   ```
4. 在 Hermes 原生 tool executor 中加入 Harbor Tool Gateway executor；
5. 禁止本地 shell / 本地 workspace fallback；
6. 注入 per-trial gateway endpoint 和短期 capability token；
7. sidecar 镜像使用 digest pin；
8. Worker 负责 heartbeat、取消、sidecar 生命周期和 official verifier；
9. Task Runner 响应只返回 allowlisted 状态、错误码和安全 final output；
10. 不返回 prompt、命令、provider 原始响应和 Secret。

#### 验收

先通过本地 fake gateway contract tests，再通过真实 smoke：

```bash
PYTHONPATH=benchmarks/src python3 -m pytest -q
./benchmarks/scripts/run-real-agent-harbor-smoke.sh hermes
```

最终必须得到：

```json
{
  "framework": "hermes",
  "official_score": 1.0,
  "official_verifier": "harbor",
  "status": "passed"
}
```

### 阶段四：Catalog、平台 API、正式回归与发布收尾

#### 目标

将 Hermes 作为正式 Agent Artifact 纳入 Git YAML Catalog 和 AgentScope API。

#### 新增配置

```text
benchmarks/artifacts/hermes-dev-smoke.yaml
benchmarks/agent-profiles/hermes-default.yaml
benchmarks/run-templates/hermes-harbor-agent-smoke.yaml
benchmarks/run-templates/hermes-terminal-bench-smoke.yaml
```

Artifact 至少固定：

```yaml
schema_version: v1
kind: AgentArtifact
id: hermes-dev-smoke
framework: hermes
image: agentlab/hermes-task-runner:local
image_digest: sha256:...
source_commit: ...
task_runner_protocol: v1
```

#### 工作项

1. `benchmarkctl validate` 校验 Hermes artifact、profile、template 引用；
2. catalog-sync 将 Hermes 配置同步到 AgentScope；
3. preflight 校验 Hermes capability、image digest 和 Task Runner protocol；
4. API 创建 Hermes experiment 并保存 immutable execution snapshot；
5. Worker claim/heartbeat/submit/cancel 全链路验证；
6. 报告显示 official score、failure code、trace correlation 和 artifact；
7. 支持 OpenHands 与 Hermes 的同 Suite / 同 Model Profile 对比；
8. 增加 Hermes API E2E 和 cancellation audit。

#### 验收

```bash
PYTHONPATH=benchmarks/src python3 -m benchmarkctl validate benchmarks
PYTHONPATH=benchmarks/src python3 benchmarks/scripts/api_e2e.py \
  --server-url http://127.0.0.1:18080 \
  --catalog-root benchmarks
```

API E2E 必须验证：

- Hermes presence；
- catalog revision；
- immutable snapshot；
- task selection metadata；
- queue → claim → heartbeat → submit；
- cancellation audit；
- official verifier result；
- OpenHands/Hermes 对比字段。

#### 收尾与发布（阶段四的一部分）

1. 各仓库独立提交并保持工作区干净；
2. 更新 `versions.lock`、artifact digest 和 source commit；
3. 更新顶层 `README.md`、Makefile、`agentlab.sh`；
4. 默认启动 AgentScope、OpenHands、Hermes、benchmarks-worker；
5. OpenClaw 继续保持暂停状态；
6. 运行全套测试、真实 smoke、API E2E；
7. 做 requirement-by-requirement completion audit；
8. 只在 Harbor official verifier 达到 1.0 后标记 Hermes benchmark 完成。

## 6. 测试矩阵

### Hermes 单元测试

- SDK settings / startup / shutdown；
- direct SDK session lifecycle；
- Run / Turn / LLM / Tool / Answer；
- LLM failure；
- tool failure；
- cancellation / timeout；
- fail-open；
- session concurrency；
- no prompt/token leakage。

### Hermes Task Runner 测试

- healthz contract；
- `POST` 立即返回 `202`；
- queued/running/terminal states；
- duplicate task id；
- invalid workspace；
- invalid gateway endpoint；
- missing capability；
- cancellation；
- timeout；
- gateway non-zero exit code；
- gateway transport failure；
- no local fallback；
- safe error projection。

### 集成测试

- Hermes interactive service registration；
- heartbeat；
- real model request；
- real tool call；
- OTLP trace；
- concurrent session isolation；
- Worker sidecar lifecycle；
- Harbor tool gateway；
- official verifier artifact。

### 最终 E2E

```bash
./scripts/agentlab.sh agents-up
./scripts/agentlab.sh status

cd hermes
python -m pytest -q

cd ../benchmarks
PYTHONPATH=src python3 -m benchmarkctl validate .
./scripts/run-real-agent-harbor-smoke.sh hermes

cd ..
PYTHONPATH=benchmarks/src python3 benchmarks/scripts/api_e2e.py \
  --server-url http://127.0.0.1:18080 \
  --catalog-root benchmarks
```

## 7. 风险与决策

### 7.1 Hermes 当前没有与 OpenHands 完全相同的 Task Runner 入口

解决方案：在 Hermes fork 中添加内部 CLI/HTTP Task Runner；它只是启动 Hermes 原生 loop，不改变 Hermes 的核心行为。

### 7.2 Hermes 工具参数和 OpenHands 不同

解决方案：只统一外部 Task Runner / Tool Gateway Contract，不统一 Agent 内部 tool schema。Hermes 保留自己的 tool schema，并在 executor 边界映射到 gateway `command` 与 `timeout`。

### 7.3 SDK 观测不能影响 Agent

解决方案：SDK 初始化、单个 span、flush 和 shutdown 都采用 fail-open；错误只进入受控诊断计数，不进入 Hermes 任务返回值。

### 7.4 provider 不支持 tool call

解决方案：在 real smoke 前增加 provider capability probe；但 probe 只能说明工具协议可用，不能替代 Harbor official verifier。

### 7.5 Hermes 上游升级导致调用图变化

解决方案：每次 source commit 变更都必须重新运行 direct-source audit、native lifecycle tests、Task Runner tests 和真实 Harbor smoke。

## 8. 完成定义

Hermes 只有同时满足以下条件才算完成：

- `AgentLab/hermes/` 是独立源码仓库；
- Hermes 源码直接导入并调用 `agentscope_sdk`；
- 正式运行时不依赖 bridge、plugin 或 HermesAdapter；
- interactive registration 和 heartbeat 正常；
- native Run/Turn/LLM/Tool/Answer/Failure Trace 正常；
- Hermes Task Runner v1 contract 完整；
- Harbor Tool Gateway 无本地 fallback；
- artifact 使用 digest pin；
- catalog validate 和 catalog-sync 通过；
- API E2E 通过；
- Hermes 真实 Harbor official verifier `official_score: 1.0`；
- 四个子仓库和 Hermes 新仓库工作区整理完成；
- Secret、prompt、provider 原始响应不进入仓库、catalog、artifact、日志或 API 响应。

在达到以上条件前，Hermes 状态只能标记为 `planned`、`in_progress` 或 `failed`，不能标记为 `completed`。
