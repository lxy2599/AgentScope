import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Button, Select, Table, Tag, Typography } from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { useActiveRuns } from "../../api/hooks";
import type { ActiveRun, RunRuntimeStatus } from "../../api/types";

const { Title } = Typography;

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

const ACTIVE_STATUS_OPTIONS = [
  { value: "", label: "全部状态" },
  { value: "running", label: "运行中" },
  { value: "thinking", label: "思考中" },
  { value: "waiting_llm", label: "等待 LLM" },
  { value: "waiting_tool", label: "等待工具" },
  { value: "waiting_retrieval", label: "等待检索" },
  { value: "waiting_memory", label: "等待记忆" },
  { value: "waiting_user", label: "等待用户" },
  { value: "retrying", label: "重试中" },
];

export default function ActiveRunsPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const [statusFilter, setStatusFilter] = useState<string>("");

  const { data: runs, isLoading, refetch } = useActiveRuns(projectId || "");

  const filtered = useMemo(() => {
    if (!runs) return [];
    if (!statusFilter) return runs;
    return runs.filter((r) => r.status === statusFilter);
  }, [runs, statusFilter]);

  const columns: ColumnsType<ActiveRun> = [
    {
      title: "Agent",
      dataIndex: "agent_name",
      key: "agent_name",
      render: (name: string, record: ActiveRun) => (
        <Link to={`/projects/${projectId}/agents/${record.agent_id}`}>
          {name || "未知"}
        </Link>
      ),
    },
    {
      title: "实例",
      dataIndex: "agent_instance_name",
      key: "agent_instance_name",
      render: (name: string) => name || "-",
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      render: (status: RunRuntimeStatus) => (
        <Tag color={STATUS_COLORS[status] || "default"}>
          {STATUS_LABELS[status] || status}
        </Tag>
      ),
    },
    {
      title: "输入",
      dataIndex: "input_summary",
      key: "input_summary",
      ellipsis: true,
      render: (val: string) => val?.slice(0, 60) || "-",
    },
    {
      title: "最后事件",
      dataIndex: "last_state",
      key: "last_state",
      render: (state: string) =>
        state ? (
          <Tag color={STATUS_COLORS[state] || "default"}>
            {STATUS_LABELS[state] || state}
          </Tag>
        ) : (
          "-"
        ),
    },
    {
      title: "更新时间",
      dataIndex: "updated_at",
      key: "updated_at",
      render: (val: string) => (val ? new Date(val).toLocaleString("zh-CN") : "-"),
    },
    {
      title: "操作",
      key: "actions",
      render: (_: unknown, record: ActiveRun) => (
        <Link to={`/projects/${projectId}/runs/${record.id}/runtime`}>
          查看详情
        </Link>
      ),
    },
  ];

  return (
    <div style={{ padding: 24 }}>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          marginBottom: 16,
        }}
      >
        <Title level={4} style={{ margin: 0 }}>
          在线运行
        </Title>
        <div style={{ display: "flex", gap: 8 }}>
          <Select
            style={{ width: 140 }}
            value={statusFilter}
            onChange={setStatusFilter}
            options={ACTIVE_STATUS_OPTIONS}
          />
          <Button icon={<ReloadOutlined />} onClick={() => refetch()}>
            刷新
          </Button>
        </div>
      </div>
      <Table
        columns={columns}
        dataSource={filtered}
        rowKey="id"
        loading={isLoading}
        pagination={{ pageSize: 20 }}
        locale={{ emptyText: "暂无在线运行" }}
      />
    </div>
  );
}
