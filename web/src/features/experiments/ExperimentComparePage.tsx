import { useParams, useNavigate } from "react-router-dom";
import {
  Card,
  Descriptions,
  Table,
  Tag,
  Typography,
  Spin,
  Empty,
  Button,
  Row,
  Col,
  Statistic,
  Alert,
  Space,
  Tooltip,
  message,
  Modal,
  Select,
  Grid,
} from "antd";
import {
  ArrowUpOutlined,
  ArrowDownOutlined,
  MinusOutlined,
  WarningOutlined,
  EyeOutlined,
  SendOutlined,
} from "@ant-design/icons";
import { useComparison, useCreateAnnotationItemFromRun, useAnnotationQueues } from "../../api/hooks";
import type { ComparisonCaseResult } from "../../api/types";
import { RunSnapshotDiffPanel } from "./RunSnapshotDiffPanel";
import { useState } from "react";

const { Title, Text } = Typography;

function formatPct(v: number): string {
  return `${(v * 100).toFixed(1)}%`;
}

function formatDelta(v: number): string {
  return v > 0 ? `+${v.toFixed(4)}` : v.toFixed(4);
}

function formatCostDelta(v: number): string {
  return v > 0 ? `+${v.toFixed(2)}%` : `${v.toFixed(2)}%`;
}

function StatusTag({ status }: { status: string }) {
  switch (status) {
    case "improved":
      return (
        <Tag icon={<ArrowUpOutlined />} color="success">
          改善
        </Tag>
      );
    case "regressed":
      return (
        <Tag icon={<ArrowDownOutlined />} color="error">
          回归
        </Tag>
      );
    case "unchanged":
      return (
        <Tag icon={<MinusOutlined />} color="default">
          不变
        </Tag>
      );
    case "incomparable":
      return <Tag color="warning">不可比较</Tag>;
    default:
      return <Tag>{status}</Tag>;
  }
}

function AnnotationButton({ projectId, runId, label, onDone }: { projectId: string; runId: string; label: string; onDone: () => void }) {
  const [queueModalOpen, setQueueModalOpen] = useState(false);
  const [selectedQueueId, setSelectedQueueId] = useState<string | undefined>(undefined);
  const { data: queues } = useAnnotationQueues(projectId);
  const createAnnotationItem = useCreateAnnotationItemFromRun(projectId);

  const handleAddToQueue = async () => {
    try {
      await createAnnotationItem.mutateAsync({
        runId,
        queueId: selectedQueueId,
      });
      message.success(`已加入标注队列`);
      setQueueModalOpen(false);
      onDone();
    } catch (err: unknown) {
      message.error(err instanceof Error ? err.message : "加入标注队列失败");
    }
  };

  return (
    <>
      <Tooltip title="加入标注队列">
        <Button
          type="link"
          size="small"
          icon={<SendOutlined />}
          loading={createAnnotationItem.isPending}
          onClick={() => setQueueModalOpen(true)}
        >
          {label}
        </Button>
      </Tooltip>
      <Modal
        title="加入标注队列"
        open={queueModalOpen}
        onCancel={() => setQueueModalOpen(false)}
        onOk={handleAddToQueue}
        confirmLoading={createAnnotationItem.isPending}
        okText="确认加入"
        cancelText="取消"
      >
        <div style={{ marginBottom: 12 }}>
          <Text>选择标注队列（可选）：</Text>
        </div>
        <Select
          style={{ width: "100%" }}
          placeholder="选择队列（留空则加入默认队列）"
          allowClear
          value={selectedQueueId}
          onChange={setSelectedQueueId}
          options={(queues || []).map((q) => ({
            value: q.id,
            label: q.name,
          }))}
        />
        {(!queues || queues.length === 0) && (
          <div style={{ marginTop: 8 }}>
            <Text type="secondary">暂无可用标注队列，将使用默认队列。</Text>
          </div>
        )}
      </Modal>
    </>
  );
}

export function ExperimentComparePage() {
  const { comparisonId } = useParams<{ comparisonId: string }>();
  const navigate = useNavigate();
  const screens = Grid.useBreakpoint();
  const isMobile = screens.md === false;
  const [recentAnnotated, setRecentAnnotated] = useState<Set<string>>(new Set());
  const { data, isLoading, error } = useComparison(comparisonId ?? "");

  if (isLoading) {
    return (
      <div style={{ textAlign: "center", padding: 80 }}>
        <Spin size="large" />
      </div>
    );
  }

  if (error || !data) {
    return (
      <div style={{ padding: 24 }}>
        <Empty description="加载对比数据失败" />
      </div>
    );
  }

  const { baseline_experiment, candidate_experiment, comparison_summary, case_results, project_id } = data;

  // ---- Summary cards ----
  const qualityImproved =
    comparison_summary.avg_score_delta > 0
      ? "提升"
      : comparison_summary.avg_score_delta < 0
        ? "下降"
        : "持平";

  // ---- Multi-dim metrics derived from comparison_summary ----
  const multiDimMetrics = comparison_summary.multi_dim_metrics;
  const multiDimEntries = multiDimMetrics ? Object.entries(multiDimMetrics) : [];
  const significance = comparison_summary.statistical_significance;
  const significanceLabel =
    significance?.conclusion === "significant_regression"
      ? "显著回归"
      : significance?.conclusion === "no_significant_change"
        ? "无显著变化"
        : significance?.conclusion === "insufficient_sample"
          ? "样本不足"
          : "未计算";

  // ---- Annotation state ----
  const snapshotPair = case_results.find(
    (item) => item.baseline_run_id && item.candidate_run_id,
  );

  return (
    <div
      style={{
        width: "100%",
        maxWidth: 1400,
        minWidth: 0,
        margin: "0 auto",
        padding: isMobile ? 0 : 24,
        boxSizing: "border-box",
      }}
    >
      <Title level={3}>
        <Space>
          <span>实验对比报告</span>
          <Tag color={data.comparison_type === "model_swap" ? "blue" : data.comparison_type === "ablation" ? "purple" : "default"}>
            {data.comparison_type === "model_swap" ? "模型替换" : data.comparison_type === "ablation" ? "消融实验" : "通用对比"}
          </Tag>
        </Space>
      </Title>

      {/* Baseline / Candidate info */}
      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col xs={24} md={12}>
          <Card title="Baseline 实验" size="small">
            <Descriptions
              column={1}
              size="small"
              items={[
                {
                  key: "name",
                  label: "名称",
                  children: <>{baseline_experiment.target_name} v{baseline_experiment.target_version}</>,
                },
                { key: "status", label: "状态", children: baseline_experiment.status },
                {
                  key: "model",
                  label: "模型",
                  children: <>{baseline_experiment.model_provider} / {baseline_experiment.model_name}</>,
                },
                { key: "harness", label: "Harness 版本", children: baseline_experiment.harness_version || "-" },
                { key: "prompt", label: "Prompt 版本", children: baseline_experiment.prompt_version || "-" },
                { key: "dataset", label: "数据集", children: baseline_experiment.dataset_version || "-" },
              ]}
            />
          </Card>
        </Col>
        <Col xs={24} md={12}>
          <Card title="Candidate 实验" size="small">
            <Descriptions
              column={1}
              size="small"
              items={[
                {
                  key: "name",
                  label: "名称",
                  children: <>{candidate_experiment.target_name} v{candidate_experiment.target_version}</>,
                },
                { key: "status", label: "状态", children: candidate_experiment.status },
                {
                  key: "model",
                  label: "模型",
                  children: <>{candidate_experiment.model_provider} / {candidate_experiment.model_name}</>,
                },
                { key: "harness", label: "Harness 版本", children: candidate_experiment.harness_version || "-" },
                { key: "prompt", label: "Prompt 版本", children: candidate_experiment.prompt_version || "-" },
                { key: "dataset", label: "数据集", children: candidate_experiment.dataset_version || "-" },
              ]}
            />
          </Card>
        </Col>
      </Row>

      {snapshotPair?.baseline_run_id && snapshotPair.candidate_run_id && (
        <RunSnapshotDiffPanel
          baselineRunId={snapshotPair.baseline_run_id}
          candidateRunId={snapshotPair.candidate_run_id}
        />
      )}

      {/* Regression gate alert */}
      {comparison_summary.regression_triggered && (
        <Alert
          type="error"
          showIcon
          icon={<WarningOutlined />}
          title="回归检测触发"
          description={
            <ul style={{ margin: 0 }}>
              {(comparison_summary.regression_reasons || []).map((r, i) => (
                <li key={i}>{r}</li>
              ))}
            </ul>
          }
          style={{ marginBottom: 16 }}
        />
      )}

      {/* Quality summary */}
      <Card title="质量对比" style={{ marginBottom: 16 }}>
        <Row gutter={24}>
          <Col xs={24} sm={12} lg={6}>
            <Statistic
              title="通过率"
              value={formatPct(comparison_summary.baseline_pass_rate)}
              suffix={
                <Text type={qualityImproved === "提升" ? "success" : qualityImproved === "下降" ? "danger" : undefined}>
                  → {formatPct(comparison_summary.candidate_pass_rate)}
                </Text>
              }
            />
          </Col>
          <Col xs={24} sm={12} lg={6}>
            <Statistic
              title="综合分"
              value={comparison_summary.baseline_avg_score.toFixed(4)}
              suffix={
                <Text type={comparison_summary.avg_score_delta > 0 ? "success" : comparison_summary.avg_score_delta < 0 ? "danger" : undefined}>
                  → {comparison_summary.candidate_avg_score.toFixed(4)} ({formatDelta(comparison_summary.avg_score_delta)})
                </Text>
              }
            />
          </Col>
          <Col xs={24} sm={12} lg={6}>
            <Statistic
              title="总 Cases / 配对"
              value={`${comparison_summary.total_cases} / ${comparison_summary.paired_cases}`}
            />
          </Col>
          <Col xs={24} sm={12} lg={6}>
            <Statistic
              title="变化分布"
              valueRender={() => (
                <Space>
                  <Text type="success">{comparison_summary.improved_cases} 改善</Text>
                  <Text type="danger">{comparison_summary.regressed_cases} 回归</Text>
                  <Text>{comparison_summary.unchanged_cases} 不变</Text>
                </Space>
              )}
            />
          </Col>
        </Row>
      </Card>

      <Card
        title="统计显著性"
        style={{ marginBottom: 16 }}
        extra={
          <Tag color={significance?.conclusion === "significant_regression" ? "error" : significance?.conclusion === "no_significant_change" ? "success" : "warning"}>
            {significanceLabel}
          </Tag>
        }
      >
        <Space style={{ marginBottom: 12 }} wrap>
          <Text>配对样本 {significance?.sample_size ?? 0}</Text>
          <Text>Bootstrap {significance?.iterations ?? 0} 次</Text>
          <Text>随机种子 {significance?.seed ?? "-"}</Text>
        </Space>
        <Table
          size="small"
          pagination={false}
          scroll={{ x: 600 }}
          rowKey="metric"
          dataSource={Object.entries(significance?.metrics || {}).map(([metric, values]) => ({
            metric,
            ...values,
          }))}
          columns={[
            { title: "指标", dataIndex: "metric" },
            { title: "平均差", dataIndex: "mean_delta", render: (value: number) => formatDelta(value) },
            {
              title: "95% 置信区间",
              key: "interval",
              render: (_: unknown, record: { ci_lower: number; ci_upper: number }) =>
                `[${record.ci_lower.toFixed(4)}, ${record.ci_upper.toFixed(4)}]`,
            },
            { title: "近似 p 值", dataIndex: "p_value", render: (value: number) => value.toFixed(4) },
          ]}
        />
      </Card>

      {/* Multi-dim metric comparison */}
      {multiDimEntries.length > 0 && (
        <Card title="多维度指标对比" style={{ marginBottom: 16 }}>
          <Table
            dataSource={multiDimEntries.map(([key, m]) => ({ key, ...m }))}
            rowKey="key"
            pagination={false}
            size="small"
            scroll={{ x: 790 }}
            columns={[
              {
                title: "指标名称",
                dataIndex: "metric_name",
                key: "name",
                width: 180,
              },
              {
                title: "维度分类",
                dataIndex: "metric_kind",
                key: "kind",
                width: 160,
              },
              {
                title: "Baseline 均值",
                dataIndex: "baseline_avg",
                key: "bl",
                width: 150,
                render: (v: number) => v.toFixed(4),
              },
              {
                title: "Candidate 均值",
                dataIndex: "candidate_avg",
                key: "cand",
                width: 150,
                render: (v: number) => v.toFixed(4),
              },
              {
                title: "差值",
                dataIndex: "delta",
                key: "delta",
                width: 150,
                render: (v: number) => (
                  <Text type={v > 0 ? "success" : v < 0 ? "danger" : undefined}>
                    {formatDelta(v)}
                  </Text>
                ),
              },
            ]}
          />
        </Card>
      )}

      {/* Cost / Latency */}
      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col xs={24} md={8}>
          <Card title="成本" size="small">
            <Statistic
              title="平均成本"
              value={comparison_summary.baseline_avg_cost.toFixed(4)}
              suffix={
                <Text type={comparison_summary.cost_delta_pct > 10 ? "danger" : "secondary"}>
                  → {comparison_summary.candidate_avg_cost.toFixed(4)} ({formatCostDelta(comparison_summary.cost_delta_pct)})
                </Text>
              }
            />
          </Card>
        </Col>
        <Col xs={24} md={8}>
          <Card title="延迟 (P95)" size="small">
            <Statistic
              title="P95 延迟"
              value={comparison_summary.baseline_p95_latency_ms}
              suffix={
                <Text type={comparison_summary.latency_delta_pct > 20 ? "danger" : "secondary"}>
                  ms → {comparison_summary.candidate_p95_latency_ms}ms ({formatCostDelta(comparison_summary.latency_delta_pct)})
                </Text>
              }
            />
          </Card>
        </Col>
        <Col xs={24} md={8}>
          <Card title="错误率" size="small">
            <Statistic
              title="执行错误率"
              value={formatPct(comparison_summary.baseline_error_rate)}
              suffix={
                <Text type={comparison_summary.candidate_error_rate > comparison_summary.baseline_error_rate ? "danger" : "secondary"}>
                  → {formatPct(comparison_summary.candidate_error_rate)}
                </Text>
              }
            />
          </Card>
        </Col>
      </Row>

      {/* Case results table */}
      <Card title="Case 级对比">
        <Table<ComparisonCaseResult>
          dataSource={case_results}
          rowKey="id"
          pagination={false}
          size="small"
          scroll={{ x: 930 }}
          columns={[
            {
              title: "Case ID",
              dataIndex: "dataset_case_id",
              key: "case",
              width: 100,
              ellipsis: true,
              render: (id: string) => <Text copyable={{ text: id }}>{id.substring(0, 8)}</Text>,
            },
            {
              title: "状态",
              dataIndex: "status",
              key: "status",
              width: 100,
              render: (s: string) => <StatusTag status={s} />,
            },
            {
              title: "Baseline 分",
              dataIndex: "baseline_score",
              key: "bl_score",
              width: 100,
              render: (v: number, r: ComparisonCaseResult) => (
                <Text type={r.baseline_passed ? "success" : "danger"}>{v.toFixed(4)}</Text>
              ),
            },
            {
              title: "Candidate 分",
              dataIndex: "candidate_score",
              key: "cand_score",
              width: 100,
              render: (v: number, r: ComparisonCaseResult) => (
                <Text type={r.candidate_passed ? "success" : "danger"}>{v.toFixed(4)}</Text>
              ),
            },
            {
              title: "分差",
              dataIndex: "score_diff",
              key: "diff",
              width: 90,
              render: (v: number) => (
                <Text type={v > 0 ? "success" : v < 0 ? "danger" : undefined}>
                  {formatDelta(v)}
                </Text>
              ),
            },
            {
              title: "BL 延迟",
              dataIndex: "baseline_latency_ms",
              key: "bl_lat",
              width: 80,
              render: (v: number) => v ? `${v}ms` : "-",
            },
            {
              title: "Cand 延迟",
              dataIndex: "candidate_latency_ms",
              key: "cand_lat",
              width: 80,
              render: (v: number) => v ? `${v}ms` : "-",
            },
            {
              title: "操作",
              key: "actions",
              width: 180,
              render: (_: unknown, r: ComparisonCaseResult) => (
                <Space>
                  {r.baseline_run_id && (
                    <Tooltip title="查看 Baseline Run">
                      <Button
                        type="link"
                        size="small"
                        icon={<EyeOutlined />}
                        onClick={() => navigate(`/runs/${r.baseline_run_id}`)}
                      />
                    </Tooltip>
                  )}
                  {r.candidate_run_id && (
                    <Tooltip title="查看 Candidate Run">
                      <Button
                        type="link"
                        size="small"
                        icon={<EyeOutlined />}
                        onClick={() => navigate(`/runs/${r.candidate_run_id}`)}
                      />
                    </Tooltip>
                  )}
                  {r.candidate_run_id && r.status === "regressed" && !recentAnnotated.has(r.id) && (
                    <AnnotationButton
                      projectId={project_id}
                      runId={r.candidate_run_id}
                      label="加入标注队列"
                      onDone={() => setRecentAnnotated((prev) => new Set(prev).add(r.id))}
                    />
                  )}
                  {recentAnnotated.has(r.id) && (
                    <Text type="success" style={{ fontSize: 12 }}>已加入</Text>
                  )}
                </Space>
              ),
            },
          ]}
        />
      </Card>
    </div>
  );
}
