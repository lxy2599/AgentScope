import { useMemo } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  Card, Tag, Typography, Space, Spin, Empty, Tooltip, Button, Grid,
} from "antd";
import {
  EyeOutlined, DashboardOutlined, SyncOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { useAgentSummaries, useProjectRealtime } from "../../api/hooks";
import type { AgentSummary } from "../../api/types";
import { RealtimeStatusIndicator } from "../../components/ui/RealtimeStatusIndicator";
import { DataTable } from "../../components/ui/DataTable";
import { PageHeader } from "../../components/ui/PageHeader";
import { StatusBadge } from "../../components/ui/StatusBadge";

const { Text } = Typography;

const STATUS_LABELS: Record<string, string> = {
  running: "运行中",
  idle: "在线",
  offline: "离线",
  unknown: "未知",
};

const FRAMEWORK_COLORS: Record<string, string> = {
  langchain: "cyan",
  "crewai": "purple",
  autogen: "geekblue",
  semantic_kernel: "volcano",
  "custom": "orange",
};

function formatMs(ms?: number): string {
  if (ms == null) return "-";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

function formatCost(cost?: number): string {
  if (cost == null) return "-";
  if (cost < 0.01) return `$${cost.toFixed(4)}`;
  return `$${cost.toFixed(3)}`;
}

function timeAgo(dateStr?: string): string {
  if (!dateStr) return "-";
  const diff = Date.now() - new Date(dateStr).getTime();
  const seconds = Math.floor(diff / 1000);
  if (seconds < 60) return `${seconds}秒前`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}小时前`;
  return new Date(dateStr).toLocaleDateString("zh-CN");
}

export function AgentsPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const navigate = useNavigate();
  const screens = Grid.useBreakpoint();
  const isMobile = screens.md === false;
  const realtimeStatus = useProjectRealtime(projectId ?? "");
  const { data: summaries, isLoading } = useAgentSummaries(projectId ?? "");

  const columns: ColumnsType<AgentSummary> = useMemo(() => [
    {
      title: "Agent / 框架",
      key: "agent",
      width: 280,
      fixed: "left",
      render: (_: unknown, record: AgentSummary) => (
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <div>
            <div>
              <Button
                type="link"
                style={{ padding: 0, fontWeight: 500 }}
                onClick={() => navigate(`/projects/${projectId}/agents/${record.id}`)}
              >
                {record.name}
              </Button>
            </div>
            <div style={{ display: "flex", gap: 4, marginTop: 2 }}>
              {record.framework && (
                <Tag color={FRAMEWORK_COLORS[record.framework] || "default"} style={{ fontSize: 11, lineHeight: "18px" }}>
                  {record.framework}
                </Tag>
              )}
              {record.runtime && (
                <Text type="secondary" style={{ fontSize: 11 }}>{record.runtime}</Text>
              )}
            </div>
          </div>
        </div>
      ),
    },
    {
      title: "状态",
      dataIndex: "current_status_summary",
      key: "status",
      width: 80,
      render: (status: string) => {
        return <StatusBadge status={status} label={STATUS_LABELS[status] || status} />;
      },
    },
    {
      title: "实例",
      key: "instances",
      width: 80,
      render: (_: unknown, record: AgentSummary) => (
        <Tooltip title={`在线 ${record.online_instance_count} / 总计 ${record.instance_count}`}>
          <span>
            <Text strong>{record.online_instance_count}</Text>
            <Text type="secondary"> / {record.instance_count}</Text>
          </span>
        </Tooltip>
      ),
    },
    {
      title: "运行中",
      dataIndex: "active_run_count",
      key: "active_runs",
      width: 72,
      render: (count: number) =>
        count > 0 ? (
          <Tag color="blue">{count}</Tag>
        ) : (
          <Text type="secondary">{count}</Text>
        ),
    },
    {
      title: "来源",
      key: "source",
      width: 80,
      render: (_: unknown, record: AgentSummary) => (
        <Text type="secondary" style={{ fontSize: 12 }}>
          {[record.source, record.environment].filter(Boolean).join(" / ") || "-"}
        </Text>
      ),
    },
    {
      title: "Agent 版本",
      dataIndex: "agent_version",
      key: "agent_version",
      width: 90,
      render: (v?: string) => v ? <Tag>{v}</Tag> : "-",
    },
    {
      title: "模型",
      key: "model",
      width: 130,
      render: (_: unknown, record: AgentSummary) => (
        <Text type="secondary" style={{ fontSize: 12 }}>
          {[record.model_provider, record.model_name].filter(Boolean).join(" / ") || "-"}
        </Text>
      ),
    },
    {
      title: "最近心跳",
      dataIndex: "last_heartbeat_at",
      key: "heartbeat",
      width: 100,
      render: (hb?: string) => (
        <Text type="secondary" style={{ fontSize: 12 }}>
          {timeAgo(hb)}
        </Text>
      ),
    },
    {
      title: "P95 延迟",
      dataIndex: "p95_latency_ms",
      key: "p95",
      width: 80,
      render: (ms?: number) => (
        <Text>{formatMs(ms)}</Text>
      ),
    },
    {
      title: "错误率",
      dataIndex: "error_rate",
      key: "error_rate",
      width: 72,
      render: (rate: number) =>
        rate > 0.1 ? (
          <Tag color="red" style={{ margin: 0 }}>{(rate * 100).toFixed(1)}%</Tag>
        ) : rate > 0 ? (
          <Tag color="orange" style={{ margin: 0 }}>{(rate * 100).toFixed(1)}%</Tag>
        ) : (
          <Text type="secondary">0%</Text>
        ),
    },
    {
      title: "成本",
      dataIndex: "total_cost",
      key: "cost",
      width: 80,
      render: (cost?: number) => (
        <Text>{formatCost(cost)}</Text>
      ),
    },
    {
      title: "操作",
      key: "actions",
      width: 100,
      fixed: "right",
      render: (_: unknown, record: AgentSummary) => (
        <Space size="small">
          <Tooltip title="查看详情">
            <Button
              type="link"
              size="small"
              icon={<EyeOutlined />}
              onClick={() => navigate(`/projects/${projectId}/agents/${record.id}`)}
            />
          </Tooltip>
          <Tooltip title="查看运行中任务">
            <Button
              type="link"
              size="small"
              icon={<SyncOutlined />}
              onClick={() => navigate(`/projects/${projectId}/active-runs?agent_id=${record.id}`)}
            />
          </Tooltip>
        </Space>
      ),
    },
  ], [navigate, projectId]);

  return (
    <Space orientation="vertical" size="middle" style={{ width: "100%" }}>
      <PageHeader eyebrow="运行中心" title="Agent 控制中心" description="查看实例在线状态、正在执行的任务与近期运行信号。" extra={<RealtimeStatusIndicator status={realtimeStatus} />} />
      <div
        style={{
          display: "flex",
          flexDirection: isMobile ? "column" : "row",
          justifyContent: "space-between",
          alignItems: isMobile ? "stretch" : "center",
          gap: 12,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <DashboardOutlined style={{ fontSize: 20, color: "#1677ff" }} />
          <Text strong>当前实例状态</Text>
        </div>
        {isMobile ? (
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "repeat(4, minmax(0, 1fr))",
              gap: 8,
              alignItems: "center",
            }}
          >
            <RealtimeStatusIndicator status={realtimeStatus} />
            <CompactStat
              label="在线"
              value={summaries?.filter(
                (summary) =>
                  summary.current_status_summary === "idle"
                  || summary.current_status_summary === "running",
              ).length ?? 0}
              color="#16a34a"
            />
            <CompactStat
              label="运行中"
              value={summaries?.reduce((sum, agent) => sum + agent.active_run_count, 0) ?? 0}
              color="#2563eb"
            />
            <CompactStat
              label="错误"
              value={summaries?.reduce((sum, agent) => sum + agent.recent_error_count, 0) ?? 0}
              color="#dc2626"
            />
          </div>
        ) : (
          <Space size="middle">
            <RealtimeStatusIndicator status={realtimeStatus} />
            {summaries && summaries.length > 0 && (
              <>
                <Text type="secondary">
                  在线 <Tag color="green">{summaries.filter(s => s.current_status_summary === "idle" || s.current_status_summary === "running").length}</Tag>
                </Text>
                <Text type="secondary">
                  运行中 <Tag color="blue">{summaries.reduce((s, a) => s + a.active_run_count, 0)}</Tag>
                </Text>
                <Text type="secondary">
                  错误 <Tag color="red">{summaries.reduce((s, a) => s + a.recent_error_count, 0)}</Tag>
                </Text>
              </>
            )}
          </Space>
        )}
      </div>

      {isLoading ? (
        <div style={{ textAlign: "center", padding: 48 }}>
          <Spin size="large" />
          <div style={{ marginTop: 16, color: "#999" }}>加载中...</div>
        </div>
      ) : summaries && summaries.length > 0 ? (
        <Card styles={{ body: { padding: 0, overflow: "hidden" } }}>
          <DataTable
            dataSource={summaries}
            columns={columns}
            rowKey="id"
            pagination={false}
            size="small"
            scroll={{ x: 1300 }}
            locale={{ emptyText: "暂无 Agent 数据" }}
          />
        </Card>
      ) : (
        <Card>
          <Empty
            description="暂无已注册的 Agent"
            image={Empty.PRESENTED_IMAGE_SIMPLE}
          >
            <span style={{ color: "#999" }}>
              Agent 通过 SDK 运行后会自动注册并上报数据
            </span>
          </Empty>
        </Card>
      )}
    </Space>
  );
}

function CompactStat({
  label,
  value,
  color,
}: {
  label: string;
  value: number;
  color: string;
}) {
  return (
    <div style={{ minWidth: 0, textAlign: "center" }}>
      <Text type="secondary" style={{ display: "block", fontSize: 11 }}>
        {label}
      </Text>
      <Text strong style={{ color, fontSize: 14 }}>
        {value}
      </Text>
    </div>
  );
}
