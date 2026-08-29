import { useCallback, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  Card,
  Descriptions,
  Table,
  Tag,
  Typography,
  Spin,
  Empty,
  Button,
  Space,
  Row,
  Col,
  Statistic,
  Tooltip,
  Alert,
  Modal,
} from "antd";
import {
  CheckCircleOutlined,
  CloseCircleOutlined,
  EyeOutlined,
  LineChartOutlined,
  ThunderboltOutlined,
  QuestionCircleOutlined,
  InfoCircleOutlined,
  WarningOutlined,
  ExperimentOutlined,
} from "@ant-design/icons";
import {
  useDatasets,
  useEvaluators,
  useExperiment,
  useExperimentReport,
  useJudgeEvaluation,
  useMetricDefinitions,
} from "../../api/hooks";
import { StatusSummary } from "./StatusSummary";
import { CompareCreateModal } from "./CompareCreateModal";
import type { MetricKindAggregation, MetricDefinitionConfig, EvalScore } from "../../api/types";
import { TechnicalId } from "../../components/ui/TechnicalId";
import { displayJsonValue, evaluatorSummary } from "../../utils/display";

const { Title, Text } = Typography;

// ---- Display helpers ----

const METRIC_KIND_LABELS: Record<string, string> = {
  final_answer: "最终答案",
  trajectory: "轨迹",
  tool_correctness: "工具调用",
  argument_correctness: "论据正确性",
  rag_groundedness: "RAG 相关性",
  safety: "安全",
  efficiency: "效率",
  helpfulness: "有帮助性",
};

const METRIC_KIND_COLORS: Record<string, string> = {
  final_answer: "#1890ff",
  trajectory: "#722ed1",
  tool_correctness: "#13c2c2",
  argument_correctness: "#fa8c16",
  rag_groundedness: "#52c41a",
  safety: "#eb2f96",
  efficiency: "#faad14",
  helpfulness: "#1677ff",
};

function metricKindLabel(kind: string): string {
  return METRIC_KIND_LABELS[kind] || kind;
}

function metricKindColor(kind: string): string {
  return METRIC_KIND_COLORS[kind] || "#999";
}

/**
 * Extract dimension_scores from a result's eval_scores.
 * Returns a mapping of metric_name → metric_kind → dimension_scores.
 */
function extractEvalScoreDimensions(
  evalScores: EvalScore[] | undefined,
): Record<string, number> {
  if (!evalScores || evalScores.length === 0) return {};
  const result: Record<string, number> = {};
  for (const es of evalScores) {
    // Use metric_name as the dimension key for display
    result[es.metric_name || es.metric_kind] = es.score;
    // Also parse any dimension_scores from details
    if (es.details) {
      try {
        const detail = JSON.parse(es.details);
        if (detail.dimension_scores && typeof detail.dimension_scores === "object") {
          const dimScores = detail.dimension_scores as Record<string, number>;
          for (const [dim, score] of Object.entries(dimScores)) {
            result[`${es.metric_name}.${dim}`] = score;
          }
        }
      } catch {
        // ignore
      }
    }
  }
  return result;
}

/** Check if any eval_score has veto_triggered=true in its details. */
function hasVetoTriggered(
  evalScores: EvalScore[] | undefined,
): boolean {
  if (!evalScores) return false;
  for (const es of evalScores) {
    if (es.details) {
      try {
        const detail = JSON.parse(es.details);
        if (detail.veto_triggered) return true;
      } catch {
        // ignore
      }
    }
  }
  return false;
}

/** Get veto reason from eval_scores details. Re-exported for use in the UI. */
function getVetoReason(
  evalScores: EvalScore[] | undefined,
): string {
  if (!evalScores) return "";
  for (const es of evalScores) {
    if (es.details) {
      try {
        const detail = JSON.parse(es.details);
        if (detail.veto_reason) return detail.veto_reason;
      } catch {
        // ignore
      }
    }
  }
  return "";
}

function resultEvaluationStatus(record: { evaluation_status?: string; details?: string }): string {
  if (record.evaluation_status) return record.evaluation_status;
  if (!record.details) return "evaluated";
  try {
    const parsed = JSON.parse(record.details) as { evaluation_status?: string };
    return parsed.evaluation_status || "evaluated";
  } catch {
    return "evaluated";
  }
}

export function ExperimentDetailPage() {
  const { experimentId } = useParams<{ experimentId: string }>();
  const { data: experiment, isLoading, error } = useExperiment(experimentId ?? "");

  const projectId = experiment?.project_id ?? "";
  const { data: report, isLoading: reportLoading } = useExperimentReport(projectId, experimentId ?? "");
  const { data: metricDefs } = useMetricDefinitions(projectId);
  const { data: datasets = [] } = useDatasets(projectId);
  const { data: evaluators = [] } = useEvaluators(projectId);
  const [judgeRunId, setJudgeRunId] = useState("");
  const [compareModalOpen, setCompareModalOpen] = useState(false);
  const judgeEvaluation = useJudgeEvaluation(projectId, judgeRunId);

  // Extract data from report (primary) with fallback to experiment for backward compat
  const results = report?.results;
  const evalAgg = report?.dimension_aggregation;
  const hasEvalScores = evalAgg && evalAgg.length > 0;
  const passedResults = results?.filter((r) => r.passed) ?? [];
  const failedResults = results?.filter((r) => !r.passed) ?? [];
  const dataset = datasets.find((item) => item.id === experiment?.dataset_id);
  const evaluator = (Array.isArray(evaluators) ? evaluators : []).find(
    (item: any) => item.id === experiment?.evaluator_id,
  );

  // ---- Execution stats: prefer report.execution, fallback to status_counts ----
  const exec = report?.execution;
  const succeededCount = exec?.succeeded ?? experiment?.status_counts?.succeeded ?? 0;
  const failedCount = exec?.failed ?? experiment?.status_counts?.failed ?? 0;
  const queuedCount = exec?.queued ?? experiment?.status_counts?.queued ?? 0;
  const executedCount = succeededCount + failedCount;

  const navigate = useNavigate();

  const handleRunClick = useCallback(
    (runId: string) => {
      navigate(`/runs/${runId}`);
    },
    [navigate],
  );

  const statusTag = (status: string) => {
    const map: Record<string, { color: string; text: string }> = {
      pending: { color: "default", text: "待开始" },
      running: { color: "processing", text: "运行中" },
      completed: { color: "success", text: "已完成" },
      failed: { color: "error", text: "失败" },
      queued: { color: "warning", text: "排队中" },
    };
    const s = map[status] || { color: "default", text: status };
    return <Tag color={s.color}>{s.text}</Tag>;
  };

  if (isLoading) {
    return (
      <div style={{ textAlign: "center", padding: 64 }}>
        <Spin size="large" />
      </div>
    );
  }

  if (error) {
    return (
      <Card>
        <Text type="danger">{error instanceof Error ? error.message : "加载实验失败"}</Text>
      </Card>
    );
  }

  if (!experiment) {
    return (
      <Card>
        <Empty description="未找到该实验" image={Empty.PRESENTED_IMAGE_SIMPLE} />
      </Card>
    );
  }

  // ---- Quality stats from report ----
  const qualityPassCount = report?.quality?.pass_count ?? passedResults.length;
  const qualityFailCount = report?.quality?.fail_count ?? failedResults.length;
  const avgScore = report?.quality?.avg_score ??
    (executedCount > 0 ? (results?.reduce((sum, r) => sum + r.score, 0) ?? 0) / executedCount : 0);
  const qualityPassRate = executedCount > 0
    ? ((qualityPassCount / executedCount) * 100).toFixed(1)
    : "0";

  // ---- Metric aggregation display ----

  // ---- Collect per-case dimension data from eval_scores ----
  const perCaseDimensions: Record<string, Record<string, number>> = {};
  const vetoCaseIds = new Set<string>();
  if (results) {
    for (const r of results) {
      if (r.eval_scores && r.eval_scores.length > 0) {
        const dimScores = extractEvalScoreDimensions(r.eval_scores);
        if (Object.keys(dimScores).length > 0) {
          perCaseDimensions[r.dataset_case_id || r.id] = dimScores;
        }
        if (hasVetoTriggered(r.eval_scores)) {
          vetoCaseIds.add(r.dataset_case_id || r.id);
        }
      }
    }
  }

  // ---- Rubric config from metric definitions ----
  const rubricMetrics = metricDefs?.filter((m) => {
    if (!m.config) return false;
    try {
      const cfg = JSON.parse(m.config) as MetricDefinitionConfig;
      return cfg.rubric !== undefined && cfg.rubric !== null && (cfg.rubric.dimensions?.length ?? 0) > 0;
    } catch {
      return false;
    }
  }) ?? [];

  const hasRubricData = Object.keys(perCaseDimensions).length > 0 || rubricMetrics.length > 0;

  // ---- Calculate weighted composite across dimensions ----
  const dimensionAverages: Record<string, { avg: number; count: number }> = {};
  const vetoCount = vetoCaseIds.size;
  // Collect veto reasons from eval_scores
  const vetoReasonsList: string[] = [];
  if (results) {
    for (const r of results) {
      if (r.eval_scores) {
        const reason = getVetoReason(r.eval_scores);
        if (reason) vetoReasonsList.push(reason);
      }
    }
  }
  const vetoDescription = vetoReasonsList.length > 0
    ? `否决原因：${vetoReasonsList.join("；")}`
    : "否决项指标未通过时，即使其他维度分数较高，整体评测结果仍为不通过。请检查否决项配置和对应 run trace。";

  for (const scores of Object.values(perCaseDimensions)) {
    for (const [dim, score] of Object.entries(scores)) {
      if (!dimensionAverages[dim]) {
        dimensionAverages[dim] = { avg: 0, count: 0 };
      }
      dimensionAverages[dim].avg += score;
      dimensionAverages[dim].count += 1;
    }
  }
  for (const dim of Object.keys(dimensionAverages)) {
    const d = dimensionAverages[dim];
    dimensionAverages[dim] = { avg: d.avg / d.count, count: d.count };
  }

  // ---- Results table columns ----
  const resultColumns = [
    {
      title: "评测结果",
      dataIndex: "passed",
      key: "passed",
      width: 80,
      render: (passed: boolean, record: { details?: string; evaluation_status?: string; eval_scores?: EvalScore[] }) => {
        const evaluationStatus = resultEvaluationStatus(record);
        if (evaluationStatus === "not_evaluated") {
          return <Tag icon={<QuestionCircleOutlined />} color="default">未评测</Tag>;
        }
        const vetoed = hasVetoTriggered(record.eval_scores);
        if (vetoed) {
          return (
            <Tooltip title="否决项触发">
              <Tag icon={<WarningOutlined />} color="error">
                否决
              </Tag>
            </Tooltip>
          );
        }
        return passed ? (
          <Tag icon={<CheckCircleOutlined />} color="success">
            通过
          </Tag>
        ) : (
          <Tag icon={<CloseCircleOutlined />} color="error">
            未通过
          </Tag>
        );
      },
    },
    {
      title: "用例",
      key: "case",
      width: 280,
      render: (_: unknown, record: { dataset_case_id: string; case_input?: string; case_expected?: string }) => (
        <Space orientation="vertical" size={2} style={{ width: "100%" }}>
          <Text strong ellipsis={{ tooltip: displayJsonValue(record.case_input) }}>
            {displayJsonValue(record.case_input)}
          </Text>
          <Text type="secondary" ellipsis={{ tooltip: displayJsonValue(record.case_expected) }}>
            期望：{displayJsonValue(record.case_expected)}
          </Text>
          <TechnicalId value={record.dataset_case_id} />
        </Space>
      ),
    },
    {
      title: "实际输出",
      dataIndex: "actual_output",
      key: "actual_output",
      width: 260,
      ellipsis: true,
      render: (value?: string) => value || <Text type="secondary">—</Text>,
    },
    {
      title: "综合分",
      dataIndex: "score",
      key: "score",
      width: 80,
      render: (score: number, record: { passed: boolean; evaluation_status?: string; details?: string; eval_scores?: EvalScore[] }) => {
        if (resultEvaluationStatus(record) === "not_evaluated") return <Text type="secondary">—</Text>;
        const vetoed = hasVetoTriggered(record.eval_scores);
        const color = vetoed ? "#ff4d4f" : record.passed ? "#52c41a" : "#ff4d4f";
        return <Text strong style={{ color }}>{score.toFixed(2)}</Text>;
      },
    },
    {
      title: "维度分",
      key: "dimensions",
      width: 200,
      render: (_: unknown, record: { dataset_case_id?: string; id: string; eval_scores?: EvalScore[] }) => {
        if (!record.eval_scores || record.eval_scores.length === 0) {
          return <Text type="secondary">—</Text>;
        }
        return (
          <Space size={4} wrap>
            {record.eval_scores.map((es) => (
              <Tag key={es.metric_name} style={{ fontSize: 11 }}>
                {es.metric_name || es.metric_kind}: {es.score.toFixed(2)}
              </Tag>
            ))}
          </Space>
        );
      },
    },
    {
      title: "评测说明",
      dataIndex: "reason",
      key: "reason",
      ellipsis: true,
    },
    {
      title: "延迟",
      dataIndex: "latency_ms",
      key: "latency_ms",
      width: 80,
      render: (ms: number | undefined) =>
        ms != null ? `${ms}ms` : "—",
    },
    {
      title: "审计",
      key: "trace",
      width: 160,
      render: (_: unknown, record: { run_id?: string }) =>
        record.run_id ? (
          <Space size={0}>
            <Button
              type="link"
              size="small"
              icon={<EyeOutlined />}
              onClick={() => handleRunClick(record.run_id!)}
            >
              Trace
            </Button>
            <Button type="link" size="small" onClick={() => setJudgeRunId(record.run_id!)}>
              Judge
            </Button>
          </Space>
        ) : (
          <Text type="secondary">—</Text>
        ),
    },
  ];

  return (
    <Space orientation="vertical" size="large" style={{ width: "100%" }}>
      {/* Header */}
      <Card>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start" }}>
          <div>
            <Title level={3} style={{ margin: 0 }}>
              实验报告
            </Title>
            <Text type="secondary" style={{ fontSize: 12 }}>
              技术 ID：<TechnicalId value={experimentId} />
            </Text>
          </div>
          <Space>
            {experiment.status === "completed" && (
              <Button
                aria-label="创建对比"
                icon={<LineChartOutlined />}
                onClick={() => setCompareModalOpen(true)}
              >
                创建对比
              </Button>
            )}
            {statusTag(experiment.status)}
          </Space>
        </div>
      </Card>

      <Card title="评测快照" size="small">
        {experiment.evaluation_snapshot_hash ? (
          <Descriptions size="small" column={2}>
            <Descriptions.Item label="Snapshot ID">
              <TechnicalId value={experiment.evaluation_snapshot_id || "—"} />
            </Descriptions.Item>
            <Descriptions.Item label="配置 Hash">
              <Text code>{experiment.evaluation_snapshot_hash}</Text>
            </Descriptions.Item>
            <Descriptions.Item label="说明" span={2}>
              <Text type="secondary">本实验的 Dataset、Evaluator、Rubric、Grader 和 Harness 配置在创建时冻结，后续配置修改不会影响历史 Job。</Text>
            </Descriptions.Item>
          </Descriptions>
        ) : (
          <Alert type="warning" showIcon message="该实验没有评测快照" description="这是旧实验或快照迁移前创建的实验，执行时可能依赖可变配置。" />
        )}
      </Card>

      <CompareCreateModal
        experimentId={experiment.id}
        projectId={projectId}
        visible={compareModalOpen}
        onClose={() => setCompareModalOpen(false)}
      />

      {/* Execution Stats */}
      <Card
        title={
          <Space>
            <ThunderboltOutlined />
            <span>执行统计</span>
          </Space>
        }
        size="small"
      >
        <Row gutter={16}>
          <Col span={4}>
            <Statistic title="总用例" value={experiment.total_cases} suffix="个" />
          </Col>
          <Col span={5}>
            <Statistic
              title="已执行"
              value={executedCount}
              suffix={`/ ${experiment.total_cases}`}
              styles={{ content: { color: "#1677ff" } }}
            />
          </Col>
          <Col span={5}>
            <Statistic
              title="执行成功"
              value={succeededCount}
              styles={{ content: { color: "#52c41a" } }}
              prefix={<CheckCircleOutlined />}
            />
          </Col>
          <Col span={5}>
            <Statistic
              title="执行失败"
              value={failedCount}
              styles={{ content: { color: "#ff4d4f" } }}
              prefix={<CloseCircleOutlined />}
            />
          </Col>
          <Col span={5}>
            <Statistic
              title="排队中"
              value={queuedCount}
              styles={{ content: { color: "#faad14" } }}
            />
          </Col>
        </Row>
        <div style={{ marginTop: 8 }}>
          <Text type="secondary" style={{ fontSize: 12 }}>
            执行成功不等于质量通过。请参考"质量统计"了解各维度质量情况。
          </Text>
        </div>
      </Card>

      {/* Quality Stats */}
      <Card
        title={
          <Space>
            <CheckCircleOutlined />
            <span>质量统计</span>
            <Tooltip title="基于实际执行结果的质量评分。avg_score 为所有结果的平均综合分，pass_count 为评分达到阈值的已评估结果数。包含维度分数和否决项统计。">
              <QuestionCircleOutlined style={{ color: "#999" }} />
            </Tooltip>
          </Space>
        }
        size="small"
      >
        <Row gutter={16}>
          <Col span={6}>
            <Card size="small">
              <Statistic
                title="平均综合分"
                value={executedCount > 0 ? (avgScore * 100).toFixed(1) : "—"}
                suffix={executedCount > 0 ? "分" : ""}
                styles={{
                  content: {
                    color: qualityPassCount > qualityFailCount ? "#52c41a" : "#ff4d4f",
                  },
                }}
              />
            </Card>
          </Col>
          <Col span={6}>
            <Card size="small">
              <Statistic
                title="质量通过"
                value={qualityPassCount}
                styles={{ content: { color: "#52c41a" } }}
                prefix={<CheckCircleOutlined />}
              />
            </Card>
          </Col>
          <Col span={6}>
            <Card size="small">
              <Statistic
                title="质量未通过"
                value={qualityFailCount}
                styles={{ content: { color: "#ff4d4f" } }}
                prefix={<CloseCircleOutlined />}
              />
            </Card>
          </Col>
          <Col span={6}>
            <Card size="small">
              <Statistic
                title="质量通过率"
                value={qualityPassRate}
                suffix="%"
                styles={{
                  content: {
                    color: Number(qualityPassRate) >= 50 ? "#52c41a" : "#ff4d4f",
                  },
                }}
              />
            </Card>
          </Col>
        </Row>
      </Card>

      {/* Veto Alert */}
      {vetoCount > 0 && (
        <Alert
          title={`否决项触发：${vetoCount} 个用例因否决项被标记为不通过`}
          description={vetoDescription}
          type="error"
          showIcon
          icon={<WarningOutlined />}
        />
      )}

      {/* Multi-dimensional Eval Scores */}
      <Card
        title={
          <Space>
            <LineChartOutlined />
            <span>维度聚合分</span>
            <Tooltip title="各维度分数来自实验配置的多维评测指标。综合分 = 各启用维度分 × 权重的加权平均。权重和阈值由项目中的 Metric Definition 定义。">
              <QuestionCircleOutlined style={{ color: "#999" }} />
            </Tooltip>
          </Space>
        }
      >
        {reportLoading ? (
          <div style={{ textAlign: "center", padding: 32 }}>
            <Spin />
          </div>
        ) : hasEvalScores ? (
          <>
            <Row gutter={[16, 16]}>
              {evalAgg.map((agg: MetricKindAggregation) => {
                return (
                  <Col xs={24} sm={12} md={8} lg={6} key={agg.metric_kind}>
                    <Card
                      size="small"
                      style={{ borderTop: `3px solid ${metricKindColor(agg.metric_kind)}` }}
                    >
                      <div style={{ textAlign: "center" }}>
                        <Text strong style={{ fontSize: 13 }}>
                          {metricKindLabel(agg.metric_kind)}
                        </Text>
                      </div>
                      <div style={{ textAlign: "center", margin: "8px 0" }}>
                        <Text style={{ fontSize: 24, fontWeight: 600, color: metricKindColor(agg.metric_kind) }}>
                          {(agg.avg_score * 100).toFixed(0)}
                        </Text>
                        <Text type="secondary"> 分</Text>
                      </div>
                      <div style={{ textAlign: "center", marginTop: 4 }}>
                        <Text type="secondary" style={{ fontSize: 11 }}>
                          {agg.passed_count}/{agg.total_count} 通过
                        </Text>
                      </div>
                    </Card>
                  </Col>
                );
              })}
            </Row>
            <Alert
              title="综合分说明"
              description="综合分 = 各启用维度分 × 权重的加权平均。维度的权重和阈值在项目的 Metric Definition 中配置，用户可以根据评测需求自定义。"
              type="info"
              showIcon
              icon={<InfoCircleOutlined />}
              style={{ marginTop: 16 }}
            />
          </>
        ) : (
          <Empty
            description="暂无多维评测数据。配置多维 evaluator 并运行实验后，各维度得分会显示在这里。"
            image={Empty.PRESENTED_IMAGE_SIMPLE}
          />
        )}
      </Card>

      {/* Rubric Dimension Breakdown */}
      {hasRubricData && (
        <Card
          title={
            <Space>
              <ExperimentOutlined />
              <span>Rubric 维度详细分</span>
              <Tooltip title="以下展示每个评测维度在各个用例上的得分分布，以及平均值。数据来自 LLM Judge 返回的 dimension_scores。">
                <QuestionCircleOutlined style={{ color: "#999" }} />
              </Tooltip>
            </Space>
          }
          size="small"
        >
          {Object.keys(dimensionAverages).length > 0 ? (
            <Row gutter={[16, 16]}>
              {Object.entries(dimensionAverages).map(([dim, info]) => {
                const dimColor = METRIC_KIND_COLORS[dim] || "#1677ff";
                return (
                  <Col xs={24} sm={12} md={8} lg={6} key={dim}>
                    <Card
                      size="small"
                      style={{ borderTop: `3px solid ${dimColor}` }}
                    >
                      <div style={{ textAlign: "center" }}>
                        <Text strong>{dim}</Text>
                      </div>
                      <div style={{ textAlign: "center", margin: "8px 0" }}>
                        <Text style={{ fontSize: 22, fontWeight: 600, color: dimColor }}>
                          {(info.avg * 100).toFixed(0)}
                        </Text>
                        <Text type="secondary"> 分</Text>
                      </div>
                      <div style={{ textAlign: "center" }}>
                        <Text type="secondary" style={{ fontSize: 11 }}>
                          {info.count} 个用例评估
                        </Text>
                      </div>
                    </Card>
                  </Col>
                );
              })}
            </Row>
          ) : (
            <Text type="secondary">暂无 Rubric 维度数据。配置 Rubric 评测指标并运行实验后，维度分数将显示在此处。</Text>
          )}
        </Card>
      )}

      {/* Status Summary */}
      <Card title="执行状态概览">
        <StatusSummary detail={experiment} />
      </Card>

      {/* Basic Info */}
      <Card title="基本信息">
        <Descriptions
          column={2}
          size="small"
          items={[
            { key: "target", label: "目标 Agent", children: experiment.target_name },
            { key: "version", label: "版本", children: experiment.target_version || "—" },
            {
              key: "dataset",
              label: "数据集",
              children: (
                <Space>
                  <Text>{dataset?.name || "未知数据集"}</Text>
                  <TechnicalId value={experiment.dataset_id} />
                </Space>
              ),
            },
            {
              key: "evaluator",
              label: "评测方式",
              children: experiment.evaluator_id ? (
                <Space orientation="vertical" size={0}>
                  <Text>{evaluator?.name || "Evaluator"} · {evaluatorSummary(evaluator?.config)}</Text>
                  <TechnicalId value={experiment.evaluator_id} />
                </Space>
              ) : "仅记录执行状态",
            },
            {
              key: "created-at",
              label: "创建时间",
              children: new Date(experiment.created_at).toLocaleString("zh-CN"),
            },
          ]}
        />
      </Card>

      {/* Results table */}
      <Card title="评测结果明细">
        {reportLoading ? (
          <div style={{ textAlign: "center", padding: 32 }}>
            <Spin />
          </div>
        ) : !results || results.length === 0 ? (
          <Empty
            description="暂无评测结果，Runner 提交结果后会显示在这里。"
            image={Empty.PRESENTED_IMAGE_SIMPLE}
          />
        ) : (
          <Table
            dataSource={results}
            columns={resultColumns}
            rowKey="id"
            pagination={false}
            size="small"
            scroll={{ x: 1380 }}
            rowClassName={(record) =>
              !record.passed ? "ant-table-row-error" : ""
            }
          />
        )}
      </Card>

      <Modal
        title="Judge 投票与聚合审计"
        open={!!judgeRunId}
        width={920}
        footer={null}
        onCancel={() => setJudgeRunId("")}
      >
        {judgeEvaluation.isLoading ? <Spin /> : (
          <Space orientation="vertical" style={{ width: "100%" }}>
            {(judgeEvaluation.data?.aggregations ?? []).map((item) => (
              <Alert
                key={item.id}
                type={item.result.veto_triggered ? "error" : item.result.low_confidence ? "warning" : "info"}
                showIcon
                message={`${item.metric_name} · ${item.result.method} · ${item.result.score.toFixed(3)}`}
                description={
                  `有效票 ${item.result.valid_vote_count}/${item.result.required_votes}，` +
                  `错误票 ${item.result.error_vote_count}，标准差 ${item.result.stddev.toFixed(3)}，` +
                  `分歧度 ${item.result.disagreement.toFixed(3)}，置信度 ${item.result.confidence.toFixed(3)}` +
                  (item.result.veto_reason ? `，否决：${item.result.veto_reason}` : "")
                }
              />
            ))}
            <Table
              size="small"
              pagination={false}
              rowKey="id"
              dataSource={judgeEvaluation.data?.votes ?? []}
              columns={[
                { title: "Provider / Model", render: (_, row) => `${row.provider} / ${row.model}` },
                { title: "分数", dataIndex: "score", render: (value) => value == null ? "错误票" : value.toFixed(3) },
                { title: "结论", render: (_, row) => row.error || row.reason || "—" },
                { title: "延迟", dataIndex: "latency_ms", render: (value) => value == null ? "—" : `${value}ms` },
              ]}
            />
          </Space>
        )}
      </Modal>
    </Space>
  );
}
