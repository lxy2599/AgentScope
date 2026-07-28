import { useParams, useNavigate } from "react-router-dom";
import { Button, Drawer, Space } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import { useState } from "react";
import { useExperiments } from "../../api/hooks";
import type { Experiment } from "../../api/types";
import { CreateExperimentPanel } from "./CreateExperimentPanel";
import { DataTable } from "../../components/ui/DataTable";
import { EmptyState } from "../../components/ui/EmptyState";
import { PageHeader } from "../../components/ui/PageHeader";
import { PageSection } from "../../components/ui/PageSection";
import { StatusBadge } from "../../components/ui/StatusBadge";

export function ExperimentsPage() {
  const { projectId = "" } = useParams<{ projectId: string }>();
  const navigate = useNavigate();
  const [createOpen, setCreateOpen] = useState(false);
  const { data: experiments = [], isLoading } = useExperiments(projectId);
  return <div>
    <PageHeader eyebrow="离线评测" title="实验" description="在固定数据集上比较 Agent 版本、评分维度和运行成本，形成可审计的发布依据。" extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>创建实验</Button>} />
    <PageSection title="实验记录" description="优先查看失败、回归或仍在执行的评测任务。">
      {experiments.length ? <DataTable<Experiment> loading={isLoading} rowKey="id" dataSource={experiments} pagination={{ pageSize: 12 }} columns={[
        { title: "目标 Agent", dataIndex: "target_name", render: (value: string, record) => <Space direction="vertical" size={0}><strong>{value}</strong><span style={{ color: "var(--as-muted)" }}>{record.target_version || "未标记版本"}</span></Space> },
        { title: "状态", dataIndex: "status", render: (value: string) => <StatusBadge status={value} /> },
        { title: "用例", dataIndex: "total_cases", render: (value: number, record) => `${value}（通过 ${record.passed_cases} / 失败 ${record.failed_cases}）` },
        { title: "综合分", dataIndex: "aggregate_score", render: (value?: number) => value === undefined ? "评测中" : value.toFixed(3) },
        { title: "创建时间", dataIndex: "created_at", render: (value: string) => new Date(value).toLocaleString("zh-CN") },
        { title: "", key: "action", render: (_, record) => <Button type="link" onClick={() => navigate(`/experiments/${record.id}`)}>查看结论</Button> },
      ]} /> : <EmptyState title="还没有实验" description="选择数据集和 Agent 后创建实验，建立版本回归基线。" actionLabel="创建实验" onAction={() => setCreateOpen(true)} />}
    </PageSection>
    <Drawer title="创建离线实验" width={Math.min(980, window.innerWidth - 24)} open={createOpen} onClose={() => setCreateOpen(false)} destroyOnHidden>
      <CreateExperimentPanel projectId={projectId} onCreated={() => setCreateOpen(false)} />
    </Drawer>
  </div>;
}
