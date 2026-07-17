import { useParams } from "react-router-dom";
import {
  Card,
  Descriptions,
  Tag,
  Typography,
  Spin,
  Empty,
  Collapse,
  Space,
} from "antd";
import { useRunDetail } from "../../api/hooks";
import { SpanTree } from "./SpanTree";
import { EventTable } from "./EventTable";

const { Title, Text } = Typography;

export function RunDetailPage() {
  const { runId } = useParams<{ runId: string }>();
  const { data, isLoading, error } = useRunDetail(runId ?? "");

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
        <Text type="danger">
          {error instanceof Error ? error.message : "加载运行记录失败"}
        </Text>
      </Card>
    );
  }

  if (!data) {
    return (
      <Card>
        <Empty description="未找到该运行记录" image={Empty.PRESENTED_IMAGE_SIMPLE} />
      </Card>
    );
  }

  const { run, spans, events } = data;

  const collapseItems = [
    ...(run.input_summary
      ? [
          {
            key: "input",
            label: "输入",
            children: (
              <pre style={{ margin: 0, fontSize: 12, whiteSpace: "pre-wrap", wordBreak: "break-all" }}>
                {run.input_summary}
              </pre>
            ),
          },
        ]
      : []),
    ...(run.output_summary
      ? [
          {
            key: "output",
            label: "输出",
            children: (
              <pre style={{ margin: 0, fontSize: 12, whiteSpace: "pre-wrap", wordBreak: "break-all" }}>
                {run.output_summary}
              </pre>
            ),
          },
        ]
      : []),
    ...(run.error
      ? [
          {
            key: "error",
            label: "错误",
            children: (
              <pre style={{ margin: 0, fontSize: 12, whiteSpace: "pre-wrap", wordBreak: "break-all", color: "#ff4d4f" }}>
                {run.error}
              </pre>
            ),
          },
        ]
      : []),
  ];

  return (
    <Space orientation="vertical" size="large" style={{ width: "100%" }}>
      {/* Header */}
      <Card>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start" }}>
          <div>
            <Title level={3} style={{ margin: 0 }}>
              运行诊断
            </Title>
            <Text type="secondary" style={{ fontSize: 13 }}>
              {run.agent_name}
            </Text>
          </div>
          {statusTag(run.status)}
        </div>
      </Card>

      {/* Basic info */}
      <Card title="基本信息">
        <Descriptions column={2} size="small">
          <Descriptions.Item label="Agent 名称">
            {run.agent_name}
          </Descriptions.Item>
          <Descriptions.Item label="状态">
            {statusTag(run.status)}
          </Descriptions.Item>
          <Descriptions.Item label="来源">
            {run.source}
          </Descriptions.Item>
          <Descriptions.Item label="版本">
            {run.agent_version || "—"}
          </Descriptions.Item>
          <Descriptions.Item label="Trace ID">
            <Text code style={{ fontSize: 12 }}>
              {run.trace_id}
            </Text>
          </Descriptions.Item>
          <Descriptions.Item label="延迟">
            {run.latency_ms != null ? `${run.latency_ms}ms` : "—"}
          </Descriptions.Item>
        </Descriptions>

        {collapseItems.length > 0 && (
          <Collapse
            items={collapseItems}
            style={{ marginTop: 16 }}
            size="small"
          />
        )}
      </Card>

      {/* Spans */}
      <Card
        title={
          <span>
            Span 树 <Text type="secondary">（{spans.length}）</Text>
          </span>
        }
      >
        {spans.length > 0 ? (
          <SpanTree spans={spans} />
        ) : (
          <Empty description="暂无 Span" image={Empty.PRESENTED_IMAGE_SIMPLE} />
        )}
      </Card>

      {/* Events */}
      {events.length > 0 && (
        <Card
          title={
            <span>
              事件 <Text type="secondary">（{events.length}）</Text>
            </span>
          }
        >
          <EventTable events={events} />
        </Card>
      )}
    </Space>
  );
}
