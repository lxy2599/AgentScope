# AgentScope Phase 2.5：多维评测体系

## 概述

Phase 2.5 将评测从单一 case result 升级为多维 Agent 质量评估。系统支持对一个 run / trace / case 进行多个维度打分，并在实验报告和 run detail 中展示这些分数。

## 支持的评测维度

| metric_kind | 说明 | 典型 evaluator |
|---|---|---|
| `final_answer` | 最终答案质量 | exact_match, contains, LLM-as-Judge |
| `trajectory` | 推理轨迹合理性 | tool_sequence, LLM-as-Judge |
| `tool_correctness` | 工具调用正确性 | required_tool_called, tool_sequence |
| `argument_correctness` | 论据正确性 | regex, numeric_threshold |
| `rag_groundedness` | RAG 相关性 | 基于 trace 的评估 |
| `safety` | 安全性 | not_contains, LLM-as-Judge |
| `efficiency` | 效率 | latency_threshold |

## 数据模型

### eval_metric_definitions（可选元数据）

```sql
CREATE TABLE eval_metric_definitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id),
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, name)
);
```

### eval_scores（多维得分明细）

```sql
CREATE TABLE eval_scores (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id),
    run_id UUID NOT NULL REFERENCES runs(id),
    trace_id TEXT NOT NULL DEFAULT '',
    span_id TEXT NOT NULL DEFAULT '',
    metric_name TEXT NOT NULL,
    metric_kind TEXT NOT NULL,
    score NUMERIC(6,4) NOT NULL CHECK (score >= 0.0 AND score <= 1.0),
    passed BOOLEAN NOT NULL DEFAULT false,
    reason TEXT NOT NULL DEFAULT '',
    details JSONB NOT NULL DEFAULT '{}',
    evaluator_run_id UUID REFERENCES runs(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## 评测配置：multi_metrics

在现有的 evaluator config 中添加 `multi_metrics` 数组，即可开启多维评测。

```json
{
  "type": "exact",
  "multi_metrics": [
    {
      "type": "exact",
      "metric_name": "final-answer-quality",
      "metric_kind": "final_answer"
    },
    {
      "type": "latency_threshold",
      "max_ms": 5000,
      "metric_name": "response-efficiency",
      "metric_kind": "efficiency"
    },
    {
      "type": "required_tool_called",
      "tool_name": "search",
      "metric_name": "tool-usage",
      "metric_kind": "tool_correctness"
    }
  ]
}
```

当 `multi_metrics` 存在时：
1. 依次评估每个 metric 配置
2. 每个 metric 的结果作为单独的 eval_score 写入
3. 第一个 metric 的 score/passed 作为主 eval_case_result 的 score/passed

当 `multi_metrics` 不存在时（旧配置）：
- 按原有单 evaluator 逻辑执行，保持向后兼容

## Deterministic Evaluators

支持以下 deterministic evaluators（详见 Python SDK）：

- **exact_match / exact**：最终答案完全匹配预期输出
- **contains**：输出包含期望关键词
- **not_contains**：输出不包含禁止关键词
- **regex**：输出匹配正则表达式
- **numeric_threshold**：数值字段达到阈值
- **latency_threshold**：延迟小于阈值（毫秒）
- **tool_sequence**：trace 中工具调用顺序符合预期
- **required_tool_called**：必须调用某工具

## LLM-as-Judge Evaluators

支持多维度的 LLM Judge（详见 `python/agentscope_sdk/multi_judges.py`）：

- **final_answer_quality**：评估回答的准确性、完整性、清晰度
- **helpfulness**：评估回答的相关性、可操作性、语气
- **safety**：评估回答的无害性、隐私保护、边界遵守
- **trajectory_reasonableness**：评估轨迹的合理性、效率、工具选择

配置方式（需要真实 LLM API key）：

```json
{
  "type": "llm_judge",
  "provider": "openai-compatible",
  "model": "deepseek-chat",
  "api_key_env": "DEEPSEEK_API_KEY",
  "base_url": "https://api.deepseek.com/v1",
  "metric_name": "final-answer-quality",
  "metric_kind": "final_answer",
  "rubric": "自定义评分标准"
}
```

## API 端点

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/projects/{projectID}/metric-definitions` | 创建 metric definition |
| GET | `/api/projects/{projectID}/metric-definitions` | 列出项目下 metric definitions |
| POST | `/api/projects/{projectID}/eval-scores` | 创建单个 eval score |
| POST | `/api/projects/{projectID}/eval-scores/batch` | 批量创建 eval scores |
| GET | `/api/projects/{projectID}/eval-scores/by-run/{runID}` | 按 run 查询 scores |
| GET | `/api/projects/{projectID}/experiments/{experimentID}/eval-scores` | 按 experiment 聚合 scores |

## Runner 集成

Python runner 自动检测 evaluator config 中的 `multi_metrics` 字段：

1. 提交 `POST /runner/jobs/{jobID}/results` 时，在 body 中附加 `eval_scores` 数组
2. 后端在同一事务中创建 eval_case_result 和 eval_scores
3. 后端自动计算实验级聚合（按 metric_kind 分组）

## 前端展示

- **实验报告页**：顶部显示多维评测得分卡片（平均分、通过率、各维度分数）
- **Run Detail 页**：新增"评测分数"区域，按 metric_kind 分组展示
- 没有 eval_scores 时显示清晰空状态，不报错
