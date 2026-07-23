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
} from "antd";
import {
  ArrowUpOutlined,
  ArrowDownOutlined,
  MinusOutlined,
  WarningOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  EyeOutlined,
} from "@ant-design/icons";
import { useComparison } from "../../api/hooks";
import type { ComparisonCaseResult, ComparisonSummary } from "../../api/types";

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

export function ExperimentComparePage() {
  const { comparisonId } = useParams<{ comparisonId: string }>();
  const navigate = useNavigate();
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

  const { baseline_experiment, candidate_experiment, comparison_summary, case_results } = data;

  // ---- Summary cards ----
  const qualityImproved =
    comparison_summary.avg_score_delta > 0
      ? "提升"
      : comparison_summary.avg_score_delta < 0
        ? "下降"
        : "持平";

  return (
    <div style={{ padding: 24, maxWidth: 1400, margin: "0 auto" }}>
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
        <Col span={12}>
          <Card title="Baseline 实验" size="small">
            <Descriptions column={1} size="small">
              <Descriptions.Item label="名称">{baseline_experiment.target_name} v{baseline_experiment.target_version}</Descriptions.Item>
              <Descriptions.Item label="状态">{baseline_experiment.status}</Descriptions.Item>
              <Descriptions.Item label="模型">{baseline_experiment.model_provider} / {baseline_experiment.model_name}</Descriptions.Item>
              <Descriptions.Item label="Harness 版本">{baseline_experiment.harness_version || "-"}</Descriptions.Item>
              <Descriptions.Item label="Prompt 版本">{baseline_experiment.prompt_version || "-"}</Descriptions.Item>
              <Descriptions.Item label="数据集">{baseline_experiment.dataset_version || "-"}</Descriptions.Item>
            </Descriptions>
          </Card>
        </Col>
        <Col span={12}>
          <Card title="Candidate 实验" size="small">
            <Descriptions column={1} size="small">
              <Descriptions.Item label="名称">{candidate_experiment.target_name} v{candidate_experiment.target_version}</Descriptions.Item>
              <Descriptions.Item label="状态">{candidate_experiment.status}</Descriptions.Item>
              <Descriptions.Item label="模型">{candidate_experiment.model_provider} / {candidate_experiment.model_name}</Descriptions.Item>
              <Descriptions.Item label="Harness 版本">{candidate_experiment.harness_version || "-"}</Descriptions.Item>
              <Descriptions.Item label="Prompt 版本">{candidate_experiment.prompt_version || "-"}</Descriptions.Item>
              <Descriptions.Item label="数据集">{candidate_experiment.dataset_version || "-"}</Descriptions.Item>
            </Descriptions>
          </Card>
        </Col>
      </Row>

      {/* Regression gate alert */}
      {comparison_summary.regression_triggered && (
        <Alert
          type="error"
          showIcon
          icon={<WarningOutlined />}
          message="回归检测触发"
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
          <Col span={6}>
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
          <Col span={6}>
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
          <Col span={6}>
            <Statistic
              title="总 Cases / 配对"
              value={`${comparison_summary.total_cases} / ${comparison_summary.paired_cases}`}
            />
          </Col>
          <Col span={6}>
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

      {/* Cost / Latency */}
      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={8}>
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
        <Col span={8}>
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
        <Col span={8}>
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
              width: 120,
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
                </Space>
              ),
            },
          ]}
        />
      </Card>
    </div>
  );
}
