import { Link, useParams } from "react-router-dom";
import { Descriptions, Spin, Tag, Timeline, Typography } from "antd";
import { LinkOutlined } from "@ant-design/icons";
import { useRunRuntimeDetail } from "../../api/hooks";
import type { RunRuntimeStatus } from "../../api/types";

const { Title, Text } = Typography;

const STATUS_LABELS: Record<string, string> = {
  queued: "排队中",
  running: "运行中",
  waiting_llm: "等待 LLM",
  waiting_tool: "等待工具",
  waiting_retrieval: "等待检索",
  retrying: "重试中",
  completed: "已完成",
  failed: "失败",
  timeout: "超时",
  cancelled: "已取消",
};

const STATUS_COLORS: Record<string, string> = {
  queued: "default",
  running: "processing",
  waiting_llm: "warning",
  waiting_tool: "orange",
  waiting_retrieval: "geekblue",
  retrying: "purple",
  completed: "success",
  failed: "error",
  timeout: "volcano",
  cancelled: "default",
};

export default function RunRuntimeDetailPage() {
  const { projectId, runId } = useParams<{
    projectId: string;
    runId: string;
  }>();
  const { data: detail, isLoading } = useRunRuntimeDetail(
    projectId || "",
    runId || "",
  );

  if (isLoading) {
    return (
      <div style={{ textAlign: "center", padding: 60 }}>
        <Spin size="large" />
      </div>
    );
  }

  if (!detail) {
    return (
      <div style={{ padding: 24 }}>
        <Text type="danger">运行详情未找到</Text>
      </div>
    );
  }

  return (
    <div style={{ padding: 24 }}>
      <Title level={4} style={{ marginBottom: 24 }}>
        运行详情
      </Title>

      <Descriptions
        column={2}
        bordered
        size="small"
        style={{ marginBottom: 24 }}
      >
        <Descriptions.Item label="运行 ID">{detail.id}</Descriptions.Item>
        <Descriptions.Item label="状态">
          <Tag color={STATUS_COLORS[detail.status] || "default"}>
            {STATUS_LABELS[detail.status] || detail.status}
          </Tag>
        </Descriptions.Item>
        <Descriptions.Item label="Agent">
          {detail.agent_id ? (
            <Link
              to={`/projects/${projectId}/agents/${detail.agent_id}`}
            >
              {detail.agent_name || "未知"}
            </Link>
          ) : (
            "-"
          )}
        </Descriptions.Item>
        <Descriptions.Item label="实例">
          {detail.agent_instance_name || "-"}
        </Descriptions.Item>
        <Descriptions.Item label="输入" span={2}>
          {detail.input || "-"}
        </Descriptions.Item>
        {detail.error && (
          <Descriptions.Item label="错误" span={2}>
            <Text type="danger">{detail.error}</Text>
          </Descriptions.Item>
        )}
        <Descriptions.Item label="Trace ID">
          {detail.trace_id ? (
            <Link
              to={`/projects/${projectId}/runs/${detail.id}`}
            >
              <LinkOutlined /> 查看 Trace
            </Link>
          ) : (
            "-"
          )}
        </Descriptions.Item>
        <Descriptions.Item label="创建时间">
          {detail.created_at
            ? new Date(detail.created_at).toLocaleString("zh-CN")
            : "-"}
        </Descriptions.Item>
      </Descriptions>

      <Title level={5} style={{ marginBottom: 16 }}>
        状态事件时间线
      </Title>
      <Timeline
        items={detail.events.map((event) => ({
          color:
            event.to_status === "failed" || event.to_status === "timeout"
              ? "red"
              : event.to_status === "completed"
                ? "green"
                : "blue",
          children: (
            <div>
              <Tag color={STATUS_COLORS[event.to_status] || "default"}>
                {STATUS_LABELS[event.to_status] || event.to_status}
              </Tag>
              {event.message && (
                <Text style={{ marginLeft: 8 }}>{event.message}</Text>
              )}
              <div>
                <Text type="secondary" style={{ fontSize: 12 }}>
                  {event.created_at
                    ? new Date(event.created_at).toLocaleString("zh-CN")
                    : ""}
                </Text>
              </div>
            </div>
          ),
        }))}
      />
    </div>
  );
}
