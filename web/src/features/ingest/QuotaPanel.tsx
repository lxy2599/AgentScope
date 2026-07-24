import { useEffect, useState } from "react";
import {
  Alert,
  Button,
  Descriptions,
  Divider,
  Form,
  InputNumber,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
  message,
} from "antd";
import { EditOutlined, SaveOutlined } from "@ant-design/icons";
import { useProjectQuota, useUpdateProjectQuota } from "../../api/hooks";
import type { ProjectQuota, QuotaDimension, QuotaEvent } from "../../api/types";

const { Text, Title } = Typography;

type EditableQuota = Omit<ProjectQuota, "project_id" | "version" | "updated_at">;

const labels: Record<QuotaDimension, string> = {
  ingest_requests: "采集请求 / 分钟",
  ingest_spans: "Span / 分钟",
  ingest_bytes: "采集流量 / 分钟",
  sse_connections: "实时连接",
  tokens_daily: "Token / 天",
  cost_micros_daily: "成本预算 / 天",
};

function formatInteger(value = 0) {
  return value.toLocaleString("zh-CN");
}

function formatBytes(value: number) {
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GB`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MB`;
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${value} B`;
}

function formatCostMicros(value: number) {
  return `$${(value / 1_000_000).toFixed(2)}`;
}

function quotaValues(quota: ProjectQuota): EditableQuota {
  return {
    ingest_requests_per_minute: quota.ingest_requests_per_minute,
    ingest_spans_per_minute: quota.ingest_spans_per_minute,
    ingest_bytes_per_minute: quota.ingest_bytes_per_minute,
    sse_connections: quota.sse_connections,
    tokens_per_day: quota.tokens_per_day,
    cost_micros_per_day: quota.cost_micros_per_day,
  };
}

export default function QuotaPanel({ projectId }: { projectId: string }) {
  const { data, isLoading, error } = useProjectQuota(projectId);
  const updateQuota = useUpdateProjectQuota(projectId);
  const [editing, setEditing] = useState(false);
  const [form] = Form.useForm<EditableQuota>();

  useEffect(() => {
    if (data?.quota) {
      form.setFieldsValue(quotaValues(data.quota));
    }
  }, [data?.quota, form]);

  if (isLoading) {
    return <Spin size="small" />;
  }
  if (error || !data) {
    return (
      <Alert
        type="warning"
        showIcon
        message="项目配额暂不可用"
        description={error instanceof Error ? error.message : "无法读取配额状态"}
      />
    );
  }

  const { quota, usage } = data;
  const values = [
    {
      key: "ingest_requests",
      label: labels.ingest_requests,
      value: `${formatInteger(usage.ingest_requests)} / ${formatInteger(quota.ingest_requests_per_minute)}`,
    },
    {
      key: "ingest_spans",
      label: labels.ingest_spans,
      value: `${formatInteger(usage.ingest_spans)} / ${formatInteger(quota.ingest_spans_per_minute)}`,
    },
    {
      key: "ingest_bytes",
      label: labels.ingest_bytes,
      value: `${formatBytes(usage.ingest_bytes ?? 0)} / ${formatBytes(quota.ingest_bytes_per_minute)}`,
    },
    {
      key: "sse_connections",
      label: labels.sse_connections,
      value: `${formatInteger(usage.sse_connections)} / ${formatInteger(quota.sse_connections)}`,
    },
    {
      key: "tokens_daily",
      label: labels.tokens_daily,
      value: `${formatInteger(usage.tokens_daily)} / ${formatInteger(quota.tokens_per_day)}`,
    },
    {
      key: "cost_micros_daily",
      label: labels.cost_micros_daily,
      value: `${formatCostMicros(usage.cost_micros_daily ?? 0)} / ${formatCostMicros(quota.cost_micros_per_day)}`,
    },
  ];

  const submit = async () => {
    const next = await form.validateFields();
    await updateQuota.mutateAsync(next);
    message.success("项目配额已更新");
    setEditing(false);
  };

  return (
    <Form form={form} layout="vertical" component={false}>
    <section aria-labelledby="quota-heading">
      <Space style={{ width: "100%", justifyContent: "space-between", marginBottom: 16 }}>
        <div>
          <Title id="quota-heading" level={5} style={{ margin: 0 }}>项目配额</Title>
          <Text type="secondary">策略版本 {quota.version}</Text>
        </div>
        {data.can_edit && !editing && (
          <Button aria-label="编辑配额" icon={<EditOutlined />} onClick={() => setEditing(true)}>编辑配额</Button>
        )}
      </Space>

      {editing ? (
        <>
          <Descriptions column={{ xs: 1, md: 2, xl: 3 }} bordered size="small">
            <Descriptions.Item label="采集请求 / 分钟">
              <Form.Item name="ingest_requests_per_minute" noStyle rules={[{ required: true, type: "number", min: 1 }]}>
                <InputNumber min={1} precision={0} style={{ width: "100%" }} />
              </Form.Item>
            </Descriptions.Item>
            <Descriptions.Item label="Span / 分钟">
              <Form.Item name="ingest_spans_per_minute" noStyle rules={[{ required: true, type: "number", min: 1 }]}>
                <InputNumber min={1} precision={0} style={{ width: "100%" }} />
              </Form.Item>
            </Descriptions.Item>
            <Descriptions.Item label="采集字节 / 分钟">
              <Form.Item name="ingest_bytes_per_minute" noStyle rules={[{ required: true, type: "number", min: 1 }]}>
                <InputNumber min={1} precision={0} style={{ width: "100%" }} />
              </Form.Item>
            </Descriptions.Item>
            <Descriptions.Item label="SSE 并发连接">
              <Form.Item name="sse_connections" noStyle rules={[{ required: true, type: "number", min: 1 }]}>
                <InputNumber min={1} precision={0} style={{ width: "100%" }} />
              </Form.Item>
            </Descriptions.Item>
            <Descriptions.Item label="Token / 天">
              <Form.Item name="tokens_per_day" noStyle rules={[{ required: true, type: "number", min: 1 }]}>
                <InputNumber min={1} precision={0} style={{ width: "100%" }} />
              </Form.Item>
            </Descriptions.Item>
            <Descriptions.Item label="成本微美元 / 天">
              <Form.Item name="cost_micros_per_day" noStyle rules={[{ required: true, type: "number", min: 1 }]}>
                <InputNumber min={1} precision={0} style={{ width: "100%" }} />
              </Form.Item>
            </Descriptions.Item>
          </Descriptions>
          <Space style={{ marginTop: 12 }}>
            <Button aria-label="保存" type="primary" icon={<SaveOutlined />} loading={updateQuota.isPending} onClick={submit}>保存</Button>
            <Button onClick={() => setEditing(false)}>取消</Button>
          </Space>
        </>
      ) : (
        <Descriptions column={{ xs: 1, md: 2, xl: 3 }} bordered size="small">
          {values.map((item) => (
            <Descriptions.Item key={item.key} label={item.label}>{item.value}</Descriptions.Item>
          ))}
        </Descriptions>
      )}

      <Divider titlePlacement="start">最近配额事件</Divider>
      <Table<QuotaEvent>
        rowKey="id"
        size="small"
        pagination={{ pageSize: 5, hideOnSinglePage: true }}
        dataSource={data.events}
        locale={{ emptyText: "最近没有超限或降级事件" }}
        columns={[
          { title: "时间", dataIndex: "occurred_at", render: (value: string) => new Date(value).toLocaleString() },
          { title: "维度", dataIndex: "dimension", render: (value: QuotaDimension) => labels[value] ?? value },
          { title: "结果", dataIndex: "outcome", render: (value: QuotaEvent["outcome"]) => <Tag color={value === "rejected" ? "error" : "warning"}>{value === "rejected" ? "已拒绝" : "降级"}</Tag> },
          { title: "请求量", dataIndex: "requested", render: formatInteger },
          { title: "剩余", dataIndex: "remaining", render: formatInteger },
          { title: "原因", dataIndex: "reason" },
        ]}
      />
    </section>
    </Form>
  );
}
