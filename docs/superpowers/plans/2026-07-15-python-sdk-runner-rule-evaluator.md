# Python SDK Runner 和规则评测器实现计划

> **给执行 agent 的要求：** 执行本计划时必须使用 `using-git-worktrees`、`executing-plans`、`test-driven-development`、`systematic-debugging`、`verification-before-completion`。本阶段可以一次性完成，但必须按任务分 commit。步骤使用 checkbox (`- [ ]`) 语法跟踪。

**目标：** 做出 Python 侧最小可用闭环：用户能注册 target，runner 能领取后端 job、执行 target、运行确定性规则 evaluator，并把结果或失败回传服务端。

**架构：** Go 服务端继续只负责任务编排和结果存储，不执行用户代码。Python SDK 在用户环境内提供 target registry、规则 evaluator、runner client 和 CLI。runner 通过 project token 调用 `/runner/...` API，target 执行失败和 evaluator 配置错误都要被明确回传。

**技术栈：** Python 3.11+、Pydantic、httpx、Typer、pytest、pytest-httpx、OpenTelemetry Python SDK。后端仍是 Go、PostgreSQL、ClickHouse。

## 全局约束

- 所有实现必须在隔离 git worktree 中完成，禁止直接在 `master` / `main` 上开发。
- 开始前先确认 Task 6 最终修复已进入当前基线；最终修复 commit 为 `a4d672d fix: 修复 Runner 结果提交项目边界`。
- 如果 `master` 仍停在 `5d830eb`，不要从 `master` 开始开发；先让维护者合并 Task 6，或者从 `task6-dataset-experiment-runner` 创建本阶段 worktree。
- 本阶段可以一次性执行完成，但必须分 4 个 commit。
- 每个任务先写失败测试，再实现，再运行验证命令，再提交。
- 提交信息必须使用中文，可以保留 `feat:`、`fix:`、`test:`、`chore:` 前缀。
- Python runner 不依赖 Go 内部包，只通过 HTTP API 和后端通信。
- Go 服务端不执行用户 agent 代码。
- project token 只能从环境变量或 CLI 参数读取，不能写入日志、测试快照或错误消息。
- evaluator 必须返回统一结构：`score`、`passed`、`reason`、`details`。
- target 抛错要调用 fail API；evaluator 配置错误也要被记录为失败，不要静默跳过。
- 第一版不做 LLM judge，不做并发 worker 池，不做复杂重试策略，不做前端页面。

---

## 当前基线检查

执行 agent 开始前运行：

```bash
cd /home/luxinyu/GolandProjects/AgentScope
git branch --show-current
git status --short
git worktree list
git log --oneline --decorate --all -n 12
```

预期：

- 当前工作区干净。
- 能看到 `task6-dataset-experiment-runner` 分支包含 `a4d672d`。
- 新阶段 worktree 不要基于旧的 `5d830eb` 继续开发。

建议 worktree：

```bash
git worktree add .claude/worktrees/task7-python-sdk-runner -b task7-python-sdk-runner task6-dataset-experiment-runner
cd .claude/worktrees/task7-python-sdk-runner
```

如果 Task 6 已合并到 `master`，也可以改为：

```bash
git worktree add .claude/worktrees/task7-python-sdk-runner -b task7-python-sdk-runner master
cd .claude/worktrees/task7-python-sdk-runner
```

---

## 文件结构

- 新建：`python/pyproject.toml`  
  Python package 元数据、依赖、pytest 配置、console script。

- 新建：`python/agentscope_sdk/__init__.py`  
  对外导出 `target`、`get_target`、`EvaluationResult`、规则 evaluator 入口。

- 新建：`python/agentscope_sdk/target.py`  
  target decorator、registry、target lookup。

- 新建：`python/agentscope_sdk/evaluators.py`  
  规则 evaluator 数据模型和执行逻辑。

- 新建：`python/agentscope_sdk/client.py`  
  runner HTTP client，封装 claim、heartbeat、submit result、fail。

- 新建：`python/agentscope_sdk/runner.py`  
  单 job 执行编排：claim job、调用 target、执行 evaluator、提交结果。

- 新建：`python/agentscope_sdk/cli.py`  
  `agentscope-runner start` CLI。

- 新建：`python/tests/test_target.py`  
  target 注册和查找测试。

- 新建：`python/tests/test_evaluators.py`  
  exact、contains、not_contains、regex、numeric threshold、latency threshold 测试。

- 新建：`python/tests/test_client.py`  
  HTTP 协议路径、鉴权 header、错误处理测试。

- 新建：`python/tests/test_runner.py`  
  runner 执行成功、target 异常、evaluator 失败回传测试。

- 修改：`.gitignore`  
  忽略 Python venv、pytest cache、build artifact。

- 必要时修改：`Makefile`  
  增加 `test-python`，不要破坏已有 Go 命令。

---

## Task 7A：Python package 和 target registry

**目标：** 建立 Python SDK 包，支持 `@agentscope_sdk.target(name, version)` 注册和按 name 查找。

**文件：**

- Create: `python/pyproject.toml`
- Create: `python/agentscope_sdk/__init__.py`
- Create: `python/agentscope_sdk/target.py`
- Create: `python/tests/test_target.py`
- Modify: `.gitignore`

**接口：**

- Produces: `target(name: str, version: str) -> Callable`
- Produces: `get_target(name: str) -> RegisteredTarget`
- Produces: `RegisteredTarget.name: str`
- Produces: `RegisteredTarget.version: str`
- Produces: `RegisteredTarget.func: Callable[[Any, dict[str, Any]], Any]`

- [ ] **Step 1: 写失败测试**

`python/tests/test_target.py` 覆盖：

- decorator 注册 target。
- `get_target("support-agent")` 返回 name、version 和原函数。
- 重复注册同名 target 抛 `TargetRegistrationError`。
- 查找不存在 target 抛 `TargetNotFoundError`。

- [ ] **Step 2: 运行测试确认失败**

```bash
cd python
python -m venv .venv
. .venv/bin/activate
python -m pip install -e ".[dev]"
pytest tests/test_target.py -q
```

预期：测试因 package 或接口不存在而失败。

- [ ] **Step 3: 实现最小 package 和 registry**

实现 `RegisteredTarget` dataclass、全局 registry、`target` decorator、`get_target`。

- [ ] **Step 4: 验证**

```bash
cd python
. .venv/bin/activate
pytest tests/test_target.py -q
```

预期：PASS。

- [ ] **Step 5: 提交**

```bash
git add .gitignore python/pyproject.toml python/agentscope_sdk/__init__.py python/agentscope_sdk/target.py python/tests/test_target.py
git commit -m "feat: 添加 Python SDK 目标注册"
```

---

## Task 7B：规则 evaluator

**目标：** 实现确定性规则 evaluator，覆盖第一版需要的 exact、contains、not_contains、regex、numeric threshold、latency threshold。

**文件：**

- Create: `python/agentscope_sdk/evaluators.py`
- Create: `python/tests/test_evaluators.py`
- Modify: `python/agentscope_sdk/__init__.py`

**接口：**

- Consumes: Python package from Task 7A
- Produces: `EvaluationResult(score: float, passed: bool, reason: str, details: dict[str, Any])`
- Produces: `evaluate_rule(config: dict[str, Any], actual: Any, expected: Any | None = None, metrics: dict[str, Any] | None = None) -> EvaluationResult`
- Produces: `EvaluatorConfigError`

- [ ] **Step 1: 写失败测试**

覆盖：

- `{"type":"exact"}`：actual 等于 expected 时 `passed=True, score=1.0`。
- `{"type":"contains","value":"hello"}`：actual 包含 value 时通过。
- `{"type":"not_contains","value":"error"}`：actual 不包含 value 时通过。
- `{"type":"regex","pattern":"order-[0-9]+"}`：actual 匹配 pattern 时通过。
- `{"type":"numeric_threshold","field":"score","op":">=","value":0.8}`：actual dict 中字段满足阈值时通过。
- `{"type":"latency_threshold","max_ms":1000}`：metrics latency 小于等于阈值时通过。
- 未知 type 抛 `EvaluatorConfigError`。

- [ ] **Step 2: 运行测试确认失败**

```bash
cd python
. .venv/bin/activate
pytest tests/test_evaluators.py -q
```

预期：失败。

- [ ] **Step 3: 实现 evaluator**

约束：

- 所有 evaluator 都返回 `EvaluationResult`。
- `score` 必须在 `0.0` 到 `1.0` 之间。
- 配置错误抛 `EvaluatorConfigError`。
- 普通不通过不是异常，返回 `passed=False`。
- `reason` 要能被 eval result 页面直接展示。

- [ ] **Step 4: 验证**

```bash
cd python
. .venv/bin/activate
pytest tests/test_evaluators.py -q
pytest -q
```

预期：PASS。

- [ ] **Step 5: 提交**

```bash
git add python/agentscope_sdk/__init__.py python/agentscope_sdk/evaluators.py python/tests/test_evaluators.py
git commit -m "feat: 添加 Python 规则评测器"
```

---

## Task 7C：Runner HTTP client

**目标：** 封装 runner 协议，保证鉴权 header、请求路径和响应模型稳定。

**文件：**

- Create: `python/agentscope_sdk/client.py`
- Create: `python/tests/test_client.py`

**接口：**

- Produces: `RunnerClient(server_url: str, project_token: str, runner_name: str, timeout: float = 30.0)`
- Produces: `RunnerClient.claim_job() -> ClaimedJob | None`
- Produces: `RunnerClient.heartbeat(job_id: str) -> None`
- Produces: `RunnerClient.submit_result(job_id: str, result: JobResultPayload) -> None`
- Produces: `RunnerClient.fail_job(job_id: str, error_type: str, message: str, details: dict[str, Any] | None = None) -> None`

- [ ] **Step 1: 写失败测试**

使用 `pytest-httpx` 覆盖：

- `claim_job` 请求 `GET /runner/jobs/claim`。
- 请求包含 `Authorization: Bearer <project_token>`。
- claim 返回 204 时返回 `None`。
- claim 返回 job JSON 时解析 case input、expected、metadata、target name、agent version、evaluator config。
- `submit_result` 请求 `POST /runner/jobs/{jobID}/results`。
- `fail_job` 请求 `POST /runner/jobs/{jobID}/fail`。
- server_url 末尾是否带 `/` 都不能生成双斜杠路径。

- [ ] **Step 2: 运行测试确认失败**

```bash
cd python
. .venv/bin/activate
pytest tests/test_client.py -q
```

预期：失败。

- [ ] **Step 3: 实现 client**

约束：

- 不打印 project token。
- 4xx/5xx 抛 `RunnerAPIError`，错误消息不能包含 token。
- 用 Pydantic model 解析服务端响应。
- `runner_name` 作为 query 或 header 传递时要和后端当前协议一致；先读 `internal/httpapi/runner_handler.go` 和 `internal/runnerapi/service.go` 后再定。

- [ ] **Step 4: 验证**

```bash
cd python
. .venv/bin/activate
pytest tests/test_client.py -q
pytest -q
```

预期：PASS。

- [ ] **Step 5: 提交**

```bash
git add python/agentscope_sdk/client.py python/tests/test_client.py
git commit -m "feat: 添加 Runner HTTP 客户端"
```

---

## Task 7D：Runner 执行编排和 CLI

**目标：** runner 能领取一个 job，找到 target，执行 target，运行 evaluator，提交 result；target 或 evaluator 异常时调用 fail。

**文件：**

- Create: `python/agentscope_sdk/runner.py`
- Create: `python/agentscope_sdk/cli.py`
- Create: `python/tests/test_runner.py`
- Modify: `python/pyproject.toml`
- Modify: `python/agentscope_sdk/__init__.py`
- Modify: `Makefile`

**接口：**

- Consumes: `get_target` from Task 7A
- Consumes: `evaluate_rule` and `EvaluationResult` from Task 7B
- Consumes: `RunnerClient` from Task 7C
- Produces: `run_once(client: RunnerClient) -> bool`
- Produces: CLI `agentscope-runner start --project-token ... --server-url ... --runner-name ... --once`

- [ ] **Step 1: 写失败测试**

覆盖：

- claim 返回 `None` 时 `run_once` 返回 `False`，不调用 submit/fail。
- target 正常返回时调用 submit_result，payload 包含 actual output、score、passed、reason、details。
- target 抛异常时调用 fail_job，error_type 为 `target_error`。
- target 不存在时调用 fail_job，error_type 为 `target_not_found`。
- evaluator 配置错误时调用 fail_job，error_type 为 `evaluator_config_error`。

- [ ] **Step 2: 运行测试确认失败**

```bash
cd python
. .venv/bin/activate
pytest tests/test_runner.py -q
```

预期：失败。

- [ ] **Step 3: 实现 runner**

约束：

- target 调用签名为 `func(case_input, context)`。
- context 至少包含 `job_id`、`case_id`、`metadata`、`agent_version`。
- 暂时不实现无限并发；`--once` 跑一个 job，默认循环 polling。
- 默认 polling interval 为 2 秒，可通过 CLI 参数覆盖。
- evaluator 只用规则 evaluator，不接 LLM judge。

- [ ] **Step 4: 实现 CLI**

CLI 参数：

```bash
agentscope-runner start \
  --project-token "$AGENTSCOPE_PROJECT_TOKEN" \
  --server-url "http://localhost:8080" \
  --runner-name "local-runner" \
  --once
```

环境变量 fallback：

- `AGENTSCOPE_PROJECT_TOKEN`
- `AGENTSCOPE_SERVER_URL`
- `AGENTSCOPE_RUNNER_NAME`

- [ ] **Step 5: 验证**

```bash
cd python
. .venv/bin/activate
pytest -q
agentscope-runner --help
agentscope-runner start --help
cd ..
go test ./... -count=1
go vet ./...
```

预期：全部通过。

- [ ] **Step 6: 提交**

```bash
git add Makefile python/pyproject.toml python/agentscope_sdk/__init__.py python/agentscope_sdk/runner.py python/agentscope_sdk/cli.py python/tests/test_runner.py
git commit -m "feat: 添加 Runner 执行编排和 CLI"
```

---

## 最终验证

执行完四个 commit 后运行：

```bash
git status --short
git log --oneline -n 6
cd python
. .venv/bin/activate
pytest -q
agentscope-runner --help
agentscope-runner start --help
cd ..
go test ./... -count=1
go vet ./...
```

如果本地有 PostgreSQL / ClickHouse：

```bash
make db-up
go test ./... -count=1
cd python && . .venv/bin/activate && pytest -q
```

最终报告必须包含：

- worktree 路径
- 分支名
- 每个 commit hash 和中文 commit message
- Python 测试结果
- Go 测试结果
- 是否基于 `a4d672d` 或已合并 Task 6 的 master
- 如果没有跑数据库集成测试，明确说明原因

---

## 审核重点

- runner 是否严格通过 HTTP 协议和服务端交互，没有依赖 Go 内部实现。
- project token 是否只进入 Authorization header，没有出现在日志、异常文本、快照或测试输出。
- target 异常、target 不存在、evaluator 配置错误是否都会回传 fail，而不是让 runner 崩溃退出。
- evaluator 失败和 evaluator 配置错误是否区分清楚：前者是正常评测不通过，后者是执行错误。
- client 路径是否和后端当前 runner API 完全一致。
- 测试是否覆盖成功路径和失败路径，尤其是 result/fail 的 payload。

---

## 一次性交给执行 agent 的提示词

你将在 `/home/luxinyu/GolandProjects/AgentScope` 仓库中实现 Task 7：Python SDK、runner 和规则 evaluator。

必须使用这些 skill：

- `using-git-worktrees`
- `executing-plans`
- `test-driven-development`
- `systematic-debugging`
- `verification-before-completion`

执行要求：

1. 先阅读 `CLAUDE.md`、`docs/superpowers/specs/2026-07-13-agent-observability-eval-design.md`、`docs/superpowers/plans/2026-07-15-python-sdk-runner-rule-evaluator.md`。
2. 先运行：

```bash
git branch --show-current
git status --short
git worktree list
git log --oneline --decorate --all -n 12
```

3. 确认 Task 6 最终修复 `a4d672d fix: 修复 Runner 结果提交项目边界` 在基线中。如果 `master` 仍停在 `5d830eb`，不要从 `master` 开新分支；从 `task6-dataset-experiment-runner` 创建：

```bash
git worktree add .claude/worktrees/task7-python-sdk-runner -b task7-python-sdk-runner task6-dataset-experiment-runner
cd .claude/worktrees/task7-python-sdk-runner
```

4. 按计划一次性完成 Task 7A、7B、7C、7D，但必须分 4 个 commit：

```bash
feat: 添加 Python SDK 目标注册
feat: 添加 Python 规则评测器
feat: 添加 Runner HTTP 客户端
feat: 添加 Runner 执行编排和 CLI
```

5. 每个任务必须先写失败测试，再实现，再运行对应测试，再提交。
6. runner client 必须读取当前后端代码中的实际 runner API 路由和请求格式，尤其是 runner_name 的传递方式。
7. 不要实现 LLM judge，不要做 React UI，不要修改 Go 后端业务逻辑，除非只是为了测试命令或 Makefile 补充。
8. 最终运行：

```bash
cd python
. .venv/bin/activate
pytest -q
agentscope-runner --help
agentscope-runner start --help
cd ..
go test ./... -count=1
go vet ./...
git status --short
```

9. 最终报告用中文，包含 worktree 路径、分支名、commit hash、测试结果、未验证项。
