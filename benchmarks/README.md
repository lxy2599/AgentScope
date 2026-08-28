# AgentLab benchmarks

独立的 benchmark catalog、`benchmarkctl` 和 Harbor Worker。它消费 AgentScope 的 durable `eval_jobs`，以不可变 OpenHands/Hermes image digest 启动单题 Task Runner sidecar，并只接受 Harbor 官方 verifier 作为最终评分依据。

## 本地校验

```bash
cd /home/luxinyu/GolandProjects/AgentLab/benchmarks
PYTHONPATH=src python3 -m benchmarkctl validate .
PYTHONPATH=src python3 -m pytest -q

# 平台已通过 ./scripts/agentlab.sh platform-up 启动后：
PYTHONPATH=src python3 scripts/api_e2e.py --server-url http://127.0.0.1:18080
```

`api_e2e.py` 创建随机隔离项目，走真实 HTTP 的 catalog sync、Template preflight/create/launch、runner claim/heartbeat/report 链路，并提交一个明确标注为 fixture 的官方 verifier 结果。它同时验证 report 只返回白名单化的不可变 Catalog 快照（revision、template、artifact digest、profile identity、task selection），不会回传原始执行配置；脚本不会输出 Cookie、Token 或临时密码。

## 真实 Agent Harbor smoke

`harbor-sentinel` 证明离线可信执行链；真实模型 Agent 则使用独立、非常小的本地 Harbor fixture：

```bash
# AgentLab/.env 中必须已经有 ARK_API_KEY 或 LLM_API_KEY。
./scripts/run-real-agent-harbor-smoke.sh openhands
./scripts/run-real-agent-harbor-smoke.sh hermes
```

该命令通过 HTTP API 创建隔离项目、case、suite、catalog revision、experiment 和临时 runner token，再以一次性 Worker 启动对应的 `image@sha256` sidecar。结果只取 AgentScope report 中的 Harbor official verifier score；脚本不会回显 token、模型 AK、Prompt 或 Harbor 原始日志。

正常 Compose Worker 会把 catalog 以**相同的绝对宿主路径**只读挂载到 trusted Worker，以便 Harbor 通过 Docker socket 启动 task environment 时可读取本地 fixture；sidecar 不接收 Docker socket 或 catalog mount。

## Catalog 与实验

```bash
# 管理端 session cookie 只从运行时环境读取。
export AGENTSCOPE_SESSION_COOKIE='...'
benchmarkctl catalog-sync . --server-url http://127.0.0.1:18080 --project-id <project-id>

# 在 AgentScope UI 或 API 中以 run_template_id 创建并 launch experiment。
```

## Worker

Worker token、模型 AK 和代理都只来自运行时 `.env` / Docker Secret：

```bash
export AGENTSCOPE_PROJECT_TOKEN='...'
export AGENTSCOPE_SERVER_URL=http://127.0.0.1:18080
export HTTP_PROXY=http://127.0.0.1:7897
export HTTPS_PROXY=http://127.0.0.1:7897

benchmark-worker \
  --server-url "$AGENTSCOPE_SERVER_URL" \
  --worker-id harbor-local \
  --work-root .agentlab/benchmark-worker
```

`benchmark-worker --once` 便于单 job 调试；`--keep-workspaces` 会保留 Harbor job 目录。常驻模式会独立 heartbeat 已 claim 的 job。实际 Worker image 必须在构建时包含与 execution profile 匹配的 Harbor 版本以及 `benchmark_worker.harbor_agent:TaskRunnerAgent`，不能在任务执行时安装依赖。

## Terminal-Bench 全量题目

仓库内的 `terminal-bench-2.0-real-five` 是离线回归 fixture，不代表官方数据集的全部题目。要导入官方仓库当前 `tasks/` 下的题目，先在部署机或受信任的构建机 checkout Terminal-Bench，再导出不包含 verifier 答案的 case 元数据：

```bash
python3 scripts/export_terminal_bench_cases.py /path/to/terminal-bench/tasks \
  --output .runtime/terminal-bench-cases.json
python3 scripts/sync_terminal_bench_cases.py .runtime/terminal-bench-cases.json \
  --server-url https://luxinyu.cloud --dataset-id <dataset-id>
```

`hermes-terminal-bench-all` 使用 Harbor Hub 的 `terminal-bench/terminal-bench@latest`，并按数据库中已导入的 task ID 选择题目。官方任务各自的 `environment/Dockerfile` 与 `tests/Dockerfile` 负责构建任务/验证器镜像；静态 verifier 依赖必须在镜像构建阶段安装，题目测试中针对 agent 生成文件的动态安装仍属于该题的验证逻辑，不能提前假设或合并到一个通用镜像。

完整协议、安全边界与 Harbor sandbox Tool Gateway 约束见 [`docs/benchmark-contract-v1.md`](docs/benchmark-contract-v1.md)。Task Runner 不声明 `agentlab.harbor-tool-gateway.v1` 或不能使用每题 capability 时，Worker 会 fail-closed，绝不会用 sidecar 本地 `/workspace` 代替 Harbor sandbox。
