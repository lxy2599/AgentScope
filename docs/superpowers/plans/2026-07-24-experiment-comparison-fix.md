# Phase 3.6 实验对比修复实施计划

**Goal:** 修复实验对比的完整可靠性、Harness 元数据错误处理、比较可信性、前端闭环、Demo 可重复执行和全量测试覆盖。

**Architecture:** 在现有 Phase 3.6 worktree 基础上，先 rebase 到 master (1ffb334)，再按系统化任务逐一修复。

**Tech Stack:** Go、PostgreSQL/sqlc、ClickHouse、Python SDK/pytest/mock、React/Vite/Ant Design、ViTest

## Global Constraints

- 所有提交使用中文，worktree 完成后不合并 master
- 每个任务先写失败测试再实现
- 必须使用 `systematic-debugging` 定位所有现实或隐蔽的问题
- 最终必须跑通 `go test ./...`、`cd python && python -m pytest`、`cd web && npm test -- --run && npm run build`、`make demo-experiment-comparison`、`make demo-trace-dataset-loop`
- 不要修改无关功能

---

### Task 0: Rebase + 迁移文件重命名

- Rename `migrations/00006_experiment_comparisons.sql` → `migrations/00007_experiment_comparisons.sql`
- Delete `httpapi.test` binary
- Stash unstaged `experiment_service.go` mods
- Rebase onto master (1ffb334)
- Regenerate sqlc
- Verify build

### Task A: Harness 元数据错误处理 + 完整字段映射
- Change create experiment handler: save metadata before returning 201
- Return error if metadata save fails (rollback experiment)
- Full field mapping for toolset/skillset/rag_config in adapter and HTTP DTO
- Python SDK: HarnessMetadata import from real module, mock HTTP transport test

### Task B: 比较可信性
- safety_regressed: read eval_scores for safety metric on paired runs
- multidimensional metric comparison per case
- model_swap/ablation strict validation with 400 errors
- completed-only, same project/dataset validation with tests

### Task C: 前端闭环
- "Create comparison" button on ExperimentDetailPage
- Comparison type and regression gates form
- Annotation queue entry for regression cases

### Task D: 比较服务测试 (compare_test.go)
- Pairing/improved/regressed/incomparable
- Pass/score/cost/latency gates
- Safety veto
- model_swap/ablation constraints
- Cross-project, different dataset, incomplete experiments, missing results

### Task E: Python 测试
- HarnessMetadata from `agentscope_sdk.runner.HarnessMetadata`
- Mock HTTP transport to verify payload

### Task F: Demo
- `make demo-experiment-comparison`
- Deterministic targets with known improved/regressed cases
- Safety veto or cost/latency gate triggered
- PostgreSQL and ClickHouse verification
