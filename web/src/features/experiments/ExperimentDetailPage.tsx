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
} from "antd";
import {
  CheckCircleOutlined,
  CloseCircleOutlined,
  EyeOutlined,
  LineChartOutlined,
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
};

const METRIC_KIND_COLORS: Record<string, string> = {
  final_answer: "#1890ff",
  trajectory: "#722ed1",
  tool_correctness: "#13c2c2",
  argument_correctness: "#fa8c16",
  rag_groundedness: "#52c41a",
  safety: "#eb2f96",
  efficiency: "#faad14",
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

  // ---- Metric aggregation display ----
  const hasEvalScores = evalAgg && evalAgg.length > 0;

  const resultColumns = [
    {
      title: "结果",
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
      title: "分数",
      dataIndex: "score",
      key: "score",
      width: 80,
      render: (score: number) => (
        <Text code>{score.toFixed(2)}</Text>
      ),
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
      title: "原因",
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
      title: "成本",
      dataIndex: "cost",
      key: "cost",
      width: 80,
      render: (cost: number | undefined) =>
        cost != null ? cost.toFixed(6) : "—",
    },
    {
      title: "创建时间",
      dataIndex: "created_at",
      key: "created_at",
      width: 180,
      render: (date: string) => new Date(date).toLocaleString("zh-CN"),
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
          {statusTag(experiment.status)}
        </div>
      </Card>

      {/* Overall Metrics */}
      <Row gutter={16}>
        <Col span={6}>
          <Card>
            <Statistic title="总用例数" value={experiment.total_cases} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="通过数"
              value={passedResults.length}
              valueStyle={{ color: "#52c41a" }}
              prefix={<CheckCircleOutlined />}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="失败数"
              value={failedResults.length}
              valueStyle={{ color: "#ff4d4f" }}
              prefix={<CloseCircleOutlined />}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="通过率"
              value={
                experiment.total_cases > 0
                  ? ((passedResults.length / experiment.total_cases) * 100).toFixed(1)
                  : "0"
              }
              suffix="%"
              valueStyle={{ color: "#52c41a" }}
            />
          </Card>
        </Col>
      </Row>

      {/* Multi-dimensional Eval Scores */}
      <Card
        title={
          <Space>
            <LineChartOutlined />
            <span>多维评测得分</span>
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
      <Card title="状态概览">
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
          />
        )}
      </Card>
    </Space>
  );
}
