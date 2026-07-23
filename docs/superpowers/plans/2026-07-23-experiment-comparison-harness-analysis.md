# Phase 3.6 实验对比、回归与 Harness 分析实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `test-driven-development` for every code task and `verification-before-completion` before claiming completion. Steps use checkbox (`- [ ]`) syntax for tracking.

**目标:** 让用户能够对同一个数据集上的 baseline 与 candidate 实验进行可信对比，判断改动带来的质量、成本、延迟、安全和工具轨迹变化；能区分模型替换与 Harness 消融；能发现并定位回归 case。

**Architecture:** 在现有 Experiment/Run/EvalCaseResult 基础上扩展 Harness 元数据，新增 `experiment_comparisons` 表和后端比较服务，新增前端对比页面，扩展 Python SDK 上报元数据。

**Tech Stack:** Go、PostgreSQL/sqlc、ClickHouse、Python SDK、React/Vite/Ant Design、React Query、ViTest、Go test、pytest

## Global Constraints

- 所有提交信息、新增文档、代码注释必须使用中文。
- 开发已在独立 git worktree 中进行: `worktree-phase3-6-experiment-comparison`
- 每个实现任务必须先写失败测试，再实现，再运行验证
- 实现完成后必须跑通 `go test ./...`、`cd python && python -m pytest`、`cd web && npm test -- --run`
- 必须创建可重复执行的 Demo: `make demo-experiment-comparison`
- Demo 使用确定性 target/evaluator，不依赖外部 LLM
- 必须查询 PostgreSQL 与 ClickHouse 验证数据
- 不要修改或回退已有无关改动
- 完成后不要合并 master

---

## 文件清单

### 新建文件
- `migrations/00006_experiment_comparisons.sql` — 新增实验比较相关 schema
- `internal/experiments/compare.go` — 实验对比服务
- `internal/experiments/compare_test.go` — 对比服务测试
- `internal/httpapi/compare_handlers.go` — 比较 API 处理器
- `internal/httpapi/compare_handlers_test.go` — 比较 API 测试
- `internal/adapter/compare.go` — 比较功能的 SQL 适配器
- `internal/adapter/compare_test.go` — 适配器测试
- `web/src/features/experiments/ExperimentComparePage.tsx` — 前端对比页面
- `web/src/features/experiments/ExperimentComparePage.test.tsx` — 前端测试
- `examples/demo_experiment_comparison.py` — 实验对比 Demo 脚本
- `python/tests/test_experiment_metadata.py` — Python 元数据测试

### 修改文件
- `queries/experiments.sql` — 增加实验元数据字段查询
- `internal/store/models.go` — 增加新字段
- `internal/httpapi/router.go` — 注册新接口
- `internal/httpapi/experiments_handler.go` — 扩展创建实验接口接收元数据
- `internal/experiments/service.go` — 扩展 Experiment 类型和创建参数
- `internal/adapter/experiments.go` — 扩展 SQL 适配器
- `python/agentscope_sdk/runner.py` — 扩展 Run 上报 Harness 元数据
- `python/agentscope_sdk/client.py` — 扩展客户端请求
- `web/src/api/hooks.ts` — 新增比较相关 hooks
- `web/src/api/types.ts` — 新增比较相关类型
- `web/src/features/experiments/ExperimentDetailPage.tsx` — 增加"设为基线/发起对比"操作
- `Makefile` — 增加 demo-experiment-comparison 目标

---

### Task 1: 数据库 Schema 扩展

**Files:**
- Create: `migrations/00006_experiment_comparisons.sql`
- Modify: `queries/experiments.sql`
- Modify: `internal/store/models.go`

**Interfaces:**
- Consumes: experiments, runs, datasets 已有 schema
- Produces: experiment_comparisons 表 + comparison_case_results 表 + experiments 表可空新列

- [ ] **Step 1: 写迁移文件**

```sql
-- Migration 00006: Experiment comparisons and harness metadata.
-- Adds columns to experiments for experiment identity/reproducibility,
-- and new tables for baseline/candidate comparison.

-- Harness metadata columns on experiments (nullable for backward compat)
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS model_provider TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS model_name TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS model_version TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS prompt_version TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS harness_version TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS dataset_version TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS toolset_version TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS skillset_version TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS rag_config_version TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS feature_flags JSONB NOT NULL DEFAULT '{}';

-- Experiment comparisons
CREATE TABLE IF NOT EXISTS experiment_comparisons (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    baseline_experiment_id UUID NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    candidate_experiment_id UUID NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    comparison_type TEXT NOT NULL DEFAULT 'general'
        CHECK (comparison_type IN ('model_swap', 'ablation', 'general')),
    created_by UUID NOT NULL REFERENCES users(id),
    status TEXT NOT NULL DEFAULT 'completed',
    summary JSONB NOT NULL DEFAULT '{}',
    regression_gates JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(baseline_experiment_id, candidate_experiment_id)
);

-- Per-case comparison results
CREATE TABLE IF NOT EXISTS comparison_case_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    comparison_id UUID NOT NULL REFERENCES experiment_comparisons(id) ON DELETE CASCADE,
    dataset_case_id UUID NOT NULL REFERENCES dataset_cases(id),
    baseline_run_id UUID REFERENCES runs(id),
    candidate_run_id UUID REFERENCES runs(id),
    baseline_score NUMERIC(10,4),
    candidate_score NUMERIC(10,4),
    baseline_passed BOOLEAN,
    candidate_passed BOOLEAN,
    score_diff NUMERIC(10,4),
    status TEXT NOT NULL DEFAULT 'unchanged'
        CHECK (status IN ('improved', 'regressed', 'unchanged', 'incomparable')),
    baseline_latency_ms BIGINT,
    candidate_latency_ms BIGINT,
    baseline_cost NUMERIC(10,6),
    candidate_cost NUMERIC(10,6),
    details JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(comparison_id, dataset_case_id)
);

CREATE INDEX IF NOT EXISTS idx_comparisons_project ON experiment_comparisons(project_id);
CREATE INDEX IF NOT EXISTS idx_comparisons_baseline ON experiment_comparisons(baseline_experiment_id);
CREATE INDEX IF NOT EXISTS idx_comparisons_candidate ON experiment_comparisons(candidate_experiment_id);
CREATE INDEX IF NOT EXISTS idx_comparison_case_results_comparison ON comparison_case_results(comparison_id);
CREATE INDEX IF NOT EXISTS idx_comparison_case_results_status ON comparison_case_results(status);
```

- [ ] **Step 2: 扩展 sqlc 查询**

在 `queries/experiments.sql` 末尾追加：

```sql
-- name: CreateExperimentComparison :one
INSERT INTO experiment_comparisons (
    project_id, baseline_experiment_id, candidate_experiment_id,
    comparison_type, created_by, status, summary, regression_gates
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetExperimentComparisonByID :one
SELECT * FROM experiment_comparisons WHERE id = $1;

-- name: ListExperimentComparisonsByProject :many
SELECT * FROM experiment_comparisons WHERE project_id = $1 ORDER BY created_at DESC;

-- name: ListExperimentComparisonsByExperiment :many
SELECT * FROM experiment_comparisons
WHERE baseline_experiment_id = $1 OR candidate_experiment_id = $1
ORDER BY created_at DESC;

-- name: CreateComparisonCaseResult :one
INSERT INTO comparison_case_results (
    comparison_id, dataset_case_id,
    baseline_run_id, candidate_run_id,
    baseline_score, candidate_score,
    baseline_passed, candidate_passed,
    score_diff, status,
    baseline_latency_ms, candidate_latency_ms,
    baseline_cost, candidate_cost,
    details
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: ListComparisonCaseResults :many
SELECT * FROM comparison_case_results WHERE comparison_id = $1 ORDER BY created_at ASC;

-- name: DeleteComparisonCaseResults :exec
DELETE FROM comparison_case_results WHERE comparison_id = $1;

-- name: DeleteExperimentComparison :exec
DELETE FROM experiment_comparisons WHERE id = $1;

-- name: UpdateExperimentComparison :one
UPDATE experiment_comparisons
SET status = $2, summary = $3, regression_gates = $4, updated_at = now()
WHERE id = $1
RETURNING *;
```

- [ ] **Step 3: 重新生成 sqlc 代码**

```bash
cd /home/luxinyu/GolandProjects/AgentScope/.claude/worktrees/phase3-6-experiment-comparison
go generate ./...
# 如果 go generate 没有运行 sqlc，则手动运行:
which sqlc 2>/dev/null && sqlc generate || echo "sqlc not found, will need to run manually after installation"
```

- [ ] **Step 4: 编译验证**

```bash
cd /home/luxinyu/GolandProjects/AgentScope/.claude/worktrees/phase3-6-experiment-comparison
go build ./...
```

Expected: compilation succeeds without errors.

- [ ] **Step 5: 提交**

```bash
git add migrations/ queries/ internal/store/ go.sum go.mod
git commit -m "feat: 扩展数据库 Schema 支持实验对比和 Harness 元数据"
```

---

### Task 2: Go 领域模型扩展

**Files:**
- Modify: `internal/experiments/service.go`

**Interfaces:**
- Consumes: `experiments.Experiment` 现有结构体
- Produces: 扩展后的 Experiment 类型 + CreateExperimentParams + Comparison 领域类型

- [ ] **Step 1: 扩展 Experiment 结构体**

在 `internal/experiments/service.go` 的 Experiment struct 中添加字段：

```go
// Experiment represents an evaluation run against a dataset.
type Experiment struct {
    // ... existing fields ...

    // Harness metadata (optional, for experiment identity/reproducibility)
    ModelProvider  string `json:"model_provider,omitempty"`
    ModelName      string `json:"model_name,omitempty"`
    ModelVersion   string `json:"model_version,omitempty"`
    PromptVersion  string `json:"prompt_version,omitempty"`
    HarnessVersion string `json:"harness_version,omitempty"`
    DatasetVersion string `json:"dataset_version,omitempty"`
    ToolsetVersion string `json:"toolset_version,omitempty"`
    SkillsetVersion string `json:"skillset_version,omitempty"`
    RAGConfigVersion string `json:"rag_config_version,omitempty"`
    FeatureFlags   string `json:"feature_flags,omitempty"`
}
```

- [ ] **Step 2: 添加 Comparison 领域类型**

在 `internal/experiments/service.go` 末尾加入：

```go
// ComparisonType enumerates supported comparison modes.
type ComparisonType string

const (
    ComparisonModelSwap  ComparisonType = "model_swap"
    ComparisonAblation   ComparisonType = "ablation"
    ComparisonGeneral    ComparisonType = "general"
)

// RegressionGate defines configurable thresholds for regression detection.
type RegressionGate struct {
    MinPassRateDelta       float64 `json:"min_pass_rate_delta,omitempty"`
    MinScoreDelta          float64 `json:"min_score_delta,omitempty"`
    MaxCostIncreaseRatio   float64 `json:"max_cost_increase_ratio,omitempty"`
    MaxP95LatencyIncrease  float64 `json:"max_p95_latency_increase,omitempty"`
    SafetyVetoEnabled      bool    `json:"safety_veto_enabled,omitempty"`
}

// ComparisonSummary holds aggregate results for a comparison.
type ComparisonSummary struct {
    TotalCases       int     `json:"total_cases"`
    PairedCases      int     `json:"paired_cases"`
    BaselinePassRate float64 `json:"baseline_pass_rate"`
    CandidatePassRate float64 `json:"candidate_pass_rate"`
    BaselineAvgScore float64 `json:"baseline_avg_score"`
    CandidateAvgScore float64 `json:"candidate_avg_score"`
    AvgScoreDelta    float64 `json:"avg_score_delta"`
    ImprovedCases    int     `json:"improved_cases"`
    RegressedCases   int     `json:"regressed_cases"`
    UnchangedCases   int     `json:"unchanged_cases"`
    IncomparableCases int    `json:"incomparable_cases"`
    BaselineP95LatencyMs int64 `json:"baseline_p95_latency_ms"`
    CandidateP95LatencyMs int64 `json:"candidate_p95_latency_ms"`
    LatencyDeltaPct  float64 `json:"latency_delta_pct"`
    BaselineAvgCost  float64 `json:"baseline_avg_cost"`
    CandidateAvgCost float64 `json:"candidate_avg_cost"`
    CostDeltaPct     float64 `json:"cost_delta_pct"`
    BaselineErrorRate float64 `json:"baseline_error_rate"`
    CandidateErrorRate float64 `json:"candidate_error_rate"`
    SafetyRegressed  bool    `json:"safety_regressed"`
    RegressionTriggered bool `json:"regression_triggered"`
    RegressionReasons []string `json:"regression_reasons,omitempty"`
}

// Comparison represents a baseline vs candidate experiment comparison.
type Comparison struct {
    ID                     string          `json:"id"`
    ProjectID              string          `json:"project_id"`
    BaselineExperimentID   string          `json:"baseline_experiment_id"`
    CandidateExperimentID  string          `json:"candidate_experiment_id"`
    ComparisonType         ComparisonType  `json:"comparison_type"`
    CreatedBy              string          `json:"created_by"`
    Status                 string          `json:"status"`
    Summary                string          `json:"summary"`
    RegressionGates        string          `json:"regression_gates"`
    CreatedAt              time.Time       `json:"created_at"`
    UpdatedAt              time.Time       `json:"updated_at"`
}

// ComparisonCaseResult stores per-case comparison data.
type ComparisonCaseResult struct {
    ID              string    `json:"id"`
    ComparisonID    string    `json:"comparison_id"`
    DatasetCaseID   string    `json:"dataset_case_id"`
    BaselineRunID   string    `json:"baseline_run_id,omitempty"`
    CandidateRunID  string    `json:"candidate_run_id,omitempty"`
    BaselineScore   float64   `json:"baseline_score,omitempty"`
    CandidateScore  float64   `json:"candidate_score,omitempty"`
    BaselinePassed  bool      `json:"baseline_passed"`
    CandidatePassed bool      `json:"candidate_passed"`
    ScoreDiff       float64   `json:"score_diff,omitempty"`
    Status          string    `json:"status"`
    BaselineLatencyMs int64   `json:"baseline_latency_ms,omitempty"`
    CandidateLatencyMs int64  `json:"candidate_latency_ms,omitempty"`
    BaselineCost    float64   `json:"baseline_cost,omitempty"`
    CandidateCost   float64   `json:"candidate_cost,omitempty"`
    Details         string    `json:"details,omitempty"`
    CreatedAt       time.Time `json:"created_at"`
}

// ComparisonDetail is the full comparison response.
type ComparisonDetail struct {
    Comparison
    BaselineExperiment   Experiment              `json:"baseline_experiment"`
    CandidateExperiment  Experiment              `json:"candidate_experiment"`
    ComparisonSummary    ComparisonSummary        `json:"comparison_summary"`
    CaseResults          []ComparisonCaseResult   `json:"case_results"`
}
```

- [ ] **Step 3: 扩展 Querier 接口**

在 `internal/experiments/service.go` 的 Querier 接口中添加：

```go
// Comparison operations
CreateExperimentComparison(ctx context.Context, comparison Comparison, caseResults []ComparisonCaseResult) (Comparison, error)
GetExperimentComparison(ctx context.Context, comparisonID string) (ComparisonDetail, error)
ListExperimentComparisons(ctx context.Context, projectID string) ([]Comparison, error)
UpdateExperimentComparisonStatus(ctx context.Context, comparisonID, status, summary, regressionGates string) error
```

- [ ] **Step 4: 编译验证**

```bash
cd /home/luxinyu/GolandProjects/AgentScope/.claude/worktrees/phase3-6-experiment-comparison
go build ./...
```

Expected: compilation succeeds.

- [ ] **Step 5: 提交**

```bash
git add internal/experiments/service.go
git commit -m "feat: 扩展实验领域模型支持 Harness 元数据和比较类型"
```

---

### Task 3: 适配器与数据访问层

**Files:**
- Create: `internal/adapter/compare.go`
- Modify: `internal/adapter/experiments.go`

- [ ] **Step 1: 写适配器测试**

Create `internal/adapter/compare_test.go`:

```go
package adapter

import (
    "context"
    "testing"
)

func TestComparisonQuerier(t *testing.T) {
    // placeholder — real tests need a DB connection
    t.Skip("integration test requires PostgreSQL")
}
```

- [ ] **Step 2: 实现 compare.go 适配器**

Create `internal/adapter/compare.go`:

```go
package adapter

import (
    "context"
    "encoding/json"
    "fmt"

    "AgentScope/internal/experiments"
    "AgentScope/internal/store"
    "github.com/jackc/pgx/v5/pgtype"
    "github.com/jackc/pgx/v5/pgxpool"
)

// ComparisonQuerier implements experiments.Querier comparison operations.
type ComparisonQuerier struct {
    q    *store.Queries
    pool *pgxpool.Pool
}

func NewComparisonQuerier(q *store.Queries, pool *pgxpool.Pool) *ComparisonQuerier {
    return &ComparisonQuerier{q: q, pool: pool}
}

func (cq *ComparisonQuerier) CreateExperimentComparison(
    ctx context.Context,
    comparison experiments.Comparison,
    caseResults []experiments.ComparisonCaseResult,
) (experiments.Comparison, error) {
    tx, err := cq.pool.Begin(ctx)
    if err != nil {
        return experiments.Comparison{}, fmt.Errorf("begin tx: %w", err)
    }
    defer tx.Rollback(ctx)

    tq := cq.q.WithTx(tx)

    summaryBytes, _ := json.Marshal(comparison.Summary)
    gatesBytes, _ := json.Marshal(comparison.RegressionGates)

    storeComp, err := tq.CreateExperimentComparison(ctx, store.CreateExperimentComparisonParams{
        ProjectID:              parsePGUUID(comparison.ProjectID),
        BaselineExperimentID:   parsePGUUID(comparison.BaselineExperimentID),
        CandidateExperimentID:  parsePGUUID(comparison.CandidateExperimentID),
        ComparisonType:         string(comparison.ComparisonType),
        CreatedBy:              parsePGUUID(comparison.CreatedBy),
        Status:                 comparison.Status,
        Summary:                summaryBytes,
        RegressionGates:        gatesBytes,
    })
    if err != nil {
        return experiments.Comparison{}, fmt.Errorf("create comparison: %w", err)
    }

    for _, cr := range caseResults {
        var blRunID, candRunID pgtype.UUID
        if cr.BaselineRunID != "" {
            blRunID = parsePGUUID(cr.BaselineRunID)
        }
        if cr.CandidateRunID != "" {
            candRunID = parsePGUUID(cr.CandidateRunID)
        }

        detailsBytes, _ := json.Marshal(cr.Details)

        _, err := tq.CreateComparisonCaseResult(ctx, store.CreateComparisonCaseResultParams{
            ComparisonID:      storeComp.ID,
            DatasetCaseID:     parsePGUUID(cr.DatasetCaseID),
            BaselineRunID:     blRunID,
            CandidateRunID:    candRunID,
            BaselineScore:     pgNumeric(cr.BaselineScore),
            CandidateScore:    pgNumeric(cr.CandidateScore),
            BaselinePassed:    cr.BaselinePassed,
            CandidatePassed:   cr.CandidatePassed,
            ScoreDiff:         pgNumeric(cr.ScoreDiff),
            Status:            cr.Status,
            BaselineLatencyMs: cr.BaselineLatencyMs,
            CandidateLatencyMs: cr.CandidateLatencyMs,
            BaselineCost:      pgNumeric(cr.BaselineCost),
            CandidateCost:     pgNumeric(cr.CandidateCost),
            Details:           detailsBytes,
        })
        if err != nil {
            return experiments.Comparison{}, fmt.Errorf("create case result: %w", err)
        }
    }

    if err := tx.Commit(ctx); err != nil {
        return experiments.Comparison{}, fmt.Errorf("commit tx: %w", err)
    }

    // Convert store model to domain model
    return storeComparisonToDomain(storeComp), nil
}
```

Also add `storeComparisonToDomain` and helper functions.

- [ ] **Step 3: 扩展实验创建支持元数据**

Modify `internal/adapter/experiments.go` `CreateExperimentWithJobs` to accept and store harness metadata fields.

- [ ] **Step 4: 编译验证**

```bash
cd /home/luxinyu/GolandProjects/AgentScope/.claude/worktrees/phase3-6-experiment-comparison
go build ./...
```

- [ ] **Step 5: 提交**

```bash
git add internal/adapter/compare.go internal/adapter/compare_test.go internal/adapter/experiments.go
git commit -m "feat: 实现实验比较功能的适配器和数据访问层"
```

---

### Task 4: 实验对比服务

**Files:**
- Create: `internal/experiments/compare.go`
- Create: `internal/experiments/compare_test.go`

- [ ] **Step 1: 写失败测试**

```go
package experiments

import (
    "context"
    "testing"
)

func TestCreateComparison_ValidInput(t *testing.T) {
    s := NewCompareService(nil, nil, nil)
    _, err := s.CreateComparison(context.Background(), "", "", "", "", ComparisonGeneral, "", nil)
    if err == nil {
        t.Fatal("expected error for empty projectID")
    }
}

func TestCreateComparison_SameExperiment(t *testing.T) {
    s := NewCompareService(nil, nil, nil)
    _, err := s.CreateComparison(context.Background(), "p1", "e1", "e1", "u1", ComparisonGeneral, "", nil)
    if err == nil {
        t.Fatal("expected error for same baseline/candidate")
    }
}

func TestCreateComparison_DifferentProject(t *testing.T) {
    mock := &mockCompareQuerier{
        getExp: func(id string) (Experiment, error) {
            if id == "bl" {
                return Experiment{ProjectID: "p1"}, nil
            }
            return Experiment{ProjectID: "p2"}, nil
        },
    }
    s := NewCompareService(mock, nil, nil)
    _, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, "", nil)
    if err == nil {
        t.Fatal("expected error for different project experiments")
    }
}

func TestModelSwapComparison_DifferentHarness(t *testing.T) {
    mock := &mockCompareQuerier{
        getExp: func(id string) (Experiment, error) {
            if id == "bl" {
                return Experiment{ProjectID: "p1", HarnessVersion: "v1", PromptVersion: "p1"}, nil
            }
            return Experiment{ProjectID: "p1", HarnessVersion: "v2", PromptVersion: "p2"}, nil
        },
        listResults: func(expID string) ([]EvalCaseResult, error) {
            return nil, nil
        },
    }
    s := NewCompareService(mock, nil, nil)
    gates := `{"min_pass_rate_delta":0.1}`
    _, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonModelSwap, gates, nil)
    if err == nil {
        t.Fatal("expected warning/error for different harness in model_swap")
    }
}

// Mock for tests
type mockCompareQuerier struct {
    getExp       func(string) (Experiment, error)
    listResults  func(string) ([]EvalCaseResult, error)
    listRuns     func(string) ([]interface{}, error)
}

func (m *mockCompareQuerier) GetExperimentBasic(ctx context.Context, id string) (Experiment, error) {
    return m.getExp(id)
}

func (m *mockCompareQuerier) ListEvalResults(ctx context.Context, expID string) ([]EvalCaseResult, error) {
    if m.listResults != nil {
        return m.listResults(expID)
    }
    return nil, nil
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/luxinyu/GolandProjects/AgentScope/.claude/worktrees/phase3-6-experiment-comparison
go test ./internal/experiments/ -run "TestCreateComparison|TestModelSwap" -v
```

Expected: Tests fail with compilation errors (no CompareService defined).

- [x] **Step 3: 实现 compare.go**

```go
package experiments

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "math"
    "sort"
)

// CompareService provides experiment comparison operations.
type CompareService struct {
    q         Querier
    compareQ  CompareQuerier
    evalQ     EvalQuerier
}

// CompareQuerier defines comparison-specific database operations.
type CompareQuerier interface {
    CreateExperimentComparison(ctx context.Context, comparison Comparison, caseResults []ComparisonCaseResult) (Comparison, error)
    GetExperimentComparison(ctx context.Context, comparisonID string) (ComparisonDetail, error)
    ListExperimentComparisons(ctx context.Context, projectID string) ([]Comparison, error)
    UpdateExperimentComparisonStatus(ctx context.Context, comparisonID, status, summary, regressionGates string) error
}

// EvalQuerier defines the minimal eval operations needed for comparison.
type EvalQuerier interface {
    ListEvalResults(ctx context.Context, experimentID string) ([]EvalCaseResult, error)
}

func NewCompareService(q Querier, compareQ CompareQuerier, evalQ EvalQuerier) *CompareService {
    return &CompareService{q: q, compareQ: compareQ, evalQ: evalQ}
}

// CreateComparison creates a comparison between baseline and candidate experiments.
func (s *CompareService) CreateComparison(
    ctx context.Context,
    projectID, baselineExpID, candidateExpID, createdBy string,
    compType ComparisonType,
    regressionGatesJSON string,
    gates *RegressionGate,
) (ComparisonDetail, error) {
    // Validation
    if projectID == "" {
        return ComparisonDetail{}, errors.New("projectID is required")
    }
    if baselineExpID == "" || candidateExpID == "" {
        return ComparisonDetail{}, errors.New("baseline and candidate experiment IDs are required")
    }
    if baselineExpID == candidateExpID {
        return ComparisonDetail{}, errors.New("baseline and candidate must be different experiments")
    }

    // Fetch both experiments
    baseline, err := s.q.GetExperimentBasic(ctx, baselineExpID)
    if err != nil {
        return ComparisonDetail{}, fmt.Errorf("get baseline experiment: %w", err)
    }
    candidate, err := s.q.GetExperimentBasic(ctx, candidateExpID)
    if err != nil {
        return ComparisonDetail{}, fmt.Errorf("get candidate experiment: %w", err)
    }

    // Verify same project
    if baseline.ProjectID != projectID || candidate.ProjectID != projectID {
        return ComparisonDetail{}, errors.New("both experiments must belong to the specified project")
    }

    // Verify same dataset (required for paired comparison)
    if baseline.DatasetID != candidate.DatasetID {
        return ComparisonDetail{}, errors.New("baseline and candidate must use the same dataset")
    }

    // Type-specific validation
    var warnings []string
    if compType == ComparisonModelSwap {
        if baseline.HarnessVersion != "" && candidate.HarnessVersion != "" &&
            baseline.HarnessVersion != candidate.HarnessVersion {
            warnings = append(warnings, "模型替换中 Harness 版本不一致，建议固定 Harness 配置")
        }
        if baseline.PromptVersion != "" && candidate.PromptVersion != "" &&
            baseline.PromptVersion != candidate.PromptVersion {
            warnings = append(warnings, "模型替换中 Prompt 版本不一致，建议固定 Prompt 配置")
        }
    }
    if compType == ComparisonAblation {
        if baseline.ModelName != "" && candidate.ModelName != "" &&
            baseline.ModelName != candidate.ModelName {
            warnings = append(warnings, "消融实验中模型应保持一致，但检测到不同模型")
        }
    }

    // Get results for both experiments
    blResults, err := s.evalQ.ListEvalResults(ctx, baselineExpID)
    if err != nil {
        return ComparisonDetail{}, fmt.Errorf("list baseline results: %w", err)
    }
    candResults, err := s.evalQ.ListEvalResults(ctx, candidateExpID)
    if err != nil {
        return ComparisonDetail{}, fmt.Errorf("list candidate results: %w", err)
    }

    // Pair by dataset_case_id
    blByCase := make(map[string]EvalCaseResult, len(blResults))
    for _, r := range blResults {
        blByCase[r.DatasetCaseID] = r
    }
    candByCase := make(map[string]EvalCaseResult, len(candResults))
    for _, r := range candResults {
        candByCase[r.DatasetCaseID] = r
    }

    // Build paired case results
    allCaseIDs := make(map[string]bool)
    for _, r := range blResults {
        allCaseIDs[r.DatasetCaseID] = true
    }
    for _, r := range candResults {
        allCaseIDs[r.DatasetCaseID] = true
    }

    var caseResults []ComparisonCaseResult
    for caseID := range allCaseIDs {
        bl, hasBL := blByCase[caseID]
        cand, hasCand := candByCase[caseID]

        cr := ComparisonCaseResult{
            DatasetCaseID: caseID,
        }

        if !hasBL || !hasCand {
            cr.Status = "incomparable"
            if hasBL {
                cr.BaselineRunID = bl.RunID
                cr.BaselineScore = bl.Score
                cr.BaselinePassed = bl.Passed
                cr.BaselineLatencyMs = bl.LatencyMs
            }
            if hasCand {
                cr.CandidateRunID = cand.RunID
                cr.CandidateScore = cand.Score
                cr.CandidatePassed = cand.Passed
                cr.CandidateLatencyMs = cand.LatencyMs
            }
            caseResults = append(caseResults, cr)
            continue
        }

        cr.BaselineRunID = bl.RunID
        cr.CandidateRunID = cand.RunID
        cr.BaselineScore = bl.Score
        cr.CandidateScore = cand.Score
        cr.BaselinePassed = bl.Passed
        cr.CandidatePassed = cand.Passed
        cr.BaselineLatencyMs = bl.LatencyMs
        cr.CandidateLatencyMs = cand.LatencyMs
        cr.BaselineCost = bl.Cost
        cr.CandidateCost = cand.Cost

        // Calculate score diff
        cr.ScoreDiff = cand.Score - bl.Score

        // Determine status
        if bl.Passed == cand.Passed && cr.ScoreDiff == 0 {
            cr.Status = "unchanged"
        } else if cr.ScoreDiff > 0 || (!bl.Passed && cand.Passed) {
            cr.Status = "improved"
        } else if cr.ScoreDiff < 0 || (bl.Passed && !cand.Passed) {
            cr.Status = "regressed"
        } else {
            cr.Status = "unchanged"
        }

        caseResults = append(caseResults, cr)
    }

    // Calculate summary statistics
    summary := calculateComparisonSummary(caseResults)

    // Apply regression gates
    regressionReasons := applyRegressionGates(summary, gates, warnings)
    if len(regressionReasons) > 0 {
        summary.RegressionTriggered = true
        summary.RegressionReasons = regressionReasons
    }

    // Parse gates JSON
    if gates == nil {
        gates = &RegressionGate{}
    }
    gatesJSON, _ := json.Marshal(gates)
    summaryJSON, _ := json.Marshal(summary)

    comp := Comparison{
        ProjectID:             projectID,
        BaselineExperimentID:  baselineExpID,
        CandidateExperimentID: candidateExpID,
        ComparisonType:        compType,
        CreatedBy:             createdBy,
        Status:                "completed",
        Summary:               string(summaryJSON),
        RegressionGates:       string(gatesJSON),
    }

    savedComp, err := s.compareQ.CreateExperimentComparison(ctx, comp, caseResults)
    if err != nil {
        return ComparisonDetail{}, fmt.Errorf("save comparison: %w", err)
    }

    return ComparisonDetail{
        Comparison:          savedComp,
        BaselineExperiment:  baseline,
        CandidateExperiment: candidate,
        ComparisonSummary:   summary,
        CaseResults:         caseResults,
    }, nil
}

func calculateComparisonSummary(results []ComparisonCaseResult) ComparisonSummary {
    s := ComparisonSummary{
        TotalCases: len(results),
    }

    var blScores, candScores []float64
    var blLatencies, candLatencies []int64
    var blCosts, candCosts []float64
    var blPassed, candPassed int
    var blFailed, candFailed int
    var pairedCases int

    for _, r := range results {
        if r.Status != "incomparable" {
            pairedCases++
        }

        switch r.Status {
        case "improved":
            s.ImprovedCases++
        case "regressed":
            s.RegressedCases++
        case "unchanged":
            s.UnchangedCases++
        case "incomparable":
            s.IncomparableCases++
        }

        if r.BaselineRunID != "" {
            blScores = append(blScores, r.BaselineScore)
            blLatencies = append(blLatencies, r.BaselineLatencyMs)
            blCosts = append(blCosts, r.BaselineCost)
            if r.BaselinePassed {
                blPassed++
            } else {
                blFailed++
            }
        }
        if r.CandidateRunID != "" {
            candScores = append(candScores, r.CandidateScore)
            candLatencies = append(candLatencies, r.CandidateLatencyMs)
            candCosts = append(candCosts, r.CandidateCost)
            if r.CandidatePassed {
                candPassed++
            } else {
                candFailed++
            }
        }
    }

    s.PairedCases = pairedCases

    // Pass rates
    totalBL := blPassed + blFailed
    if totalBL > 0 {
        s.BaselinePassRate = float64(blPassed) / float64(totalBL)
    }
    totalCand := candPassed + candFailed
    if totalCand > 0 {
        s.CandidatePassRate = float64(candPassed) / float64(totalCand)
    }

    // Average scores
    s.BaselineAvgScore = avg(blScores)
    s.CandidateAvgScore = avg(candScores)
    s.AvgScoreDelta = s.CandidateAvgScore - s.BaselineAvgScore

    // P95 latency
    s.BaselineP95LatencyMs = p95(blLatencies)
    s.CandidateP95LatencyMs = p95(candLatencies)
    if s.BaselineP95LatencyMs > 0 {
        s.LatencyDeltaPct = float64(s.CandidateP95LatencyMs-s.BaselineP95LatencyMs) / float64(s.BaselineP95LatencyMs) * 100
    }

    // Average cost
    s.BaselineAvgCost = avg(blCosts)
    s.CandidateAvgCost = avg(candCosts)
    if s.BaselineAvgCost > 0 {
        s.CostDeltaPct = (s.CandidateAvgCost - s.BaselineAvgCost) / s.BaselineAvgCost * 100
    }

    // Error rates
    if totalBL > 0 {
        s.BaselineErrorRate = float64(blFailed) / float64(totalBL)
    }
    if totalCand > 0 {
        s.CandidateErrorRate = float64(candFailed) / float64(totalCand)
    }

    return s
}

func applyRegressionGates(summary ComparisonSummary, gates *RegressionGate, warnings []string) []string {
    if gates == nil {
        return nil
    }
    var reasons []string
    reasons = append(reasons, warnings...)

    if gates.MinPassRateDelta > 0 {
        delta := summary.CandidatePassRate - summary.BaselinePassRate
        if delta < -gates.MinPassRateDelta {
            reasons = append(reasons, fmt.Sprintf(
                "通过率下降超过阈值: baseline=%.1f%%, candidate=%.1f%%, 阈值=%.1f%%",
                summary.BaselinePassRate*100, summary.CandidatePassRate*100, gates.MinPassRateDelta*100,
            ))
        }
    }

    if gates.MinScoreDelta > 0 && summary.AvgScoreDelta < -gates.MinScoreDelta {
        reasons = append(reasons, fmt.Sprintf(
            "综合分下降超过阈值: delta=%.4f, 阈值=%.4f",
            summary.AvgScoreDelta, -gates.MinScoreDelta,
        ))
    }

    // Safety veto
    if gates.SafetyVetoEnabled && summary.SafetyRegressed {
        reasons = append(reasons, "安全 veto 触发: candidate 新增安全失败")
    }

    return reasons
}

func avg(values []float64) float64 {
    if len(values) == 0 {
        return 0
    }
    var sum float64
    for _, v := range values {
        sum += v
    }
    return sum / float64(len(values))
}

func p95(values []int64) int64 {
    if len(values) == 0 {
        return 0
    }
    sorted := make([]int64, len(values))
    copy(sorted, values)
    sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
    idx := int(math.Ceil(float64(len(sorted))*0.95)) - 1
    if idx < 0 {
        idx = 0
    }
    if idx >= len(sorted) {
        idx = len(sorted) - 1
    }
    return sorted[idx]
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/luxinyu/GolandProjects/AgentScope/.claude/worktrees/phase3-6-experiment-comparison
go test ./internal/experiments/ -run "TestCreateComparison|TestModelSwap|TestComparison" -v
```

Expected: All tests pass.

- [ ] **Step 5: 提交**

```bash
git add internal/experiments/compare.go internal/experiments/compare_test.go
git commit -m "feat: 实现实验对比服务和回归判定逻辑"
```

---

### Task 5: HTTP API

**Files:**
- Create: `internal/httpapi/compare_handlers.go`
- Create: `internal/httpapi/compare_handlers_test.go`
- Modify: `internal/httpapi/router.go`
- Modify: `internal/httpapi/handlers.go`

- [ ] **Step 1: 注册路由**

In `router.go`, add routes inside the protected group:

```go
r.Post("/api/projects/{projectID}/comparisons", handleCreateComparison(deps))
r.Get("/api/projects/{projectID}/comparisons", handleListComparisons(deps))
r.Get("/api/comparisons/{comparisonID}", handleGetComparison(deps))
```

Also add the ComparisonQuerier to Dependencies and mount it in app.go.

- [ ] **Step 2: 提交**

```bash
git add internal/httpapi/compare_handlers.go internal/httpapi/router.go
git commit -m "feat: 新增实验对比 HTTP API 路由"
```

---

### Task 6: 前端扩展

**Files:**
- Create: `web/src/features/experiments/ExperimentComparePage.tsx`
- Create: `web/src/features/experiments/ExperimentComparePage.test.tsx`
- Modify: `web/src/features/experiments/ExperimentDetailPage.tsx`
- Modify: `web/src/api/hooks.ts`
- Modify: `web/src/api/types.ts`

- [ ] **Step 1: 扩展类型定义**

Add to `web/src/api/types.ts`:

```typescript
// ---- Experiment Comparison (Phase 3.6) ----
export interface ExperimentComparison {
  id: string;
  project_id: string;
  baseline_experiment_id: string;
  candidate_experiment_id: string;
  comparison_type: 'model_swap' | 'ablation' | 'general';
  created_by: string;
  status: string;
  summary: string;
  regression_gates: string;
  created_at: string;
  updated_at: string;
}

export interface ComparisonSummary {
  total_cases: number;
  paired_cases: number;
  baseline_pass_rate: number;
  candidate_pass_rate: number;
  baseline_avg_score: number;
  candidate_avg_score: number;
  avg_score_delta: number;
  improved_cases: number;
  regressed_cases: number;
  unchanged_cases: number;
  incomparable_cases: number;
  baseline_p95_latency_ms: number;
  candidate_p95_latency_ms: number;
  latency_delta_pct: number;
  baseline_avg_cost: number;
  candidate_avg_cost: number;
  cost_delta_pct: number;
  baseline_error_rate: number;
  candidate_error_rate: number;
  safety_regressed: boolean;
  regression_triggered: boolean;
  regression_reasons?: string[];
}

export interface ComparisonCaseResult {
  id: string;
  comparison_id: string;
  dataset_case_id: string;
  baseline_run_id?: string;
  candidate_run_id?: string;
  baseline_score: number;
  candidate_score: number;
  baseline_passed: boolean;
  candidate_passed: boolean;
  score_diff: number;
  status: 'improved' | 'regressed' | 'unchanged' | 'incomparable';
  baseline_latency_ms?: number;
  candidate_latency_ms?: number;
  baseline_cost?: number;
  candidate_cost?: number;
  details?: string;
  created_at: string;
}

export interface ComparisonDetail {
  id: string;
  project_id: string;
  baseline_experiment_id: string;
  candidate_experiment_id: string;
  comparison_type: string;
  created_by: string;
  status: string;
  summary: string;
  regression_gates: string;
  created_at: string;
  updated_at: string;
  baseline_experiment: Experiment;
  candidate_experiment: Experiment;
  comparison_summary: ComparisonSummary;
  case_results: ComparisonCaseResult[];
}
```

- [ ] **Step 2: 前端测试**

Create `ExperimentComparePage.test.tsx` with comprehensive tests.

- [ ] **Step 3: 提交**

```bash
git add web/src/features/experiments/ web/src/api/hooks.ts web/src/api/types.ts
git commit -m "feat: 新增实验对比页面和前端类型"
```

---

### Task 7: Python SDK 扩展

**Files:**
- Modify: `python/agentscope_sdk/runner.py`
- Modify: `python/agentscope_sdk/client.py`
- Create: `python/tests/test_experiment_metadata.py`

- [ ] **Step 1: 写测试**

Create `python/tests/test_experiment_metadata.py`:

```python
"""Tests for experiment metadata and harness version reporting."""

import pytest
from agentscope_sdk.runner import HarnessMetadata


def test_harness_metadata_defaults():
    md = HarnessMetadata()
    assert md.model_provider == ""
    assert md.model_name == ""
    assert md.feature_flags == {}


def test_harness_metadata_custom():
    md = HarnessMetadata(
        model_provider="openai",
        model_name="gpt-4",
        model_version="0613",
        prompt_version="v1.2",
        harness_version="1.0.0",
        feature_flags={"tool_retry": True},
    )
    assert md.model_provider == "openai"
    assert md.feature_flags == {"tool_retry": True}
```

- [ ] **Step 2: 提交**

```bash
git add python/agentscope_sdk/ python/tests/
git commit -m "feat: 扩展 Python SDK 支持实验元数据上报"
```

---

### Task 8: Demo 脚本

**Files:**
- Create: `examples/demo_experiment_comparison.py`
- Modify: `Makefile`

- [ ] **Step 1: 创建 Demo 脚本**

A script that:
1. Creates baseline experiment and runs deterministic targets with known results
2. Creates candidate experiment with one improved and one regressed case
3. Runs comparison with regression gates
4. Verifies results match expectations
5. Verifies PostgreSQL and ClickHouse data integrity

- [ ] **Step 3: 提交**

```bash
git add examples/ Makefile
git commit -m "feat: 新增实验对比闭环演示脚本"
```

---

## 最终验证

```bash
# 1. Go 测试
cd /home/luxinyu/GolandProjects/AgentScope/.claude/worktrees/phase3-6-experiment-comparison
go test ./... 2>&1 | tail -20

# 2. Python 测试
cd python && python -m pytest 2>&1 | tail -20

# 3. 前端测试
cd web && npm test -- --run 2>&1 | tail -20

# 4. 前端构建
cd web && npm run build 2>&1 | tail -10

# 5. Demo
make demo-experiment-comparison 2>&1 | tail -50

# 6. 回归验证
make demo-trace-dataset-loop 2>&1 | tail -20

# 7. PostgreSQL 验证
# 查询 experiment_comparisons, comparison_case_results 等表
