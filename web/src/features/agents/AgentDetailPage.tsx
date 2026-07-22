import { useParams, useNavigate, Link } from "react-router-dom";
import {
  Card, Descriptions, Table, Tag, Typography, Badge, Space, Spin, Button,
  Empty,
} from "antd";
import { ArrowLeftOutlined, RobotOutlined } from "@ant-design/icons";
import { useAgentDetail } from "../../api/hooks";
import type { AgentCapabilityInfo, AgentRuntimeMetadata, CapabilityMetadata } from "../../api/types";

const { Title, Text } = Typography;

function agentMeta(meta?: AgentRuntimeMetadata): AgentRuntimeMetadata {
  return (meta ?? {}) as AgentRuntimeMetadata;
}

function capMeta(m?: CapabilityMetadata): CapabilityMetadata {
  return (m ?? {}) as CapabilityMetadata;
}

function sideEffectColor(risk: string): "success" | "warning" | "error" | "default" {
  switch (risk) {
    case "low": return "success";
    case "medium": return "warning";
    case "high": return "error";
    default: return "default";
  }
}

export function AgentDetailPage() {
  const { agentId } = useParams<{ agentId: string }>();
  const navigate = useNavigate();
  const { data: detail, isLoading } = useAgentDetail(agentId ?? "");

  if (isLoading) {
    return (
      <div style={{ textAlign: "center", padding: 48 }}>
        <Spin size="large" />
        <div style={{ marginTop: 16, color: "#999" }}>加载中...</div>
      </div>
    );
  }

  if (!detail) {
    return (
      <Card>
        <Empty description="Agent 未找到" />
      </Card>
    );
  }

  const meta = agentMeta(detail.metadata);

  const statusColor = (status: string) => {
    switch (status) {
      case "active": return "green";
      case "inactive": return "default";
      case "deprecated": return "orange";
      default: return "default";
    }
  };

  const instanceColumns = [
    {
      title: "实例 ID",
      dataIndex: "instance_id",
      key: "instance_id",
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      render: (status: string) => (
        <Badge status={status === "online" ? "success" : "default"} text={status} />
      ),
    },
    {
      title: "主机名",
      dataIndex: "hostname",
      key: "hostname",
    },
    {
      title: "PID",
      dataIndex: "pid",
      key: "pid",
      render: (pid: number) => pid > 0 ? pid : "—",
    },
    {
      title: "上次心跳",
      dataIndex: "last_heartbeat_at",
      key: "last_heartbeat_at",
      render: (date: string) =>
        date ? new Date(date).toLocaleString("zh-CN") : "—",
    },
    {
      title: "启动时间",
      dataIndex: "started_at",
      key: "started_at",
      render: (date: string) =>
        date ? new Date(date).toLocaleString("zh-CN") : "—",
    },
  ];

  const capabilityColumns = [
    {
      title: "名称",
      dataIndex: "name",
      key: "name",
    },
    {
      title: "类型",
      key: "kind",
      render: (_: unknown, record: AgentCapabilityInfo) => {
        const m = capMeta(record.metadata);
        return m.kind ? <Tag color="blue">{m.kind}</Tag> : "—";
      },
    },
    {
      title: "版本",
      key: "version",
      render: (_: unknown, record: AgentCapabilityInfo) => {
        const m = capMeta(record.metadata);
        return m.version || "—";
      },
    },
    {
      title: "风险等级",
      key: "risk",
      render: (_: unknown, record: AgentCapabilityInfo) => {
        const m = capMeta(record.metadata);
        return m.side_effect_risk ? (
          <Badge status={sideEffectColor(m.side_effect_risk)} text={m.side_effect_risk} />
        ) : "—";
      },
    },
    {
      title: "描述",
      dataIndex: "description",
      key: "description",
      render: (desc: string) => desc || "—",
    },
    {
      title: "注册时间",
      dataIndex: "created_at",
      key: "created_at",
      render: (date: string) =>
        date ? new Date(date).toLocaleString("zh-CN") : "—",
    },
  ];

  return (
    <Space direction="vertical" size="large" style={{ width: "100%" }}>
      <Button
        type="link"
        icon={<ArrowLeftOutlined />}
        style={{ padding: 0 }}
        onClick={() => navigate(-1)}
      >
        返回
      </Button>

      <Card>
        <div style={{ display: "flex", alignItems: "center", gap: 12, marginBottom: 24 }}>
          <RobotOutlined style={{ fontSize: 32, color: "#1677ff" }} />
          <div>
            <Title level={4} style={{ margin: 0 }}>{detail.name}</Title>
            <Text type="secondary">{detail.description || "无描述"}</Text>
          </div>
          <div style={{ marginLeft: "auto" }}>
            <Space>
              <Badge status={statusColor(detail.status) as "success" | "default" | "warning"} text={detail.status} />
              <Tag>v{detail.version}</Tag>
            </Space>
          </div>
        </div>

        <Descriptions column={2} size="small" bordered>
          <Descriptions.Item label="Agent ID">{detail.id}</Descriptions.Item>
          <Descriptions.Item label="项目 ID">{detail.project_id}</Descriptions.Item>
          <Descriptions.Item label="框架">{meta.framework || "—"}</Descriptions.Item>
          <Descriptions.Item label="运行时">{meta.runtime || "—"}</Descriptions.Item>
          <Descriptions.Item label="来源">{meta.source || "—"}</Descriptions.Item>
          <Descriptions.Item label="环境">{meta.environment || "—"}</Descriptions.Item>
          <Descriptions.Item label="Agent 版本">
            {meta.agent_version ? <Tag>{meta.agent_version}</Tag> : <Tag>v{detail.version}</Tag>}
          </Descriptions.Item>
          <Descriptions.Item label="合约版本">{meta.contract_version || "—"}</Descriptions.Item>
          <Descriptions.Item label="创建时间">
            {detail.created_at ? new Date(detail.created_at).toLocaleString("zh-CN") : "—"}
          </Descriptions.Item>
          <Descriptions.Item label="更新时间">
            {detail.updated_at ? new Date(detail.updated_at).toLocaleString("zh-CN") : "—"}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Card title="在线运行">
        <Space direction="vertical" style={{ width: "100%" }}>
          <Text>
            查看该 Agent 的当前运行状态和最近运行记录。
          </Text>
          <Link to={`/projects/${detail.project_id}/active-runs?agent_id=${detail.id}`}>
            <Button type="primary" size="small">查看在线运行</Button>
          </Link>
        </Space>
      </Card>

      <Card title="运行实例">
        {detail.instances && detail.instances.length > 0 ? (
          <Table
            dataSource={detail.instances}
            columns={instanceColumns}
            rowKey="id"
            pagination={false}
            size="small"
          />
        ) : (
          <Empty description="暂无运行实例" image={Empty.PRESENTED_IMAGE_SIMPLE} />
        )}
      </Card>

      <Card title="能力 (Capabilities)">
        {detail.capabilities && detail.capabilities.length > 0 ? (
          <Table
            dataSource={detail.capabilities}
            columns={capabilityColumns}
            rowKey="id"
            pagination={false}
            size="small"
          />
        ) : (
          <Empty description="暂无注册的能力" image={Empty.PRESENTED_IMAGE_SIMPLE} />
        )}
      </Card>
    </Space>
  );
}
