# Phase 4 容量与故障恢复验收

本文档说明如何使用仓库内的 HTTP 压测器验证 AgentScope 的采集容量、Runtime
状态链路、SSE 延迟和依赖故障恢复。工具不会绕过 Handler 或直接调用内部 Service。

## 前置条件

先启动完整依赖和前后端：

```bash
make db-up
make dev
```

服务端必须启用 PostgreSQL、ClickHouse 和 Redis。测试脚本会通过 HTTP 创建独立账号、
项目、Project Token、Agent 和实例；报告不记录 Token 或 Session Cookie。

## 容量冒烟

```bash
make phase4-load-smoke
```

默认发送 50 个 OTLP 请求，每批 10 个 Span，并使用 4 个并发 Worker。同时驱动
Runtime `start → waiting_tool → completed`，为该项目建立 SSE 连接。JSON 报告写入
`.tmp/phase4-load-report.json`，包含：

- 请求数、Span 数、错误率和丢弃率；
- P50、P95、P99 和最大延迟；
- Runtime 调用与错误数；
- SSE 连接、事件、首事件延迟和事件传输延迟；
- 请求/Span 每秒吞吐与并发、批量、限速、超时配置；
- 阈值结论与违规原因。

可以覆盖参数：

```bash
AGENTSCOPE_LOAD_REQUESTS=1000 \
AGENTSCOPE_LOAD_CONCURRENCY=32 \
AGENTSCOPE_LOAD_BATCH_SIZE=64 \
AGENTSCOPE_LOAD_MAX_P95=1500ms \
make phase4-load-smoke
```

## 多项目目标文件

命令行工具接收权限为 `0600` 的 JSON 文件：

```json
[
  {
    "project_id": "project-uuid",
    "token": "project-token",
    "session_cookie": "signed-session",
    "agent_id": "agent-uuid",
    "instance_id": "instance-id",
    "agent_name": "research-agent",
    "agent_version": "v2",
    "environment": "staging"
  }
]
```

直接运行：

```bash
go run ./cmd/agentscope-loadtest \
  --targets /secure/path/targets.json \
  --duration 60s \
  --rate 200 \
  --concurrency 32 \
  --batch-size 64 \
  --sse --sse-after-id 1 --runtime \
  --max-p95 2s \
  --max-error-rate 0.01 \
  --max-drop-rate 0.001 \
  --output json
```

阈值通过返回 `0`，阈值失败返回 `2`，配置或传输故障返回 `1`。日志和报告只展示项目
数量与聚合结果，不展示凭证。

## 故障恢复

```bash
make phase4-recovery-test
```

脚本按顺序验证：

1. 持续发送真实 OTLP；
2. 暂停 Redis，确认 Run/Trace 仍能写入；
3. 确认 PostgreSQL Outbox 积压；
4. 恢复 Redis，确认 Outbox 清空；
5. 新建 SSE 连接，确认能够收到事件；
6. 暂停 ClickHouse，确认采集错误可观察且客户端非零退出；
7. 恢复 ClickHouse；
8. 强制 Python SDK 队列饱和，确认 drop 指标增长。

脚本会输出 Redis 恢复到 Outbox 清空的耗时，并通过 `trap` 恢复被暂停的容器。
不要在共享或生产环境运行故障恢复测试。
