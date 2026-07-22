import { useMemo } from "react";
import { useParams, useNavigate, Link } from "react-router-dom";
import {
  Card, Descriptions, Table, Tag, Typography, Badge, Space, Spin, Button,
  Empty, Tooltip, Row, Col, Statistic,
} from "antd";
import {
  ArrowLeftOutlined, RobotOutlined, SyncOutlined,
  WarningOutlined, ClockCircleOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { useAgentDetail, useAgentRecentErrors, useActiveRuns } from "../../api/hooks";
import type {
  AgentInstanceInfo, AgentCapabilityInfo, RecentRunType,
  ActiveRun, AgentRuntimeMetadata, CapabilityMetadata,
} from "../../api/types";

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

export function AgentDetailPage() {
  const { projectId, agentId } = useParams<{ projectId: string; agentId: string }>();
  const navigate = useNavigate();
  const { data: detail, isLoading } = useAgentDetail(agentId ?? "");
  const { data: recentRuns } = useAgentRecentErrors(agentId ?? "");
  const { data: activeRuns } = useActiveRuns(projectId ?? "", agentId);

  const statusColor = (status: string) => {
    switch (status) {
      case "active": return "green";
      case "inactive": return "default";
      case "deprecated": return "orange";
      default: return "default";
    }
  };

  // Extract metadata fields
  const meta = detail?.metadata || {};
  const framework = meta.framework as string || "";
  const runtime = meta.runtime as string || "";
  const source = meta.source as string || "";
  const environment = meta.environment as string || "";
  const agentVersion = meta.agent_version as string || "";
  const modelProvider = meta.model_provider as string || "";
  const modelName = meta.model_name as string || "";
  const promptVersion = meta.prompt_version as string || "";
  const harnessVersion = meta.harness_version as string || "";

  const instanceColumns: ColumnsType<AgentInstanceInfo> = useMemo(() => [
    {
      title: "实例 ID",
      dataIndex: "instance_id",
      key: "instance_id",
      width: 120,
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      width: 80,
      render: (status: string) => (
        <Badge status={status === "online" ? "success" : "default"} text={status} />
      ),
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
      render: (ms?: string) => formatMs(ms ? Number(ms) : undefined),
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
      render: (_: unknown, record: RecentRunType) => (
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
      render: (id: string) => (
        <Tooltip title={id}>
          <span style={{ fontFamily: "monospace", fontSize: 12 }}>{id.slice(0, 8)}</span>
        </Tooltip>
      ),
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

  const onlineCount = detail.instances?.filter(i => i.status === "online").length || 0;
  const totalCount = detail.instances?.length || 0;
  const recentErrors = recentRuns?.length || 0;

  return (
    <Space direction="vertical" size="middle" style={{ width: "100%" }}>
      <Button
        type="link"
        icon={<ArrowLeftOutlined />}
        style={{ padding: 0 }}
        onClick={() => navigate(-1)}
      >
        返回 Agent 列表
      </Button>

      {/* Header */}
      <Card bodyStyle={{ padding: 16 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <RobotOutlined style={{ fontSize: 28, color: "#1677ff" }} />
          <div>
            <Title level={4} style={{ margin: 0 }}>{detail.name}</Title>
            <Text type="secondary">{detail.description || "无描述"}</Text>
          </div>
          <div style={{ marginLeft: "auto" }}>
            <Space>
              <Badge status={statusColor(detail.status) as "success" | "default" | "warning"} text={detail.status} />
              <Tag>v{detail.version}</Tag>
            </Space>
          </div>
        </div>

        {/* Summary stats row */}
        <Row gutter={16} style={{ marginTop: 16 }}>
          <Col span={6}>
            <Statistic
              title="在线实例"
              value={onlineCount}
              suffix={`/ ${totalCount}`}
              valueStyle={{ color: onlineCount > 0 ? "#3f8600" : "#999" }}
            />
          </Col>
          <Col span={6}>
            <Statistic
              title="运行中任务"
              value={activeRuns?.filter(r => !["completed","failed","timeout","cancelled"].includes(r.status)).length || 0}
              valueStyle={{ color: "#1677ff" }}
              prefix={<SyncOutlined />}
            />
          </Col>
          <Col span={6}>
            <Statistic
              title="最近错误"
              value={recentErrors}
              valueStyle={{ color: recentErrors > 0 ? "#ff4d4f" : "#999" }}
              prefix={recentErrors > 0 ? <WarningOutlined /> : undefined}
            />
          </Col>
          <Col span={6}>
            <Statistic
              title="能力数"
              value={detail.capabilities?.length || 0}
            />
          </Col>
        </Row>

        <Descriptions column={2} size="small" bordered style={{ marginTop: 16 }}>
          <Descriptions.Item label="Agent ID">{detail.id}</Descriptions.Item>
          <Descriptions.Item label="项目 ID">{detail.project_id}</Descriptions.Item>
          <Descriptions.Item label="框架">{meta.framework || "—"}</Descriptions.Item>
          <Descriptions.Item label="运行时">{meta.runtime || "—"}</Descriptions.Item>
          <Descriptions.Item label="来源">{meta.source || "—"}</Descriptions.Item>
          <Descriptions.Item label="环境">{meta.environment || "—"}</Descriptions.Item>
          <Descriptions.Item label="Agent 版本">
            {meta.agent_version ? <Tag>{meta.agent_version}</Tag> : <Tag>v{detail.version}</Tag>}
          </Descriptions.Item>
          <Descriptions.Item label="合约版本">{meta.contract_version || "—"}</Descriptions.Item>
          <Descriptions.Item label="创建时间">
            {detail.created_at ? new Date(detail.created_at).toLocaleString("zh-CN") : "—"}
          </Descriptions.Item>
          <Descriptions.Item label="更新时间">
            {detail.updated_at ? new Date(detail.updated_at).toLocaleString("zh-CN") : "—"}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      {/* Runtime Contract */}
      {(framework || runtime || source || environment || agentVersion || modelProvider) && (
        <Card title="Runtime Contract" size="small">
          <Descriptions column={3} size="small" bordered>
            {framework && <Descriptions.Item label="框架">{framework}</Descriptions.Item>}
            {runtime && <Descriptions.Item label="运行时">{runtime}</Descriptions.Item>}
            {source && <Descriptions.Item label="来源">{source}</Descriptions.Item>}
            {environment && <Descriptions.Item label="环境">{environment}</Descriptions.Item>}
            {agentVersion && <Descriptions.Item label="Agent 版本">{agentVersion}</Descriptions.Item>}
            {modelName && (
              <Descriptions.Item label="模型">
                {[modelProvider, modelName].filter(Boolean).join(" / ") || modelName}
              </Descriptions.Item>
            )}
            {promptVersion && <Descriptions.Item label="提示词版本">{promptVersion}</Descriptions.Item>}
            {harnessVersion && <Descriptions.Item label="Harness 版本">{harnessVersion}</Descriptions.Item>}
          </Descriptions>
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

      {/* Online Instances */}
      <Card title="运行实例" size="small">
        {detail.instances && detail.instances.length > 0 ? (
          <Table
            dataSource={detail.instances}
            columns={instanceColumns}
            rowKey="id"
            pagination={false}
            size="small"
            scroll={{ x: 800 }}
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
