import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Button, Select, Skeleton, Space } from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { useActiveRuns, useProjectRealtime } from "../../api/hooks";
import type { ActiveRun, RunRuntimeStatus } from "../../api/types";
import { RealtimeStatusIndicator } from "../../components/ui/RealtimeStatusIndicator";
import { DataTable } from "../../components/ui/DataTable";
import { EmptyState } from "../../components/ui/EmptyState";
import { PageHeader } from "../../components/ui/PageHeader";
import { PageSection } from "../../components/ui/PageSection";
import { StatusBadge } from "../../components/ui/StatusBadge";

const STATUS_LABELS: Record<string, string> = { queued: "排队中", running: "运行中", thinking: "思考中", waiting_llm: "等待 LLM", waiting_tool: "等待工具", waiting_retrieval: "等待检索", waiting_memory: "等待记忆", waiting_user: "等待用户", retrying: "重试中", completed: "已完成", failed: "失败", timeout: "超时", cancelled: "已取消" };
const options = [{ value: "", label: "全部状态" }, ...["running", "thinking", "waiting_llm", "waiting_tool", "waiting_retrieval", "waiting_memory", "waiting_user", "retrying"].map((value) => ({ value, label: STATUS_LABELS[value] }))];

export default function ActiveRunsPage() {
  const { projectId = "" } = useParams<{ projectId: string }>();
  const [statusFilter, setStatusFilter] = useState("");
  const realtimeStatus = useProjectRealtime(projectId);
  const { data: runs = [], isLoading, refetch } = useActiveRuns(projectId, undefined, realtimeStatus);
  const filtered = useMemo(() => statusFilter ? runs.filter((run) => run.status === statusFilter) : runs, [runs, statusFilter]);
  const columns: ColumnsType<ActiveRun> = [
    { title: "Agent", dataIndex: "agent_name", render: (value, record) => <Link to={`/projects/${projectId}/agents/${record.agent_id}`}>{value || "未知 Agent"}</Link> },
    { title: "实例", dataIndex: "agent_instance_name", render: (value) => value || "未上报" },
    { title: "当前状态", dataIndex: "status", render: (value: RunRuntimeStatus) => <StatusBadge status={value} /> },
    { title: "输入摘要", dataIndex: "input_summary", ellipsis: true, render: (value: string) => value?.slice(0, 80) || "未采集" },
    { title: "最后事件", dataIndex: "last_state", render: (value: string) => value ? <StatusBadge status={value} /> : "—" },
    { title: "更新时间", dataIndex: "updated_at", render: (value: string) => value ? new Date(value).toLocaleString("zh-CN") : "—" },
    { title: "", key: "action", fixed: "right", render: (_, record) => <Link to={`/projects/${projectId}/runs/${record.id}/runtime`}>查看详情</Link> },
  ];
  return <div>
    <PageHeader eyebrow="运行中心" title="在线运行" description="SSE 优先推送当前执行状态；连接不可用时会自动降级为轮询。" extra={<Space wrap><RealtimeStatusIndicator status={realtimeStatus} /><Select style={{ width: 144 }} value={statusFilter} onChange={setStatusFilter} options={options} /><Button icon={<ReloadOutlined />} onClick={() => refetch()}>刷新</Button></Space>} />
    <PageSection title={`${filtered.length} 个活跃 Run`} description="点击实时详情可以查看状态流、关键事件和当前 Trace。">{isLoading ? <Skeleton active paragraph={{ rows: 8 }} /> : filtered.length ? <DataTable columns={columns} dataSource={filtered} rowKey="id" pagination={{ pageSize: 20 }} /> : <EmptyState title="暂无在线运行" description="当前筛选条件下没有在线运行；正在执行、等待或重试的 Agent 任务会自动显示在这里。" />}</PageSection>
  </div>;
}
