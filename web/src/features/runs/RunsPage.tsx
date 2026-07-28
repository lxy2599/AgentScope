import { useNavigate, useParams } from "react-router-dom";
import { Button, Skeleton } from "antd";
import { DashboardOutlined, EyeOutlined } from "@ant-design/icons";
import { useProjectRuns } from "../../api/hooks";
import { DataTable } from "../../components/ui/DataTable";
import { EmptyState } from "../../components/ui/EmptyState";
import { PageHeader } from "../../components/ui/PageHeader";
import { PageSection } from "../../components/ui/PageSection";
import { StatusBadge } from "../../components/ui/StatusBadge";
import { TechnicalId } from "../../components/ui/TechnicalId";

export function RunsPage() {
  const { projectId = "" } = useParams<{ projectId: string }>();
  const navigate = useNavigate();
  const { data: runs = [], isLoading } = useProjectRuns(projectId);
  const failures = runs.filter((run) => ["failed", "error", "timeout"].includes(run.status.toLowerCase())).length;
  return <div>
    <PageHeader eyebrow="运行中心" title="Runs / Trace" description="按照状态与时间定位真实运行，打开 Run 后查看完整执行路径、评测和不可变快照。" extra={<Button icon={<DashboardOutlined />} onClick={() => navigate(`/projects/${projectId}/active-runs`)}>查看实时运行</Button>} />
    <div className="signal-overview"><div className={`signal-card ${failures ? "signal-card--danger" : "signal-card--success"}`}><span className="signal-card__label">失败 / 超时</span><strong className="signal-card__value">{failures}</strong><span className="signal-card__hint">需要优先诊断的运行</span></div><div className="signal-card"><span className="signal-card__label">运行记录</span><strong className="signal-card__value">{runs.length}</strong><span className="signal-card__hint">当前项目可追溯 Run</span></div></div>
    <PageSection title="运行记录" description="长 Trace ID 与详情在独立 Run 页面中完整呈现。">
      {isLoading ? <Skeleton active paragraph={{ rows: 7 }} /> : runs.length ? <DataTable rowKey="id" dataSource={runs} pagination={{ pageSize: 15 }} columns={[
        { title: "Agent", dataIndex: "agent_name", render: (value: string) => <strong>{value || "未命名 Agent"}</strong> },
        { title: "状态", dataIndex: "status", render: (value: string) => <StatusBadge status={value} /> },
        { title: "来源", dataIndex: "source", render: (value: string) => value || "—" },
        { title: "Trace ID", dataIndex: "trace_id", render: (value: string) => <TechnicalId value={value} /> },
        { title: "创建时间", dataIndex: "created_at", render: (value: string) => new Date(value).toLocaleString("zh-CN") },
        { title: "", key: "action", fixed: "right", render: (_: unknown, record: { id: string }) => <Button type="link" icon={<EyeOutlined />} onClick={() => navigate(`/runs/${record.id}`)}>查看诊断</Button> },
      ]} /> : <EmptyState title="尚未收到运行记录" description="使用项目 Token 接入 SDK 或 Runner 后，Run 与 Trace 会自动关联到这里。" actionLabel="查看接入方式" onAction={() => navigate(`/projects/${projectId}/settings`)} />}
    </PageSection>
  </div>;
}
