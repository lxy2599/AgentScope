import { useState } from "react";
import { Card, Col, Row, Statistic, Table, Segmented, Typography, Spin, Alert, Tag } from "antd";
import {
  CloudServerOutlined,
  WarningOutlined,
  CheckCircleOutlined,
  DollarOutlined,
} from "@ant-design/icons";
import { useParams } from "react-router-dom";
import { useProjectIngestAgentStats, useProjectIngestHealthEvents, useProjectIngestStats } from "../../api/hooks";
import type { IngestAgentStatsItem, IngestHealthEventItem } from "../../api/types";
import QuotaPanel from "./QuotaPanel";

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
  const { data: healthEvents, isLoading: eventsLoading } = useProjectIngestHealthEvents(projectId!, minutes);

  if (!projectId) {
    return <Alert message="缺少项目 ID" type="error" showIcon />;
  }

  if (isLoading) {
    return <Spin size="large" style={{ display: "block", margin: "100px auto" }} />;
  }

  const healthLabels = {
    healthy: ["采集正常", "success"], no_traffic: ["暂无采集流量", "default"],
    degraded: ["存在拒绝请求", "warning"], storage_error: ["存在存储失败", "error"], unavailable: ["采集后端不可用", "error"],
  } as const;
  const health = stats?.status ?? "unavailable";

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

      <Alert style={{ marginBottom: 16 }} type={healthLabels[health][1] as "success" | "warning" | "error" | "info"} showIcon
        message={healthLabels[health][0]}
        description={health === "unavailable" ? "ClickHouse 采集统计暂不可用，请检查后端连接与迁移状态。" : "仅展示采集元数据，不包含模型输入输出或工具内容。"} />

      {health === "unavailable" ? (
        <Card>
          <Alert
            message="采集健康统计暂不可用"
            description="请检查 ClickHouse 连接和采集健康迁移。"
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
            <Col xs={12} sm={8} md={4}><Card><Statistic title="成功请求" value={stats?.accepted ?? 0} /></Card></Col>
            <Col xs={12} sm={8} md={4}><Card><Statistic title="拒绝请求" value={stats?.rejected ?? 0} valueStyle={{ color: (stats?.rejected ?? 0) > 0 ? "#faad14" : undefined }} /></Card></Col>
            <Col xs={12} sm={8} md={4}><Card><Statistic title="身份字段缺失" value={(stats?.missing_run_id ?? 0) + (stats?.missing_trace_id ?? 0)} valueStyle={{ color: "#ff4d4f" }} /></Card></Col>
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
          <Card title="最近采集事件" style={{ marginBottom: 24 }}>
            <Table<IngestHealthEventItem> dataSource={healthEvents ?? []} loading={eventsLoading} rowKey={(item) => `${item.event_time}-${item.outcome}-${item.agent_name}`} pagination={{ pageSize: 10, hideOnSinglePage: true }}
              columns={[
                { title: "时间", dataIndex: "event_time", render: (v: string) => new Date(v).toLocaleString() },
                { title: "Agent", dataIndex: "agent_name", render: (v: string) => v || "unknown" },
                { title: "结果", dataIndex: "outcome", render: (v: string) => <Tag color={v === "accepted" ? "success" : "error"}>{v}</Tag> },
                { title: "HTTP", dataIndex: "http_status" }, { title: "Span", dataIndex: "span_count" },
                { title: "诊断", dataIndex: "error_code", render: (v?: string) => v || "正常写入" },
              ]} />
          </Card>
          <Card title="SDK 导出背压" style={{ marginBottom: 24 }}>
            <Row gutter={[16, 16]}>
              <Col xs={12} md={5}>
                <Statistic
                  title="队列深度"
                  value={stats?.sdk_queue_depth ?? "未知"}
                  suffix={stats?.sdk_queue_capacity != null ? `/ ${stats.sdk_queue_capacity}` : undefined}
                />
              </Col>
              <Col xs={12} md={5}>
                <Statistic title="累计丢弃 Span" value={stats?.sdk_spans_dropped ?? "未知"} />
              </Col>
              <Col xs={12} md={5}>
                <Statistic title="累计导出重试" value={stats?.sdk_export_retries ?? "未知"} />
              </Col>
              <Col xs={12} md={5}>
                <Statistic
                  title="最近导出延迟"
                  value={stats?.sdk_export_latency_ms != null ? Math.round(stats.sdk_export_latency_ms) : "未知"}
                  suffix={stats?.sdk_export_latency_ms != null ? "ms" : undefined}
                />
              </Col>
            </Row>
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
              数据保留 30 天
            </Text>
          </Col>
          <Col span={8}>
            <Text type="secondary">
              项目 ID: {projectId}
            </Text>
          </Col>
        </Row>
      </Card>
      <QuotaPanel projectId={projectId} />
    </div>
  );
}
