import { Alert, Button, Card, Popconfirm, Space, Table, Tag, Typography } from "antd";
import { DeleteOutlined, LaptopOutlined, SafetyCertificateOutlined } from "@ant-design/icons";
import { useBrowserSessions, useRevokeBrowserSession } from "../../api/hooks";
import type { BrowserSession } from "../../api/types";
import { ErrorState } from "../../components/ui/ErrorState";

const { Paragraph, Text, Title } = Typography;

function formatTime(value?: string) {
  return value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "尚未使用";
}

/** User-facing device list. It intentionally exposes only coarse device/network metadata. */
export function SessionSettingsPage() {
  const { data: sessions = [], isLoading, error, refetch } = useBrowserSessions();
  const revoke = useRevokeBrowserSession();

  if (error) return <ErrorState title="无法读取登录设备" error={error} onRetry={() => void refetch()} />;

  return (
    <div className="page-stack">
      <div>
        <Space size={8}><SafetyCertificateOutlined /><Text type="secondary">账户安全</Text></Space>
        <Title level={2} style={{ margin: "8px 0 4px" }}>登录设备</Title>
        <Paragraph type="secondary" style={{ maxWidth: 680, margin: 0 }}>
          Cookie 和会话密钥不会显示或保存在浏览器页面中。移除设备后，该设备的下一次请求将立即失效。
        </Paragraph>
      </div>
      <Alert type="info" showIcon message="当前设备不能在这里移除" description="如需退出当前设备，请使用右上角“退出登录”；退出后服务端会撤销当前会话。" />
      <Card>
        <Table<BrowserSession>
          rowKey="id"
          loading={isLoading}
          dataSource={sessions}
          pagination={false}
          scroll={{ x: 760 }}
          locale={{ emptyText: "没有可显示的登录会话" }}
          columns={[
            { title: "设备", key: "device", render: (_, item) => <Space direction="vertical" size={2}><Space><LaptopOutlined /><Text strong>{item.user_agent_summary || "未知设备"}</Text>{item.current && <Tag color="blue">当前设备</Tag>}</Space><Text type="secondary">网络：{item.ip_prefix || "未记录"}</Text></Space> },
            { title: "创建时间", dataIndex: "created_at", render: formatTime },
            { title: "最近使用", dataIndex: "last_used_at", render: formatTime },
            { title: "到期时间", dataIndex: "expires_at", render: formatTime },
            { title: "操作", key: "actions", fixed: "right", render: (_, item) => item.current ? <Text type="secondary">请使用退出登录</Text> : <Popconfirm title="移除此登录设备？" description="该设备的服务端会话会立即失效。" okText="移除设备" cancelText="取消" onConfirm={() => revoke.mutate(item.id)}><Button danger size="small" icon={<DeleteOutlined />} loading={revoke.isPending}>移除</Button></Popconfirm> },
          ]}
        />
      </Card>
    </div>
  );
}
