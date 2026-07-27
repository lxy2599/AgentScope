import { useMemo, useState } from "react";
import { useParams, useNavigate, Link } from "react-router-dom";
import {
  Card, Descriptions, Table, Tag, Typography, Badge, Space, Spin, Button,
  Empty, Row, Col, Statistic, Segmented,
} from "antd";
import {
  ArrowLeftOutlined, RobotOutlined, SyncOutlined,
  WarningOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import {
  useActiveRuns,
  useAgentDetail,
  useAgentRecentErrors,
  useProjectRealtime,
} from "../../api/hooks";
import type {
  AgentInstanceInfo, AgentCapabilityInfo, RecentRunType,
  ActiveRun, AgentRuntimeMetadata, CapabilityMetadata,
} from "../../api/types";
import { RealtimeStatusIndicator } from "../../components/ui/RealtimeStatusIndicator";
import { TechnicalId } from "../../components/ui/TechnicalId";

const { Title, Text } = Typography;

const STATUS_LABELS: Record<string, string> = {
  queued: "排队中",
  running: "运行中",
  thinking: "思考中",
  waiting_llm: "等待 LLM",
  waiting_tool: "等待工具",
  waiting_retrieval: "等待检索",
  waiting_memory: "等待记忆",
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
  waiting_memory: "cyan",
  waiting_user: "gold",
  retrying: "volcano",
  completed: "success",
  failed: "error",
  timeout: "default",
  cancelled: "default",
};

const CONNECTION_LABELS: Record<string, string> = {
  online: "在线",
  offline: "离线",
  unknown: "未知",
};

const CONNECTION_COLORS: Record<string, string> = {
  online: "green",
  offline: "default",
  unknown: "default",
};

const REGISTRATION_LABELS: Record<string, string> = {
  active: "已注册",
  inactive: "已停用",
  deprecated: "已弃用",
};

type InstanceFilter = "all" | "online" | "offline";

function formatMs(ms?: number): string {
  if (ms == null) return "-";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}

function capMeta(m?: CapabilityMetadata): CapabilityMetadata {
  return (m ?? {}) as CapabilityMetadata;
}

function sideEffectColor(risk: string): "success" | "warning" | "error" | "default" {
  switch (risk) {
    case "low": return "success";
    case "medium": return "warning";
    case "high": return "error";
    default: return "default";
  }
}

function instanceMetadataValue(metadata: Record<string, unknown> | undefined, key: string): string {
  const value = metadata?.[key];
  if (value == null || value === "") return "—";
  if (typeof value === "string" || typeof value === "number") return String(value);
  return JSON.stringify(value);
}

export function AgentDetailPage() {
  const { projectId, agentId } = useParams<{ projectId: string; agentId: string }>();
  const navigate = useNavigate();
  const realtimeStatus = useProjectRealtime(projectId ?? "");
  const { data: detail, isLoading } = useAgentDetail(agentId ?? "");
  const { data: recentRuns } = useAgentRecentErrors(agentId ?? "");
  const { data: activeRuns } = useActiveRuns(
    projectId ?? "",
    agentId,
    realtimeStatus,
  );
  const [instanceFilter, setInstanceFilter] = useState<InstanceFilter>("all");

  const statusColor = (status: string): "success" | "default" | "warning" => {
    switch (status) {
      case "active": return "success";
      case "inactive": return "default";
      case "deprecated": return "warning";
      default: return "default";
    }
  };

  // Extract metadata fields
  const meta: AgentRuntimeMetadata = detail?.metadata ?? {};
  const framework = meta.framework || "";
  const runtime = meta.runtime || "";
  const source = meta.source || "";
  const environment = meta.environment || "";
  const agentVersion = meta.agent_version || "";
  const modelProvider = meta.model_provider || "";
  const modelName = meta.model_name || "";
  const promptVersion = meta.prompt_version || "";
  const harnessVersion = meta.harness_version || "";

  const instanceColumns: ColumnsType<AgentInstanceInfo> = useMemo(() => [
    {
      title: "实例名",
      dataIndex: "instance_name",
      key: "instance_name",
      width: 140,
      render: (name: string) => name || "—",
    },
    {
      title: "实例 ID",
      dataIndex: "instance_id",
      key: "instance_id",
      width: 120,
      render: (id: string) => <TechnicalId value={id} />,
    },
    {
      title: "连接状态",
      dataIndex: "connection_status",
      key: "connection_status",
      width: 80,
      render: (status: string) => (
        <Badge
          status={status === "online" ? "success" : "default"}
          text={CONNECTION_LABELS[status] || "未知"}
        />
      ),
    },
    {
      title: "上报状态",
      dataIndex: "status",
      key: "status",
      width: 90,
      render: (status: string) => status || "—",
    },
    {
      title: "主机名",
      dataIndex: "hostname",
      key: "hostname",
      width: 120,
    },
    {
      title: "PID",
      dataIndex: "pid",
      key: "pid",
      width: 60,
      render: (pid: number) => pid > 0 ? pid : "—",
    },
    {
      title: "容量 / Inflight",
      key: "capacity",
      width: 130,
      render: (_: unknown, record: AgentInstanceInfo) => (
        <Text type="secondary">
          {instanceMetadataValue(record.metadata, "capacity")} / {instanceMetadataValue(record.metadata, "inflight_runs")}
        </Text>
      ),
    },
    {
      title: "SDK 版本",
      key: "sdk_version",
      width: 110,
      render: (_: unknown, record: AgentInstanceInfo) =>
        instanceMetadataValue(record.metadata, "sdk_version"),
    },
    {
      title: "运行中任务",
      dataIndex: "active_run_id",
      key: "active_run_id",
      width: 100,
      render: (rid: string) =>
        rid ? <Link to={`/projects/${projectId}/runs/${rid}/runtime`}>查看</Link> : "—",
    },
    {
      title: "上次心跳",
      dataIndex: "last_heartbeat_at",
      key: "last_heartbeat_at",
      width: 160,
      render: (date: string) =>
        date ? new Date(date).toLocaleString("zh-CN") : "—",
    },
    {
      title: "启动时间",
      dataIndex: "started_at",
      key: "started_at",
      width: 160,
      render: (date: string) =>
        date ? new Date(date).toLocaleString("zh-CN") : "—",
    },
  ], [projectId]);

  const capabilityColumns: ColumnsType<AgentCapabilityInfo> = useMemo(() => [
    {
      title: "名称",
      dataIndex: "name",
      key: "name",
    },
    {
      title: "类型",
      key: "kind",
      render: (_: unknown, record: AgentCapabilityInfo) => {
        const m = capMeta(record.metadata);
        return m.kind ? <Tag color="blue">{m.kind}</Tag> : "—";
      },
    },
    {
      title: "版本",
      key: "version",
      render: (_: unknown, record: AgentCapabilityInfo) => {
        const m = capMeta(record.metadata);
        return m.version || "—";
      },
    },
    {
      title: "风险等级",
      key: "risk",
      render: (_: unknown, record: AgentCapabilityInfo) => {
        const m = capMeta(record.metadata);
        return m.side_effect_risk ? (
          <Badge status={sideEffectColor(m.side_effect_risk)} text={m.side_effect_risk} />
        ) : "—";
      },
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
  ], []);

  const activeRunColumns: ColumnsType<ActiveRun> = useMemo(() => [
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      width: 100,
      render: (status: string) => (
        <Tag color={STATUS_COLORS[status] || "default"}>
          {STATUS_LABELS[status] || status}
        </Tag>
      ),
    },
    {
      title: "当前阶段",
      dataIndex: "last_state",
      key: "last_state",
      width: 100,
      render: (state: string) =>
        state ? (
          <Tag color={STATUS_COLORS[state] || "default"}>
            {STATUS_LABELS[state] || state}
          </Tag>
        ) : "-",
    },
    {
      title: "输入",
      dataIndex: "input_summary",
      key: "input_summary",
      ellipsis: true,
      render: (val: string) => val?.slice(0, 60) || "-",
    },
    {
      title: "时长",
      dataIndex: "latency_ms",
      key: "latency_ms",
      width: 80,
      render: (ms?: number) => formatMs(ms),
    },
    {
      title: "更新时间",
      dataIndex: "updated_at",
      key: "updated_at",
      width: 160,
      render: (val: string) => (val ? new Date(val).toLocaleString("zh-CN") : "-"),
    },
    {
      title: "操作",
      key: "actions",
      width: 80,
      render: (_: unknown, record: ActiveRun) => (
        <Link to={`/projects/${projectId}/runs/${record.id}/runtime`}>
          详情
        </Link>
      ),
    },
  ], [projectId]);

  const errorColumns: ColumnsType<RecentRunType> = useMemo(() => [
    {
      title: "Run ID",
      dataIndex: "id",
      key: "id",
      width: 100,
      ellipsis: true,
      render: (id: string) => <TechnicalId value={id} />,
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      width: 80,
      render: (status: string) => (
        <Tag color={STATUS_COLORS[status] || "default"}>
          {STATUS_LABELS[status] || status}
        </Tag>
      ),
    },
    {
      title: "错误信息",
      dataIndex: "error",
      key: "error",
      ellipsis: true,
      render: (err: string) => err ? <Text style={{ color: "#ff4d4f" }}>{err.slice(0, 60)}</Text> : "—",
    },
    {
      title: "输入",
      dataIndex: "input_summary",
      key: "input_summary",
      ellipsis: true,
      width: 120,
      render: (val: string) => val?.slice(0, 40) || "-",
    },
    {
      title: "延迟",
      dataIndex: "latency_ms",
      key: "latency_ms",
      width: 72,
      render: (ms?: number) => formatMs(ms),
    },
    {
      title: "更新时间",
      dataIndex: "updated_at",
      key: "updated_at",
      width: 140,
      render: (val: string) => (val ? new Date(val).toLocaleString("zh-CN") : "-"),
    },
    {
      title: "操作",
      key: "actions",
      width: 100,
      render: (_: unknown, record: RecentRunType) => (
        <Space size="small">
          <Link to={`/projects/${projectId}/runs/${record.id}/runtime`}>运行详情</Link>
          {record.trace_id && (
            <Link to={`/projects/${projectId}/traces/${record.trace_id}`}>Trace</Link>
          )}
        </Space>
      ),
    },
  ], [projectId]);

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

  const runtimeSummary = detail.runtime_summary ?? {
    total_instance_count: detail.instances.length,
    online_instance_count: detail.instances.filter((instance) => instance.connection_status === "online").length,
    offline_instance_count: detail.instances.filter((instance) => instance.connection_status === "offline").length,
    connection_status: "unknown" as const,
  };
  const onlineCount = runtimeSummary.online_instance_count;
  const offlineCount = runtimeSummary.offline_instance_count;
  const totalCount = runtimeSummary.total_instance_count;
  const activeRunCount = activeRuns?.filter(
    (run) => !["completed", "failed", "timeout", "cancelled"].includes(run.status),
  ).length || 0;
  const recentErrors = recentRuns?.length || 0;
  const filteredInstances = detail.instances.filter((instance) =>
    instanceFilter === "all" || instance.connection_status === instanceFilter,
  );

  return (
    <Space orientation="vertical" size="middle" style={{ width: "100%" }}>
      <Button
        type="link"
        icon={<ArrowLeftOutlined />}
        style={{ padding: 0 }}
        onClick={() => navigate(-1)}
      >
        返回 Agent 列表
      </Button>

      {/* Header */}
      <Card styles={{ body: { padding: 16 } }}>
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <RobotOutlined style={{ fontSize: 28, color: "#1677ff" }} />
          <div>
            <Title level={4} style={{ margin: 0 }}>{detail.name}</Title>
            <Text type="secondary">{detail.description || "无描述"}</Text>
          </div>
          <div style={{ marginLeft: "auto" }}>
            <Space>
              <RealtimeStatusIndicator status={realtimeStatus} />
              <Tag color={statusColor(detail.status)}>
                注册：{REGISTRATION_LABELS[detail.status] || detail.status}
              </Tag>
              <Tag color={CONNECTION_COLORS[runtimeSummary.connection_status] || "default"}>
                连接：{CONNECTION_LABELS[runtimeSummary.connection_status] || "未知"}
              </Tag>
              <Tag>v{detail.version}</Tag>
            </Space>
          </div>
        </div>

        {/* Summary stats row */}
        <Row gutter={16} style={{ marginTop: 16 }}>
          <Col xs={12} md={6} xl={4}>
            <Statistic
              title="在线实例"
              value={onlineCount}
              suffix={`/ ${totalCount}`}
              styles={{ content: { color: onlineCount > 0 ? "#3f8600" : "#999" } }}
            />
          </Col>
          <Col xs={12} md={6} xl={4}>
            <Statistic
              title="离线实例"
              value={offlineCount}
              styles={{ content: { color: offlineCount > 0 ? "#8c8c8c" : "#999" } }}
            />
          </Col>
          <Col xs={12} md={6} xl={4}>
            <Statistic
              title="运行中任务"
              value={activeRunCount}
              styles={{ content: { color: "#1677ff" } }}
              prefix={<SyncOutlined />}
            />
          </Col>
          <Col xs={12} md={6} xl={4}>
            <Statistic
              title="最近错误"
              value={recentErrors}
              styles={{ content: { color: recentErrors > 0 ? "#ff4d4f" : "#999" } }}
              prefix={recentErrors > 0 ? <WarningOutlined /> : undefined}
            />
          </Col>
          <Col xs={12} md={6} xl={4}>
            <Statistic
              title="能力数"
              value={detail.capabilities?.length || 0}
            />
          </Col>
        </Row>
      </Card>

      <Card title="注册信息" size="small">
        <Descriptions
          column={2}
          size="small"
          bordered
          items={[
            { key: "agent-id", label: "Agent 技术 ID", children: <TechnicalId value={detail.id} /> },
            { key: "project-id", label: "项目技术 ID", children: <TechnicalId value={detail.project_id} /> },
            {
              key: "registration-status",
              label: "注册状态",
              children: <Badge status={statusColor(detail.status)} text={REGISTRATION_LABELS[detail.status] || detail.status} />,
            },
            {
              key: "connection-status",
              label: "连接状态",
              children: <Badge status={runtimeSummary.connection_status === "online" ? "success" : "default"} text={CONNECTION_LABELS[runtimeSummary.connection_status] || "未知"} />,
            },
            { key: "framework", label: "框架", children: meta.framework || "—" },
            { key: "runtime", label: "运行时", children: meta.runtime || "—" },
            { key: "source", label: "来源", children: meta.source || "—" },
            { key: "environment", label: "环境", children: meta.environment || "—" },
            {
              key: "agent-version",
              label: "Agent 版本",
              children: meta.agent_version ? <Tag>{meta.agent_version}</Tag> : <Tag>v{detail.version}</Tag>,
            },
            { key: "contract-version", label: "合约版本", children: meta.contract_version || "—" },
            {
              key: "created-at",
              label: "注册时间",
              children: detail.created_at ? new Date(detail.created_at).toLocaleString("zh-CN") : "—",
            },
            {
              key: "updated-at",
              label: "最后更新",
              children: detail.updated_at ? new Date(detail.updated_at).toLocaleString("zh-CN") : "—",
            },
            {
              key: "last-heartbeat",
              label: "最近心跳",
              children: runtimeSummary.last_heartbeat_at ? new Date(runtimeSummary.last_heartbeat_at).toLocaleString("zh-CN") : "—",
            },
          ]}
        />
      </Card>

      {/* Runtime Contract */}
      {(framework || runtime || source || environment || agentVersion || modelProvider) && (
        <Card title="Runtime Contract" size="small">
          <Descriptions
            column={3}
            size="small"
            bordered
            items={[
              ...(framework ? [{ key: "framework", label: "框架", children: framework }] : []),
              ...(runtime ? [{ key: "runtime", label: "运行时", children: runtime }] : []),
              ...(source ? [{ key: "source", label: "来源", children: source }] : []),
              ...(environment ? [{ key: "environment", label: "环境", children: environment }] : []),
              ...(agentVersion ? [{ key: "agent-version", label: "Agent 版本", children: agentVersion }] : []),
              ...(modelName ? [{
                key: "model",
                label: "模型",
                children: [modelProvider, modelName].filter(Boolean).join(" / ") || modelName,
              }] : []),
              ...(promptVersion ? [{ key: "prompt-version", label: "提示词版本", children: promptVersion }] : []),
              ...(harnessVersion ? [{ key: "harness-version", label: "Harness 版本", children: harnessVersion }] : []),
            ]}
          />
        </Card>
      )}

      {/* Active Runs */}
      <Card
        title={
          <Space>
            <SyncOutlined />
            <span>运行中任务</span>
          </Space>
        }
        size="small"
      >
        {activeRuns && activeRuns.filter(r => !["completed","failed","timeout","cancelled"].includes(r.status)).length > 0 ? (
          <Table
            dataSource={activeRuns.filter(r => !["completed","failed","timeout","cancelled"].includes(r.status))}
            columns={activeRunColumns}
            rowKey="id"
            pagination={false}
            size="small"
          />
        ) : (
          <Empty description="暂无运行中任务" image={Empty.PRESENTED_IMAGE_SIMPLE} />
        )}
      </Card>

      {/* Recent Errors */}
      <Card
        title={
          <Space>
            <WarningOutlined />
            <span>最近错误</span>
            {recentErrors > 0 && <Tag color="red">{recentErrors}</Tag>}
          </Space>
        }
        size="small"
      >
        {recentRuns && recentRuns.length > 0 ? (
          <Table
            dataSource={recentRuns}
            columns={errorColumns}
            rowKey="id"
            pagination={false}
            size="small"
          />
        ) : (
          <Empty description="最近 24 小时无错误" image={Empty.PRESENTED_IMAGE_SIMPLE} />
        )}
      </Card>

      <Card
        title="运行实例"
        size="small"
        extra={
          <Segmented
            value={instanceFilter}
            options={[
              { label: `全部 ${totalCount}`, value: "all" },
              { label: `在线 ${onlineCount}`, value: "online" },
              { label: `离线 ${offlineCount}`, value: "offline" },
            ]}
            onChange={(value) => setInstanceFilter(value as InstanceFilter)}
          />
        }
      >
        {detail.instances && detail.instances.length > 0 ? (
          <Table
            dataSource={filteredInstances}
            columns={instanceColumns}
            rowKey="id"
            pagination={false}
            size="small"
            scroll={{ x: 1150 }}
          />
        ) : (
          <Empty description="暂无运行实例" image={Empty.PRESENTED_IMAGE_SIMPLE} />
        )}
      </Card>

      {/* Capabilities */}
      <Card title="能力 (Capabilities)" size="small">
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
