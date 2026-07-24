import { Alert, Descriptions, Empty, Spin, Tag, Typography } from "antd";
import { ApiError } from "../../api/client";
import { useRunSnapshot } from "../../api/hooks";

const { Text } = Typography;

export function RunSnapshotPanel({ runId }: { runId: string }) {
  const { data, isLoading, error } = useRunSnapshot(runId);

  if (isLoading) {
    return <Spin size="small" />;
  }
  if (error instanceof ApiError && error.code === "snapshot_unavailable") {
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        description="该运行创建于不可变快照功能启用之前"
      />
    );
  }
  if (error || !data) {
    return <Alert type="error" title="运行配置快照加载失败" showIcon />;
  }

  return (
    <div>
      <Descriptions size="small" column={2} bordered>
        <Descriptions.Item label="契约版本">
          <Tag color="blue">{data.schema_version}</Tag>
        </Descriptions.Item>
        <Descriptions.Item label="创建时间">
          {new Date(data.created_at).toLocaleString("zh-CN")}
        </Descriptions.Item>
        <Descriptions.Item label="SHA-256" span={2}>
          <Text code copyable style={{ wordBreak: "break-all" }}>
            {data.payload_hash}
          </Text>
        </Descriptions.Item>
      </Descriptions>
      <pre
        style={{
          marginTop: 16,
          maxHeight: 520,
          overflow: "auto",
          padding: 12,
          border: "1px solid #d9d9d9",
          borderRadius: 4,
          background: "#fafafa",
          whiteSpace: "pre-wrap",
          wordBreak: "break-word",
        }}
      >
        {JSON.stringify(data.canonical_payload, null, 2)}
      </pre>
    </div>
  );
}
