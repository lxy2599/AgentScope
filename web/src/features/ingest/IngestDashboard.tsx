import { useState } from "react";
import { Card, Col, Row, Statistic, Table, Segmented, Typography, Spin, Alert } from "antd";
import {
  CloudServerOutlined,
  WarningOutlined,
  CheckCircleOutlined,
  DollarOutlined,
} from "@ant-design/icons";
import { useParams } from "react-router-dom";
import { useProjectIngestAgentStats, useProjectIngestStats } from "../../api/hooks";
import type { IngestAgentStatsItem } from "../../api/types";

const { Title, Text } = Typography;

const MINUTES_OPTIONS = [
  { value: 5, label: "最近 5 分钟" },
  { value: 15, label: "最近 15 分钟" },
  { value: 60, label: "最近 1 小时" },
];

export default function IngestDashboard() {
  const { projectId } = useParams<{ projectId: string }>();
  const [minutes, setMinutes] = useState<number>(15);

  const { data: stats, isLoading, error } = useProjectIngestStats(projectId!, minutes);
  const { data: agentStats, isLoading: agentLoading } = useProjectIngestAgentStats(projectId!, minutes);

  if (!projectId) {
    return <Alert message="缺少项目 ID" type="error" showIcon />;
  }

  if (isLoading) {
    return <Spin size="large" style={{ display: "block", margin: "100px auto" }} />;
  }

  const isPlaceholder = stats?.message;

  const agentColumns = [
    {
      title: "Agent",
      dataIndex: "agent_name",
      key: "agent_name",
      render: (v: string) => v || "(unknown)",
    },
    {
      title: "运行次数",
      dataIndex: "run_count",
      key: "run_count",
      sorter: (a: IngestAgentStatsItem, b: IngestAgentStatsItem) => a.run_count - b.run_count,
    },
    {
      title: "错误数",
      dataIndex: "error_count",
      key: "error_count",
      render: (v: number) => (
        <span style={{ color: v > 0 ? "#ff4d4f" : undefined }}>
          {v}
        </span>
      ),
      sorter: (a: IngestAgentStatsItem, b: IngestAgentStatsItem) => a.error_count - b.error_count,
    },
    {
      title: "错误率",
      key: "error_rate",
      render: (_: unknown, record: IngestAgentStatsItem) => {
        if (record.run_count === 0) return "-";
        const rate = ((record.error_count / record.run_count) * 100).toFixed(1);
        return <span style={{ color: record.error_count > 0 ? "#ff4d4f" : "#52c41a" }}>{rate}%</span>;
      },
    },
    {
      title: "P95 延迟 (ms)",
      dataIndex: "p95_latency",
      key: "p95_latency",
      render: (v: number) => (v ? Math.round(v).toLocaleString() : "-"),
      sorter: (a: IngestAgentStatsItem, b: IngestAgentStatsItem) => a.p95_latency - b.p95_latency,
    },
  ];

  return (
    <div>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 24 }}>
        <Title level={4} style={{ margin: 0 }}>
          <CloudServerOutlined style={{ marginRight: 8 }} />
          采集健康
        </Title>
        <Segmented
          options={MINUTES_OPTIONS}
          value={minutes}
          onChange={(v) => setMinutes(v as number)}
        />
      </div>

      {error && (
        <Alert message="获取采集数据失败" description={(error as Error).message} type="error" showIcon style={{ marginBottom: 16 }} />
      )}

      {isPlaceholder ? (
        <Card>
          <Alert
            message="采集健康统计需要 ClickHouse 后端连接"
            description="聚合查询需要 ClickHouse tracestore 配置。部署配置好 ClickHouse 后，此页面将展示实时采集指标。"
            type="info"
            showIcon
          />
        </Card>
      ) : (
        <>
          <Row gutter={[16, 16]} style={{ marginBottom: 24 }}>
            <Col xs={12} sm={8} md={4}>
              <Card>
                <Statistic
                  title="Span 总数"
                  value={stats?.spans ?? 0}
                  prefix={<CloudServerOutlined />}
                  valueStyle={{ color: "#1677ff" }}
                />
              </Card>
            </Col>
            <Col xs={12} sm={8} md={4}>
              <Card>
                <Statistic
                  title="事件数"
                  value={stats?.events ?? 0}
                  prefix={<CloudServerOutlined />}
                />
              </Card>
            </Col>
            <Col xs={12} sm={8} md={4}>
              <Card>
                <Statistic
                  title="错误数"
                  value={stats?.errors ?? 0}
                  prefix={<WarningOutlined />}
                  valueStyle={{ color: (stats?.errors ?? 0) > 0 ? "#ff4d4f" : "#52c41a" }}
                />
              </Card>
            </Col>
            <Col xs={12} sm={8} md={4}>
              <Card>
                <Statistic
                  title="LLM 调用"
                  value={stats?.llms ?? 0}
                  prefix={<CheckCircleOutlined />}
                />
              </Card>
            </Col>
            <Col xs={12} sm={8} md={4}>
              <Card>
                <Statistic
                  title="工具调用"
                  value={stats?.tools ?? 0}
                />
              </Card>
            </Col>
            <Col xs={12} sm={8} md={4}>
              <Card>
                <Statistic
                  title="Token 总数"
                  value={stats?.tokens ?? 0}
                  suffix={stats?.tokens != null ? "t" : undefined}
                />
              </Card>
            </Col>
          </Row>

          <Card title="Agent 维度错误统计" style={{ marginBottom: 24 }}>
            <Table
              dataSource={agentStats ?? []}
              columns={agentColumns}
              rowKey="agent_name"
              loading={agentLoading}
              pagination={{ pageSize: 10, hideOnSinglePage: true }}
              locale={{ emptyText: "暂无数据" }}
            />
          </Card>
        </>
      )}

      <Card title="限流与写入状态" style={{ marginBottom: 24 }}>
        <Row gutter={[16, 16]}>
          <Col span={8}>
            <Statistic
              title="写入模式"
              value={stats ? "批量写入" : "等待数据"}
              prefix={<DollarOutlined />}
            />
          </Col>
          <Col span={8}>
            <Text type="secondary">
              数据保留 {stats?.message ? "(等待配置)" : "30 天"}
            </Text>
          </Col>
          <Col span={8}>
            <Text type="secondary">
              项目 ID: {projectId}
            </Text>
          </Col>
        </Row>
      </Card>
    </div>
  );
}
