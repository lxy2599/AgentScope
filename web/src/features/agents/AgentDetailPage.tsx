import { useParams, useNavigate } from "react-router-dom";
import {
  Card, Descriptions, Table, Tag, Typography, Badge, Space, Spin, Button,
  Empty,
} from "antd";
import { ArrowLeftOutlined, RobotOutlined } from "@ant-design/icons";
import { useAgentDetail } from "../../api/hooks";

const { Title, Text } = Typography;

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
          <Descriptions.Item label="创建时间">
            {detail.created_at ? new Date(detail.created_at).toLocaleString("zh-CN") : "—"}
          </Descriptions.Item>
          <Descriptions.Item label="更新时间">
            {detail.updated_at ? new Date(detail.updated_at).toLocaleString("zh-CN") : "—"}
          </Descriptions.Item>
        </Descriptions>
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
