import { useMemo } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Button, Skeleton, Space, Typography } from "antd";
import { ArrowRightOutlined, DatabaseOutlined, ExperimentOutlined, FormOutlined, PlusOutlined } from "@ant-design/icons";
import { useAgents, useDatasets, useExperiments, useProjectRuns } from "../../api/hooks";
import { DataTable } from "../../components/ui/DataTable";
import { EmptyState } from "../../components/ui/EmptyState";
import { PageHeader } from "../../components/ui/PageHeader";
import { PageSection } from "../../components/ui/PageSection";
import { StatusBadge } from "../../components/ui/StatusBadge";
import { TechnicalId } from "../../components/ui/TechnicalId";

const { Text } = Typography;

/** The project home is a decision workbench, not an asset-management catch-all. */
export function ProjectHomePage() {
  const { projectId = "" } = useParams<{ projectId: string }>();
  const navigate = useNavigate();
  const { data: runs = [], isLoading: runsLoading } = useProjectRuns(projectId);
  const { data: agents = [], isLoading: agentsLoading } = useAgents(projectId);
  const { data: datasets = [], isLoading: datasetsLoading } = useDatasets(projectId);
  const { data: experiments = [], isLoading: experimentsLoading } = useExperiments(projectId);

  const signals = useMemo(() => {
    const failedRuns = runs.filter((run) => ["failed", "error", "timeout"].includes(run.status.toLowerCase())).length;
    const activeRuns = runs.filter((run) => ["running", "pending", "queued"].includes(run.status.toLowerCase())).length;
    const pendingExperiments = experiments.filter((experiment) => ["pending", "queued", "running"].includes(experiment.status.toLowerCase())).length;
    return [
      { label: "需要关注的失败 Run", value: failedRuns, hint: failedRuns ? "优先查看错误根因" : "当前未发现失败运行", tone: failedRuns ? "danger" : "success", action: () => navigate(`/projects/${projectId}/runs`) },
      { label: "正在执行", value: activeRuns, hint: "实时状态持续更新", tone: activeRuns ? "warning" : "default", action: () => navigate(`/projects/${projectId}/active-runs`) },
      { label: "已注册 Agent", value: agents.length, hint: "实例与版本状态", tone: "default", action: () => navigate(`/projects/${projectId}/agents`) },
      { label: "可回归数据集", value: datasets.length, hint: "手工与线上 Gold 样本", tone: "default", action: () => navigate(`/projects/${projectId}/datasets`) },
      { label: "进行中的实验", value: pendingExperiments, hint: `${experiments.length} 个实验记录`, tone: pendingExperiments ? "warning" : "default", action: () => navigate(`/projects/${projectId}/experiments`) },
    ];
  }, [agents.length, datasets.length, experiments, navigate, projectId, runs]);

  const recentRuns = [...runs].sort((a, b) => +new Date(b.created_at) - +new Date(a.created_at)).slice(0, 6);
  const recentExperiments = [...experiments].sort((a, b) => +new Date(b.created_at) - +new Date(a.created_at)).slice(0, 5);
  const loading = runsLoading || agentsLoading || datasetsLoading || experimentsLoading;

  return (
    <div>
      <PageHeader
        eyebrow="项目工作台"
        title="把运行信号转化为质量决策"
        description="先处理失败和低质量运行，再将线上反馈沉淀为可回归的数据集与实验结论。"
        extra={<Space wrap><Button onClick={() => navigate(`/projects/${projectId}/settings`)}>接入与 Token</Button><Button type="primary" icon={<PlusOutlined />} onClick={() => navigate(`/projects/${projectId}/experiments`)}>创建实验</Button></Space>}
      />
      {loading ? <Skeleton active paragraph={{ rows: 8 }} /> : <>
        <div className="signal-overview">
          {signals.map((signal) => <button type="button" className={`signal-card signal-card--${signal.tone}`} key={signal.label} onClick={signal.action}>
            <span className="signal-card__label">{signal.label}</span><strong className="signal-card__value">{signal.value}</strong><span className="signal-card__hint">{signal.hint}</span>
          </button>)}
        </div>
        <div className="surface-grid surface-grid--2">
          <PageSection title="实时运行" description="优先处理仍在运行、等待或重试的任务" extra={<Button type="link" onClick={() => navigate(`/projects/${projectId}/active-runs`)}>查看实时运行 <ArrowRightOutlined /></Button>}>
            {runs.filter((run) => ["running", "pending", "queued"].includes(run.status.toLowerCase())).length ? <DataTable
              rowKey="id" pagination={false} dataSource={runs.filter((run) => ["running", "pending", "queued"].includes(run.status.toLowerCase())).slice(0, 5)}
              columns={[
                { title: "Agent", dataIndex: "agent_name", render: (value: string) => <Text strong>{value || "未命名 Agent"}</Text> },
                { title: "状态", dataIndex: "status", render: (value: string) => <StatusBadge status={value} /> },
                { title: "开始时间", dataIndex: "created_at", render: (value: string) => new Date(value).toLocaleString("zh-CN") },
                { title: "", key: "action", render: (_: unknown, record: { id: string }) => <Button type="link" onClick={() => navigate(`/runs/${record.id}`)}>诊断</Button> },
              ]}
            /> : <EmptyState title="没有正在执行的 Run" description="连接 Agent 后，实时运行会出现在这里。" />}
          </PageSection>
          <PageSection title="最近需要处理" description="失败运行、质量回归和人工标注构成线上闭环" extra={<Button type="link" onClick={() => navigate(`/projects/${projectId}/annotation-queues`)}>前往标注队列 <ArrowRightOutlined /></Button>}>
            {recentRuns.length ? <DataTable rowKey="id" pagination={false} dataSource={recentRuns}
              columns={[
                { title: "Agent / Trace", dataIndex: "agent_name", render: (value: string, record: any) => <Space direction="vertical" size={0}><Text strong>{value || "未命名 Agent"}</Text><TechnicalId value={record.trace_id} /></Space> },
                { title: "状态", dataIndex: "status", render: (value: string) => <StatusBadge status={value} /> },
                { title: "时间", dataIndex: "created_at", render: (value: string) => new Date(value).toLocaleString("zh-CN") },
                { title: "", key: "action", render: (_: unknown, record: { id: string }) => <Button type="link" onClick={() => navigate(`/runs/${record.id}`)}>查看</Button> },
              ]}
            /> : <EmptyState title="还没有运行记录" description="使用 Project Token 接入 SDK 后，这里会展示实时 Trace。" actionLabel="查看接入方式" onAction={() => navigate(`/projects/${projectId}/settings`)} />}
          </PageSection>
        </div>
        <div className="surface-grid surface-grid--3" style={{ marginTop: 16 }}>
          <PageSection title="质量资产" description="将高价值 Gold 回灌为回归样本" extra={<DatabaseOutlined />}><strong style={{ fontSize: 28 }}>{datasets.length}</strong><div><Text type="secondary">个数据集</Text></div><Button type="link" style={{ paddingLeft: 0 }} onClick={() => navigate(`/projects/${projectId}/datasets`)}>管理数据集 <ArrowRightOutlined /></Button></PageSection>
          <PageSection title="最近实验" description="用真实 Case 验证版本变化" extra={<ExperimentOutlined />}>
            {recentExperiments.length ? <Space direction="vertical" size={9} style={{ width: "100%" }}>{recentExperiments.slice(0, 3).map((experiment) => <div key={experiment.id} style={{ display: "flex", justifyContent: "space-between", gap: 8 }}><span><Text strong>{experiment.target_name}</Text><br /><Text type="secondary">{experiment.total_cases} 个用例</Text></span><StatusBadge status={experiment.status} /></div>)}</Space> : <Text type="secondary">创建首个实验，建立版本基线。</Text>}<br /><Button type="link" style={{ paddingLeft: 0 }} onClick={() => navigate(`/projects/${projectId}/experiments`)}>查看实验 <ArrowRightOutlined /></Button>
          </PageSection>
          <PageSection title="线上到离线闭环" description="抽样、自动评测、人工 Gold、回灌 Dataset" extra={<FormOutlined />}><ol style={{ margin: 0, paddingLeft: 18, color: "var(--as-muted)", lineHeight: 2 }}><li>配置 Sampling Rule</li><li>处理低分 Run</li><li>提交 Gold 标注</li><li>纳入离线回归</li></ol><Button type="link" style={{ paddingLeft: 0 }} onClick={() => navigate(`/projects/${projectId}/sampling-rules`)}>配置线上评测 <ArrowRightOutlined /></Button></PageSection>
        </div>
      </>}
    </div>
  );
}
