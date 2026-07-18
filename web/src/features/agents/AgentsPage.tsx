import { useNavigate, useParams } from "react-router-dom";
import { Card, Table, Button, Typography, Badge, Space, Spin, Empty, Tag } from "antd";
import { EyeOutlined, RobotOutlined } from "@ant-design/icons";
import { useAgents } from "../../api/hooks";

const { Title } = Typography;

export function AgentsPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const navigate = useNavigate();
  const { data: agents, isLoading } = useAgents(projectId ?? "");

  const statusColor = (status: string) => {
    switch (status) {
      case "active": return "green";
      case "inactive": return "default";
      case "deprecated": return "orange";
      default: return "default";
    }
  };

  const columns = [
    {
      title: "Agent 名称",
      dataIndex: "name",
      key: "name",
      render: (name: string, record: { id: string }) => (
        <Button
          type="link"
          style={{ padding: 0 }}
          onClick={() => navigate(`/projects/${projectId}/agents/${record.id}`)}
        >
          <Space>
            <RobotOutlined />
            {name}
          </Space>
        </Button>
      ),
    },
    {
      title: "描述",
      dataIndex: "description",
      key: "description",
      render: (desc: string) => desc || "—",
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      render: (status: string) => (
        <Badge status={statusColor(status) as "success" | "default" | "warning"} text={status} />
      ),
    },
    {
      title: "版本",
      dataIndex: "version",
      key: "version",
      render: (v: number) => <Tag>v{v}</Tag>,
    },
    {
      title: "创建时间",
      dataIndex: "created_at",
      key: "created_at",
      render: (date: string) =>
        date ? new Date(date).toLocaleDateString("zh-CN") : "—",
    },
    {
      title: "操作",
      key: "action",
      render: (_: unknown, record: { id: string }) => (
        <Button
          type="primary"
          ghost
          size="small"
          icon={<EyeOutlined />}
          onClick={() => navigate(`/projects/${projectId}/agents/${record.id}`)}
        >
          查看详情
        </Button>
      ),
    },
  ];

  return (
    <Space orientation="vertical" size="large" style={{ width: "100%" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <Title level={3} style={{ margin: 0 }}>
          Agent 管理
        </Title>
      </div>

      {isLoading ? (
        <div style={{ textAlign: "center", padding: 48 }}>
          <Spin size="large" />
          <div style={{ marginTop: 16, color: "#999" }}>加载中...</div>
        </div>
      ) : agents && agents.length > 0 ? (
        <Card>
          <Table
            dataSource={agents}
            columns={columns}
            rowKey="id"
            pagination={false}
            onRow={(record) => ({
              onClick: () => navigate(`/projects/${projectId}/agents/${record.id}`),
              style: { cursor: "pointer" },
            })}
          />
        </Card>
      ) : (
        <Card>
          <Empty
            description="暂无已注册的 Agent"
            image={Empty.PRESENTED_IMAGE_SIMPLE}
          >
            <span style={{ color: "#999" }}>
              Agent 通过 SDK 运行后会自动注册
            </span>
          </Empty>
        </Card>
      )}
    </Space>
  );
}
