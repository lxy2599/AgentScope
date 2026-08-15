# AgentLab 单仓库运行指南

这个仓库的根目录就是 AgentScope 控制面；`openhands/`、`hermes/` 和 `benchmarks/` 是同一仓库内、可独立开发的源码目录。OpenClaw 已暂停，不会被默认构建、启动或验收流程引用。

## 一键启动本地运行环境

```bash
./scripts/agentlab.sh bootstrap
# Edit .env: set AGENTLAB_ACCOUNT_EMAIL and ARK_API_KEY.
./scripts/agentlab.sh provision-account
./scripts/agentlab.sh agents-up
./scripts/agentlab.sh status
```

`bootstrap` 只在本机创建 `.env` 并生成会话密钥。先在 `.env` 填写固定的 `AGENTLAB_ACCOUNT_EMAIL` 和 `ARK_API_KEY`（或对应的 `LLM_*` 配置），再运行 `provision-account`：它会使用该固定账号登录或首次注册、复用同名项目或首次创建，并将项目 ID 与一次生成的 Project Token 保存到本机 `.env`（权限为 `0600`）。账号密码只在本次命令中安全读取，**不会**写入 `.env`；后续运行会复用同一个项目和 Token。`agents-up` 不再自动创建随机账号或项目，缺少 Token 时会明确失败并提示先 provision。若不存在本地镜像，它会先构建；显式重建使用 `./scripts/agentlab.sh build`。

首次配置可以直接交互式执行：

```bash
./scripts/agentlab.sh provision-account
```

也可以避免交互输入，将密码只传给单次进程（不要把该命令写进 shell history）：

```bash
read -rsp 'AgentScope 固定账号密码: ' AGENTLAB_ACCOUNT_PASSWORD; echo
export AGENTLAB_ACCOUNT_PASSWORD
./scripts/agentlab.sh provision-account
unset AGENTLAB_ACCOUNT_PASSWORD
```

如需在脚本或 CI 中显式指定身份，可使用：

```bash
AGENTLAB_ACCOUNT_PASSWORD='...' ./scripts/agentlab.sh provision-account \
  --email 'your-fixed-email@example.com' \
  --display-name 'AgentLab Model Test Account' \
  --project-name 'AgentLab Model Tests'
```

Project Token 仍然是随机密钥，这是安全要求；“固定、持久”指账号、项目以及 `.env` 中被复用的同一个 Token，而不是把 Token 设为可预测字符串。

默认本地地址：

- AgentScope：`http://127.0.0.1:18080`
- OpenHands：`http://127.0.0.1:18000`
- Hermes：`http://127.0.0.1:18001`（`GET /healthz`、`POST /v1/chat`）

`.env` 仅保存本机凭据，已被 Git 忽略。默认代理配置对应宿主机 `http://127.0.0.1:7897`，容器内通过 `host.docker.internal:7897` 访问。

## 运行真实 Harbor benchmark

```bash
./benchmarks/scripts/run-real-agent-harbor-smoke.sh openhands
./benchmarks/scripts/run-real-agent-harbor-smoke.sh hermes
```

每个 smoke 会新建隔离项目与临时 runner token，在 Harbor Sandbox 中执行任务，并仅以 Harbor official verifier score 判定结果；Agent 的最终回答、HTTP 200 或 CLI 退出码都不能替代官方评分。

## 日常命令

```bash
make agentlab-up
make agentlab-status
make agentlab-bench RUN=<template-id>
make agentlab-down
```

完整架构与已实施计划位于 [plans/](plans/)。

## 可选：直接运行 OpenHands 源码探针

默认应使用 `./scripts/agentlab.sh agents-up` 启动容器化服务。若要调试顶层 `openhands/` 源码的原生 SDK 埋点，可在 AgentScope 已运行、`.env` 已有项目 token 和模型配置后使用：

```bash
./scripts/start-observed-openhands.sh start
./scripts/start-observed-openhands.sh status
./scripts/start-observed-openhands.sh stop
```

该辅助进程只运行 OpenHands，不会启动 OpenClaw；它调用真实 `Conversation.run()`，并保持进程存活以持续上报 AgentScope heartbeat。
