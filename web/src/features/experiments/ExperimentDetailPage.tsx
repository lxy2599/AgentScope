import { useCallback } from "react";
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
  Progress,
  Tooltip,
} from "antd";
import {
  CheckCircleOutlined,
  CloseCircleOutlined,
  EyeOutlined,
  LineChartOutlined,
  ThunderboltOutlined,
  QuestionCircleOutlined,
} from "@ant-design/icons";
import { useExperiment, useExperimentResults, useExperimentEvalScores } from "../../api/hooks";
import { StatusSummary } from "./StatusSummary";
import type { MetricKindAggregation } from "../../api/types";

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

export function ExperimentDetailPage() {
  const { experimentId } = useParams<{ experimentId: string }>();
  const { data: experiment, isLoading, error } = useExperiment(experimentId ?? "");

  const projectId = experiment?.project_id ?? "";
  const { data: results, isLoading: resultsLoading } = useExperimentResults(projectId, experimentId ?? "");
  const { data: evalAgg, isLoading: aggLoading } = useExperimentEvalScores(projectId, experimentId ?? "");

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

  const passedResults = results?.filter((r) => r.passed) ?? [];
  const failedResults = results?.filter((r) => !r.passed) ?? [];

  // ---- Execution stats (from status_counts) ----
  const succeededCount = experiment.status_counts?.succeeded ?? 0;
  const failedCount = experiment.status_counts?.failed ?? 0;
  const queuedCount = experiment.status_counts?.queued ?? 0;
  const executedCount = succeededCount + failedCount;

  // ---- Quality stats ----
  const qualityPassCount = passedResults.length;
  const qualityFailCount = failedResults.length;
  const avgScore = executedCount > 0
    ? (results?.reduce((sum, r) => sum + r.score, 0) ?? 0) / executedCount
    : 0;
  const qualityPassRate = executedCount > 0
    ? ((qualityPassCount / executedCount) * 100).toFixed(1)
    : "0";

  // ---- Metric aggregation display ----
  const hasEvalScores = evalAgg && evalAgg.length > 0;

  const resultColumns = [
    {
      title: "执行状态",
      dataIndex: "passed",
      key: "passed",
      width: 80,
      render: (passed: boolean) =>
        passed ? (
          <Tag icon={<CheckCircleOutlined />} color="success">
            通过
          </Tag>
        ) : (
          <Tag icon={<CloseCircleOutlined />} color="error">
            未通过
          </Tag>
        ),
    },
    {
      title: "综合分",
      dataIndex: "score",
      key: "score",
      width: 80,
      render: (score: number) => {
        const color = score >= 0.7 ? "#52c41a" : score >= 0.4 ? "#faad14" : "#ff4d4f";
        return <Text strong style={{ color }}>{score.toFixed(2)}</Text>;
      },
    },
    {
      title: "用例 ID",
      dataIndex: "dataset_case_id",
      key: "dataset_case_id",
      width: 100,
      ellipsis: true,
      render: (id: string) => (
        <Text code style={{ fontSize: 12 }}>
          {id ? id.slice(0, 8) : "—"}
        </Text>
      ),
    },
    {
      title: "评测说明",
      dataIndex: "reason",
      key: "reason",
      ellipsis: true,
    },
    {
      title: "实际输出",
      dataIndex: "actual_output",
      key: "actual_output",
      ellipsis: true,
      render: (val: string | undefined) => (
        <Text style={{ fontSize: 12 }}>{val || "—"}</Text>
      ),
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
      title: "Trace",
      key: "trace",
      width: 80,
      render: (_: unknown, record: { run_id?: string }) =>
        record.run_id ? (
          <Button
            type="link"
            size="small"
            icon={<EyeOutlined />}
            onClick={() => handleRunClick(record.run_id!)}
          >
            Trace
          </Button>
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
              ID：{experimentId?.slice(0, 8)}
            </Text>
          </div>
          <Space>{statusTag(experiment.status)}</Space>
        </div>
      </Card>

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
            <Statistic title="总用例" value={experiment.total_cases} suffix={`个`} />
          </Col>
          <Col span={5}>
            <Statistic
              title="已执行"
              value={executedCount}
              suffix={`/ ${experiment.total_cases}`}
              valueStyle={{ color: "#1677ff" }}
            />
          </Col>
          <Col span={5}>
            <Statistic
              title="执行成功"
              value={succeededCount}
              valueStyle={{ color: "#52c41a" }}
              prefix={<CheckCircleOutlined />}
            />
          </Col>
          <Col span={5}>
            <Statistic
              title="执行失败"
              value={failedCount}
              valueStyle={{ color: "#ff4d4f" }}
              prefix={<CloseCircleOutlined />}
            />
          </Col>
          <Col span={5}>
            <Statistic
              title="排队中"
              value={queuedCount}
              valueStyle={{ color: "#faad14" }}
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
            <Tooltip title="基于实际执行结果的质量评分。avg_score 为所有结果的平均综合分，pass_count 为评分≥0.7的结果数。">
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
                valueStyle={{
                  color: avgScore >= 0.7 ? "#52c41a" : avgScore >= 0.4 ? "#faad14" : "#ff4d4f",
                }}
              />
            </Card>
          </Col>
          <Col span={6}>
            <Card size="small">
              <Statistic
                title="质量通过"
                value={qualityPassCount}
                valueStyle={{ color: "#52c41a" }}
                prefix={<CheckCircleOutlined />}
              />
            </Card>
          </Col>
          <Col span={6}>
            <Card size="small">
              <Statistic
                title="质量未通过"
                value={qualityFailCount}
                valueStyle={{ color: "#ff4d4f" }}
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
                valueStyle={{
                  color: Number(qualityPassRate) >= 70 ? "#52c41a" : "#ff4d4f",
                }}
              />
            </Card>
          </Col>
        </Row>
      </Card>

      {/* Multi-dimensional Eval Scores */}
      <Card
        title={
          <Space>
            <LineChartOutlined />
            <span>维度聚合分</span>
          </Space>
        }
      >
        {aggLoading ? (
          <div style={{ textAlign: "center", padding: 32 }}>
            <Spin />
          </div>
        ) : hasEvalScores ? (
          <Row gutter={[16, 16]}>
            {evalAgg.map((agg: MetricKindAggregation) => {
              const passRate = agg.total_count > 0
                ? ((agg.passed_count / agg.total_count) * 100).toFixed(0)
                : "0";
              return (
                <Col span={6} key={agg.metric_kind}>
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
                    <Progress
                      percent={Number(passRate)}
                      size="small"
                      showInfo={false}
                      strokeColor={metricKindColor(agg.metric_kind)}
                    />
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
        ) : (
          <Empty
            description="暂无多维评测数据。配置多维 evaluator 并运行实验后，各维度得分会显示在这里。"
            image={Empty.PRESENTED_IMAGE_SIMPLE}
          />
        )}
      </Card>

      {/* Status Summary */}
      <Card title="执行状态概览">
        <StatusSummary detail={experiment} />
      </Card>

      {/* Basic Info */}
      <Card title="基本信息">
        <Descriptions column={2} size="small">
          <Descriptions.Item label="目标 Agent">
            {experiment.target_name}
          </Descriptions.Item>
          <Descriptions.Item label="版本">
            {experiment.target_version || "—"}
          </Descriptions.Item>
          <Descriptions.Item label="数据集 ID">
            <Text code style={{ fontSize: 12 }}>
              {experiment.dataset_id}
            </Text>
          </Descriptions.Item>
          <Descriptions.Item label="Evaluator ID">
            <Text code style={{ fontSize: 12 }}>
              {experiment.evaluator_id || "—"}
            </Text>
          </Descriptions.Item>
          <Descriptions.Item label="创建时间">
            {new Date(experiment.created_at).toLocaleString("zh-CN")}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      {/* Results table */}
      <Card title="评测结果明细">
        {resultsLoading ? (
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
            scroll={{ x: 800 }}
            rowClassName={(record) =>
              !record.passed ? "ant-table-row-error" : ""
            }
          />
        )}
      </Card>
    </Space>
  );
}
