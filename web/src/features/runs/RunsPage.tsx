import { useParams, useNavigate } from "react-router-dom";
import { Card, Table, Tag, Button, Typography, Spin, Empty, Space } from "antd";
import { EyeOutlined } from "@ant-design/icons";
import { useProjectRuns } from "../../api/hooks";

const { Title, Text } = Typography;

export function RunsPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const { data: runs, isLoading } = useProjectRuns(projectId ?? "");
  const navigate = useNavigate();

  const statusTag = (status: string) => {
    const map: Record<string, { color: string; text: string }> = {
      pending: { color: "default", text: "待开始" },
      running: { color: "processing", text: "运行中" },
      completed: { color: "success", text: "已完成" },
      succeeded: { color: "success", text: "成功" },
      failed: { color: "error", text: "失败" },
      error: { color: "error", text: "错误" },
    };
    const s = map[status] || { color: "default", text: status };
    return <Tag color={s.color}>{s.text}</Tag>;
  };

  const columns = [
    {
      title: "Agent",
      dataIndex: "agent_name",
      key: "agent_name",
      render: (name: string, record: { id: string }) => (
        <Button type="link" style={{ padding: 0 }} onClick={() => navigate(`/runs/${record.id}`)}>
          {name}
        </Button>
      ),
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      render: (s: string) => statusTag(s),
    },
    {
      title: "来源",
      dataIndex: "source",
      key: "source",
    },
    {
      title: "Trace ID",
      dataIndex: "trace_id",
      key: "trace_id",
      ellipsis: true,
      render: (id: string) => (
        <Text code style={{ fontSize: 12 }}>
          {id}
        </Text>
      ),
    },
    {
      title: "延迟",
      dataIndex: "latency_ms",
      key: "latency_ms",
      render: (ms: number | undefined) =>
        ms != null ? `${ms}ms` : "—",
    },
    {
      title: "成本",
      dataIndex: "cost",
      key: "cost",
      render: (cost: number | undefined) =>
        cost != null ? cost.toFixed(6) : "—",
    },
    {
      title: "创建时间",
      dataIndex: "created_at",
      key: "created_at",
      render: (date: string) => new Date(date).toLocaleString("zh-CN"),
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
          onClick={() => navigate(`/runs/${record.id}`)}
        >
          查看诊断
        </Button>
      ),
    },
  ];

  if (isLoading) {
    return (
      <div style={{ textAlign: "center", padding: 64 }}>
        <Spin size="large" />
      </div>
    );
  }

  return (
    <Space orientation="vertical" size="large" style={{ width: "100%" }}>
      <Title level={3} style={{ margin: 0 }}>
        运行追踪
      </Title>

      {runs && runs.length > 0 ? (
        <Card>
          <Table
            dataSource={runs}
            columns={columns}
            rowKey="id"
            pagination={false}
          />
        </Card>
      ) : (
        <Card>
          <Empty
            description="暂无运行记录"
            image={Empty.PRESENTED_IMAGE_SIMPLE}
          />
        </Card>
      )}
    </Space>
  );
}
