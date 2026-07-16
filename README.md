# AgentScope

Agent 可观测与评测系统。

## 快速开始

### 系统要求

- Go 1.23+
- Node.js 20+
- Docker & Docker Compose

### 一键启动

```bash
make dev
```

这会依次完成：
1. 启动 PostgreSQL 和 ClickHouse 容器
2. 应用数据库迁移
3. 启动 Go 后端服务 (http://127.0.0.1:8080)
4. 启动前端开发服务器 (http://127.0.0.1:5173)

按 `Ctrl+C` 停止前端和后端进程；Docker 服务保持运行。

### 端口冲突处理

如果 PostgreSQL 默认端口 5432 或 ClickHouse 端口 9000 被占用，`make dev` 脚本会自动选择可用端口（从 15432 / 19100 开始递增）。

### 端到端演示

1. 启动服务后，在另一个终端运行：

```bash
make demo
```

或直接运行：

```bash
./scripts/demo.sh
```

演示脚本会：
1. 注册/登录一个临时用户
2. 创建项目
3. 创建 Project Token
4. 创建数据集并添加 4 个测试用例
5. 创建实验
6. 使用 Python runner 执行评测（mock target，不依赖真实 LLM）
7. 输出结果和资源 ID

输出示例：

```
============================================================
演示完成！以下是可通过浏览器访问的资源：
============================================================

  前端地址:   http://127.0.0.1:5173
  项目:       http://127.0.0.1:5173/projects/<project-uuid>
  实验详情:   http://127.0.0.1:5173/experiments/<experiment-uuid>
  实验详情:   http://127.0.0.1:5173/experiments/<experiment-uuid>
  API 结果:   http://127.0.0.1:8080/api/projects/<project-id>/experiments/<experiment-id>/results
```

### 浏览器查看

1. 打开 **http://127.0.0.1:5173**
2. 如果未登录，使用 demo 输出的邮箱和密码登录（或注册新账号）
3. 登录后可看到：
   - **Projects 列表** — 所有项目
   - **Project Home** — 项目详情、数据集、实验列表
   - **Dataset** — 创建数据集、添加测试用例、查看用例
   - **Experiment** — 创建实验、查看状态汇总
   - **Experiment Detail** — 实验详情、测试结果表格、跳转到 Run Trace
   - **Runs** — 项目下所有运行记录
   - **Run Detail** — 单次运行的 trace span 和事件

## 架构

```
cmd/agentscope-server/     # Go 服务端入口
internal/
  app/                     # 依赖装配和生命周期
  config/                  # 环境变量读取
  store/                   # PostgreSQL pgx/sqlc 数据访问
  tracestore/              # ClickHouse trace 明细访问
  httpapi/                 # chi 路由和 handler
  auth/                    # 用户、session、密码
  projects/                # 项目、成员、project token
  runs/                    # live/eval 统一 Run 模型
  ingest/                  # OTLP trace ingestion
  datasets/                # 数据集和样本
  experiments/             # 实验、聚合结果
  runnerapi/               # 远程 runner job 协议
  evaluators/              # evaluator 配置校验
migrations/                # PostgreSQL goose SQL migration
clickhouse/migrations/     # ClickHouse trace schema migration
queries/                   # PostgreSQL sqlc SQL
python/agentscope_sdk/     # Python SDK 和 runner
web/                       # React + TypeScript + Vite UI
examples/                  # demo agent 和 dataset
scripts/                   # dev.sh 和 demo.sh 启动脚本
```

## 关键命令

| 命令 | 说明 |
|------|------|
| `make test` | 运行 Go 测试 |
| `make test-python` | 运行 Python SDK 测试 |
| `make web-test` | 运行前端测试 |
| `make web-build` | 前端生产构建 |
| `make test-all` | 运行所有测试 |
| `make db-up` | 启动 PostgreSQL 和 ClickHouse |
| `make db-down` | 停止本地依赖 |
| `make server` | 单独启动 Go 后端 |
| `make dev` | 一键启动（依赖 + 后端 + 前端） |
| `make demo` | 运行端到端演示脚本 |

## API 端点

### 认证

- `POST /api/auth/register` — 注册新用户
- `POST /api/auth/login` — 登录

### 项目

- `POST /api/projects` — 创建项目
- `GET /api/projects` — 列出我的项目
- `POST /api/projects/{projectID}/tokens` — 创建 project token

### 数据集

- `POST /api/projects/{projectID}/datasets` — 创建数据集
- `GET /api/projects/{projectID}/datasets` — 列出项目下数据集
- `POST /api/datasets/{datasetID}/cases` — 添加测试用例
- `GET /api/datasets/{datasetID}/cases` — 列出用例

### 实验

- `POST /api/projects/{projectID}/experiments` — 创建实验
- `GET /api/projects/{projectID}/experiments` — 列出项目下实验
- `GET /api/experiments/{experimentID}` — 实验详情
- `GET /api/projects/{projectID}/experiments/{experimentID}/results` — 获取实验结果

### Runs

- `GET /api/projects/{projectID}/runs` — 列出项目下 runs
- `GET /api/runs/{runID}` — Run 详情（含 trace spans）

### 健康检查

- `GET /healthz` — 健康检查

### Runner

- `GET /runner/jobs/claim` — 签出 job
- `POST /runner/jobs/{jobID}/heartbeat` — 心跳
- `POST /runner/jobs/{jobID}/results` — 提交结果
- `POST /runner/jobs/{jobID}/fail` — 标记失败

### Trace Ingestion (OTLP)

- `POST /v1/traces` — 接收 OpenTelemetry trace
