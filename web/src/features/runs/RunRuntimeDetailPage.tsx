import { Link, useParams } from "react-router-dom";
import { Card, Descriptions, Spin, Tag, Timeline, Typography, Alert } from "antd";
import { LinkOutlined, LoadingOutlined } from "@ant-design/icons";
import { useRunRuntimeDetail } from "../../api/hooks";
import type { RunRuntimeStatus } from "../../api/types";
import { AgentTimeline, buildTimeline } from "./AgentTimeline";

const { Title, Text } = Typography;

const STATUS_LABELS: Record<string, string> = {
  queued: "排队中",
  running: "运行中",
  thinking: "思考中",
  waiting_llm: "等待 LLM",
  waiting_tool: "等待工具",
  waiting_retrieval: "等待检索",
  waiting_memory: "等待 Memory",
  waiting_user: "等待用户",
  retrying: "重试中",
  completed: "已完成",
  failed: "失败",
  timeout: "超时",
  cancelled: "已取消",
};

const STATUS_COLORS: Record<string, string> = {
  queued: "default",
  running: "processing",
  thinking: "purple",
  waiting_llm: "warning",
  waiting_tool: "orange",
  waiting_retrieval: "geekblue",
  waiting_memory: "pink",
  waiting_user: "cyan",
  retrying: "purple",
  completed: "success",
  failed: "error",
  timeout: "volcano",
  cancelled: "default",
};

// Live statuses for determining if run is in progress
const LIVE_STATUSES = new Set([
  "running",
  "thinking",
  "waiting_llm",
  "waiting_tool",
  "waiting_retrieval",
  "waiting_memory",
  "waiting_user",
  "retrying",
]);

function isLiveStatus(status: string): boolean {
  return LIVE_STATUSES.has(status);
}

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

  // Build timeline from semantic events
  const timelineItems = buildTimeline(
    detail.thoughts,
    detail.final_answers,
    detail.llm_calls,
    detail.tool_calls,
    detail.retrieval_calls,
    detail.skill_calls,
  );

  const hasSemanticEvents = timelineItems.length > 0;
  const runIsLive = isLiveStatus(detail.status);

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

      {/* Semantic Events Section for Live Runs */}
      {runIsLive && (
        <Card
          size="small"
          title={
            <span>
              <LoadingOutlined style={{ marginRight: 6 }} />
              已到达语义事件
            </span>
          }
          style={{ marginBottom: 24 }}
        >
          {hasSemanticEvents ? (
            <AgentTimeline items={timelineItems} />
          ) : (
            <div style={{ padding: "16px 0", textAlign: "center" }}>
              <Alert
                type="info"
                message="等待语义事件到达"
                description="该运行仍在进行中，ClickHouse 中的语义事件尚未到达。页面每 2.5 秒自动刷新。"
                showIcon
              />
            </div>
          )}
        </Card>
      )}

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
