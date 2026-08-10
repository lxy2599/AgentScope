# AgentLab 单仓库运行指南

这个仓库的根目录就是 AgentScope 控制面；`openhands/`、`hermes/` 和 `benchmarks/` 是同一仓库内、可独立开发的源码目录。OpenClaw 已暂停，不会被默认构建、启动或验收流程引用。

## 一键启动本地运行环境

```bash
./scripts/agentlab.sh bootstrap
./scripts/agentlab.sh agents-up
./scripts/agentlab.sh status
```

`bootstrap` 只在本机创建 `.env` 并生成会话密钥；在 `.env` 中填写 `ARK_API_KEY`（或对应的 `LLM_*` 配置）后，`agents-up` 会启动 AgentScope、PostgreSQL、ClickHouse、OpenHands、Hermes 和常驻 Harbor benchmark worker。若不存在本地镜像，它会先构建；显式重建使用 `./scripts/agentlab.sh build`。

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
