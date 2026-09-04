import { useMemo, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { Alert, Badge, Button, Card, Descriptions, Drawer, Empty, Input, List, Modal, Select, Space, Tabs, Tag, Typography } from "antd";
import { ExperimentOutlined, PlayCircleOutlined, PlusOutlined, ReloadOutlined, RocketOutlined, SettingOutlined } from "@ant-design/icons";
import { useBenchmarkCatalog, useBenchmarkSuites, useBenchmarkTargets, useBenchmarkWorkers, useDatasetCases, useExperiments } from "../../api/hooks";
import type { BenchmarkAgentArtifact, BenchmarkCatalog, BenchmarkRunTemplate, BenchmarkSuite, BenchmarkWorker, DatasetCase, Experiment } from "../../api/types";
import { ErrorState } from "../../components/ui/ErrorState";
import { DataTable } from "../../components/ui/DataTable";
import { PageHeader } from "../../components/ui/PageHeader";
import { PageSection } from "../../components/ui/PageSection";
import { StatusBadge, statusLabel } from "../../components/ui/StatusBadge";
import { CreateBenchmarkExperimentPanel } from "./CreateBenchmarkExperimentPanel";
import { RegisterBenchmarkSuitePanel } from "./RegisterBenchmarkSuitePanel";
import { executionKindLabel, shortCommit } from "./benchmark-utils";

const { Paragraph, Text, Title } = Typography;
const BENCHMARK_TABS = ["experiments", "catalog", "tasks", "workers"] as const;
type BenchmarkTab = (typeof BENCHMARK_TABS)[number];

function isBenchmarkTab(value: string | null): value is BenchmarkTab {
  return Boolean(value && (BENCHMARK_TABS as readonly string[]).includes(value));
}

function caseTaskID(benchmarkCase: DatasetCase) {
  const metadata = benchmarkCase.metadata;
  if (metadata && typeof metadata === "object" && !Array.isArray(metadata)) {
    const taskID = (metadata as Record<string, unknown>).task_id;
    if (typeof taskID === "string" && taskID.trim()) return taskID;
  }
  return benchmarkCase.id;
}

function caseMetadata(benchmarkCase: DatasetCase) {
  return benchmarkCase.metadata && typeof benchmarkCase.metadata === "object" && !Array.isArray(benchmarkCase.metadata)
    ? benchmarkCase.metadata as Record<string, unknown>
    : {};
}

function caseFitsCloudMachine(benchmarkCase: DatasetCase) {
  const environment = caseMetadata(benchmarkCase).environment;
  if (!environment || typeof environment !== "object" || Array.isArray(environment)) return true;
  const values = environment as Record<string, unknown>;
  return !(
    (typeof values.cpus === "number" && values.cpus > 4) ||
    (typeof values.memory_mb === "number" && values.memory_mb > 4096) ||
    (typeof values.storage_mb === "number" && values.storage_mb > 40960) ||
    (typeof values.gpus === "number" && values.gpus > 0)
  );
}

function resourceTags(benchmarkCase: DatasetCase) {
  const environment = caseMetadata(benchmarkCase).environment;
  if (!environment || typeof environment !== "object" || Array.isArray(environment)) return [];
  const values = environment as Record<string, unknown>;
  const tags: string[] = [];
  if (typeof values.cpus === "number") tags.push(`${values.cpus}C`);
  if (typeof values.memory_mb === "number") tags.push(`${values.memory_mb / 1024}G RAM`);
  if (typeof values.storage_mb === "number") tags.push(`${values.storage_mb / 1024}G disk`);
  if (typeof values.gpus === "number" && values.gpus > 0) tags.push(`${values.gpus} GPU`);
  return tags;
}

function BenchmarkTaskCatalog({ suite }: { suite: BenchmarkSuite }) {
  const casesQuery = useDatasetCases(suite.dataset_id);
  const [detailCase, setDetailCase] = useState<DatasetCase>();
  const [search, setSearch] = useState("");
  const cases = casesQuery.data ?? [];
  const filteredCases = cases.filter((benchmarkCase) => {
    const metadata = caseMetadata(benchmarkCase);
    const haystack = [caseTaskID(benchmarkCase), metadata.category, metadata.subcategory, ...(Array.isArray(metadata.tags) ? metadata.tags : [])]
      .filter((value): value is string => typeof value === "string")
      .join(" ")
      .toLowerCase();
    return haystack.includes(search.trim().toLowerCase());
  });

  return (
    <Card size="small" className="benchmark-task-catalog">
      <Space orientation="vertical" size={12} style={{ width: "100%" }}>
        <div className="benchmark-tab-section__heading">
          <div>
            <Text strong>题目目录</Text>
            <Text type="secondary">从当前 Suite 的固定数据集选择任务，执行前会再次进行资源预检。</Text>
          </div>
          <Space wrap>
            <Tag color="blue">{suite.name}</Tag>
            <Tag>{suite.version}</Tag>
            <Badge count={cases.length} showZero color="#0f8a72" />
          </Space>
        </div>
        {casesQuery.isLoading ? <Text type="secondary">正在读取题目目录…</Text> : cases.length ? (
          <>
            <Input.Search allowClear placeholder="按任务 ID、分类或标签筛选" onChange={(event) => setSearch(event.target.value)} />
            <List
              size="small"
              dataSource={filteredCases}
              locale={{ emptyText: "没有匹配的题目" }}
              renderItem={(benchmarkCase) => (
                <List.Item actions={[<Button key="detail" type="link" onClick={() => setDetailCase(benchmarkCase)}>查看详情</Button>]}>
                  <div className="benchmark-task-row">
                    <Text code className="benchmark-task-row__id">{caseTaskID(benchmarkCase)}</Text>
                    <Space wrap size={[4, 4]}>
                      {resourceTags(benchmarkCase).map((tag) => <Tag key={tag}>{tag}</Tag>)}
                      {!caseFitsCloudMachine(benchmarkCase) ? <Tag color="warning">超出 4C/4G/40G</Tag> : null}
                    </Space>
                  </div>
                </List.Item>
              )}
            />
          </>
        ) : <Alert type="warning" showIcon message="尚未导入题目 case" description="同步 Terminal-Bench task metadata 后，这里会显示可选题目。" />}
      </Space>
      <Modal title={`题目详情 · ${detailCase ? caseTaskID(detailCase) : ""}`} open={Boolean(detailCase)} onCancel={() => setDetailCase(undefined)} footer={null} width={760} className="benchmark-task-detail-modal">
        {detailCase ? (() => {
          const metadata = caseMetadata(detailCase);
          const verifier = metadata.verifier && typeof metadata.verifier === "object" && !Array.isArray(metadata.verifier) ? metadata.verifier as Record<string, unknown> : {};
          const environment = metadata.environment && typeof metadata.environment === "object" && !Array.isArray(metadata.environment) ? metadata.environment as Record<string, unknown> : {};
          return <Space orientation="vertical" size={12} style={{ width: "100%" }}>
            <Descriptions bordered size="small" column={1}>
              <Descriptions.Item label="任务 ID"><Text code>{caseTaskID(detailCase)}</Text></Descriptions.Item>
              <Descriptions.Item label="任务名称">{typeof metadata.task_name === "string" ? metadata.task_name : "未提供"}</Descriptions.Item>
              <Descriptions.Item label="分类 / 标签">{[metadata.category, metadata.subcategory, ...(Array.isArray(metadata.tags) ? metadata.tags : [])].filter((item): item is string => typeof item === "string" && item.trim().length > 0).map((item) => <Tag key={item}>{item}</Tag>)}</Descriptions.Item>
              <Descriptions.Item label="输入题目"><pre className="benchmark-task-detail__pre">{typeof detailCase.input === "string" ? detailCase.input : JSON.stringify(detailCase.input, null, 2)}</pre></Descriptions.Item>
            </Descriptions>
            <Card size="small" title="资源与时间预算">
              <Descriptions size="small" column={2}>
                <Descriptions.Item label="CPU">{typeof environment.cpus === "number" ? `${environment.cpus} vCPU` : "未声明"}</Descriptions.Item>
                <Descriptions.Item label="内存">{typeof environment.memory_mb === "number" ? `${environment.memory_mb / 1024} GB` : "未声明"}</Descriptions.Item>
                <Descriptions.Item label="磁盘">{typeof environment.storage_mb === "number" ? `${environment.storage_mb / 1024} GB` : "未声明"}</Descriptions.Item>
                <Descriptions.Item label="GPU">{typeof environment.gpus === "number" ? environment.gpus : "未声明"}</Descriptions.Item>
                <Descriptions.Item label="Verifier 超时">{typeof metadata.verifier_timeout_sec === "number" ? `${metadata.verifier_timeout_sec}s` : "未声明"}</Descriptions.Item>
                <Descriptions.Item label="Agent 超时">{typeof metadata.agent_timeout_sec === "number" ? `${metadata.agent_timeout_sec}s` : "未声明"}</Descriptions.Item>
              </Descriptions>
              {!caseFitsCloudMachine(detailCase) ? <Alert type="warning" showIcon message="当前 4 核 4G / 40G 服务器不满足该题目的声明资源" description="该题目仍会保留在目录中，但创建或执行前应切换到更高规格 Worker。" /> : null}
            </Card>
            <Card className="benchmark-verifier-card" size="small" title="Verifier 具体方式">
              <Descriptions size="small" column={1}>
                <Descriptions.Item label="入口文件"><Text code>{typeof verifier.entrypoint === "string" ? verifier.entrypoint : "tests/test.sh"}</Text></Descriptions.Item>
                <Descriptions.Item label="执行命令"><Text code>{typeof verifier.command === "string" ? verifier.command : "由题目镜像执行官方 verifier"}</Text></Descriptions.Item>
                <Descriptions.Item label="检查项目"><span className="benchmark-verifier-value">{Array.isArray(verifier.checks) && verifier.checks.length ? verifier.checks.map((item) => <div key={String(item)}>• {String(item)}</div>) : "读取官方 reward.txt/reward.json"}</span></Descriptions.Item>
                <Descriptions.Item label="预装依赖"><span className="benchmark-verifier-value benchmark-verifier-tags">{Array.isArray(verifier.dependencies) && verifier.dependencies.length ? verifier.dependencies.map((item) => <Tag key={String(item)}>{String(item)}</Tag>) : "由题目 verifier 镜像提供"}</span></Descriptions.Item>
                <Descriptions.Item label="运行时策略"><span className="benchmark-verifier-value">{typeof verifier.runtime_install_policy === "string" ? verifier.runtime_install_policy : "静态依赖在镜像构建阶段安装；运行时不安装依赖。"}</span></Descriptions.Item>
              </Descriptions>
            </Card>
          </Space>;
        })() : null}
      </Modal>
    </Card>
  );
}

function SuiteCard({ suite, onLaunch }: { suite: BenchmarkSuite; onLaunch: (suite: BenchmarkSuite) => void }) {
  return (
    <article className="benchmark-suite-card">
      <div className="benchmark-suite-card__masthead"><div><span className="benchmark-kicker">{suite.harness_kind}</span><Title level={4}>{suite.name}</Title></div><Tag color="geekblue">{suite.version}</Tag></div>
      <Paragraph className="benchmark-suite-card__description" ellipsis={{ rows: 3, expandable: "collapsible" }}>{suite.description || "此套件尚未补充说明。"}</Paragraph>
      <div className="benchmark-suite-card__facts"><span><strong>执行</strong>{executionKindLabel(suite.execution_kind)}</span><span><strong>来源</strong>{shortCommit(suite.source_commit)}</span><span><strong>能力</strong>{suite.required_capabilities?.length || 0} 项</span></div>
      {suite.required_capabilities?.length ? <Space wrap size={[4, 4]} className="benchmark-suite-card__capabilities">{suite.required_capabilities.map((capability) => <Tag key={capability}>{capability}</Tag>)}</Space> : <Text type="secondary" className="benchmark-suite-card__capabilities">没有额外能力声明</Text>}
      <Button type="primary" icon={<PlayCircleOutlined />} onClick={() => onLaunch(suite)}>配置评测</Button>
    </article>
  );
}

function RunTemplateCard({ template, artifact, catalog, onLaunch }: { template: BenchmarkRunTemplate; artifact?: BenchmarkAgentArtifact; catalog: BenchmarkCatalog; onLaunch: (template: BenchmarkRunTemplate) => void }) {
  const profile = (id: string) => catalog.profiles.find((item) => item.id === id);
  const model = profile(template.model_profile_id);
  const execution = profile(template.execution_profile_id);
  return (
    <article className="benchmark-suite-card benchmark-suite-card--template">
      <div className="benchmark-suite-card__masthead"><div><span className="benchmark-kicker">immutable run template</span><Title level={4}>{template.display_name || template.id}</Title></div><Tag color="purple">{artifact?.framework || "Agent"}</Tag></div>
      <div className="benchmark-suite-card__facts"><span><strong>Artifact</strong>{template.agent_artifact_id}</span><span><strong>模型</strong>{model?.display_name || template.model_profile_id}</span><span><strong>Trial</strong>每任务 {template.trials_per_case} 次</span></div>
      <Space wrap size={[4, 4]} className="benchmark-suite-card__capabilities"><Tag>{template.suite_id}</Tag><Tag>{execution?.display_name || template.execution_profile_id}</Tag>{artifact?.image_digest ? <Tag color="blue">{artifact.image_digest.slice(0, 19)}…</Tag> : null}</Space>
      <Button type="primary" icon={<PlayCircleOutlined />} onClick={() => onLaunch(template)}>使用模板创建</Button>
    </article>
  );
}

function isBenchmarkExperiment(experiment: Experiment) {
  return Boolean(experiment.benchmark_suite_id || experiment.benchmark_key);
}

function BenchmarkExperiments({ experiments, loading, onOpen, onCreate }: { experiments: Experiment[]; loading: boolean; onOpen: (experimentId: string) => void; onCreate: () => void }) {
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("all");
  const benchmarkExperiments = useMemo(() => experiments.filter(isBenchmarkExperiment).sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()), [experiments]);
  const statuses = useMemo(() => Array.from(new Set(benchmarkExperiments.map((item) => item.status))).sort(), [benchmarkExperiments]);
  const filtered = benchmarkExperiments.filter((experiment) => {
    const query = search.trim().toLowerCase();
    const haystack = [experiment.id, experiment.target_name, experiment.target_version, experiment.benchmark_key, experiment.benchmark_version].filter(Boolean).join(" ").toLowerCase();
    return (status === "all" || experiment.status === status) && (!query || haystack.includes(query));
  });

  return <PageSection className="benchmark-workspace-section" title="实验记录" description="集中查看所有 Benchmark 实验，运行中的任务和最近的回归结果优先展示。" extra={<Space wrap><Tag color="blue">Benchmark only</Tag><Badge count={benchmarkExperiments.length} showZero color="#2457d6" /></Space>}>
    <div className="benchmark-list-toolbar">
      <Input.Search allowClear value={search} onChange={(event) => setSearch(event.target.value)} placeholder="按实验 ID、Benchmark 或 Agent 搜索" />
      <Select value={status} onChange={setStatus} options={[{ value: "all", label: "全部状态" }, ...statuses.map((value) => ({ value, label: statusLabel(value) }))]} />
    </div>
    {loading ? <div className="benchmark-loading">正在读取实验记录…</div> : filtered.length ? <DataTable<Experiment> rowKey="id" dataSource={filtered} pagination={{ pageSize: 12 }} columns={[
      { title: "Benchmark / 版本", key: "benchmark", render: (_: unknown, record) => <Space orientation="vertical" size={1}><Text strong>{record.benchmark_key || "未命名 Benchmark"}</Text><Text type="secondary">{record.benchmark_version || "未标记版本"}</Text></Space> },
      { title: "目标 Agent", key: "agent", render: (_: unknown, record) => <Space orientation="vertical" size={1}><Text strong>{record.target_name}</Text><Text type="secondary">{record.target_version || "未标记版本"}</Text></Space> },
      { title: "任务 / Trial", key: "cases", render: (_: unknown, record) => `${record.total_cases} 题 · 每题 ${record.trials_per_case || 1} 次` },
      { title: "结果", key: "result", render: (_: unknown, record) => <span className="benchmark-result-inline">{record.total_cases ? `${record.passed_cases} / ${record.total_cases} 通过` : "等待结果"}{record.aggregate_score === undefined ? "" : ` · ${record.aggregate_score.toFixed(3)}`}</span> },
      { title: "状态", dataIndex: "status", render: (value: string) => <StatusBadge status={value} /> },
      { title: "创建时间", dataIndex: "created_at", render: (value: string) => new Date(value).toLocaleString("zh-CN") },
      { title: "操作", key: "action", render: (_: unknown, record) => <Button type="link" onClick={() => onOpen(record.id)}>查看详情</Button> },
    ]} /> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={benchmarkExperiments.length ? "没有符合筛选条件的实验" : "还没有 Benchmark 实验"}><Button type="primary" icon={<RocketOutlined />} onClick={onCreate}>创建第一个基准实验</Button></Empty>}
  </PageSection>;
}

function BenchmarkWorkers({ workers, loading, isError, onRetry }: { workers: BenchmarkWorker[]; loading: boolean; isError: boolean; onRetry: () => void }) {
  const now = Date.now();
  const activeWorkers = workers.filter((worker) => new Date(worker.expires_at).getTime() > now);
  const kinds = Array.from(new Set(activeWorkers.flatMap((worker) => worker.execution_kinds))).sort();
  const totalCapacity = activeWorkers.reduce((total, worker) => total + worker.capacity, 0);
  return <PageSection className="benchmark-workspace-section" title="执行资源" description="Worker 通过短租约上报在线状态；这里只展示资源可用性，不直接启停 Worker。" extra={<Space wrap><Tag color={activeWorkers.length ? "success" : "default"}>{activeWorkers.length} 在线</Tag><Tag>{totalCapacity} 容量</Tag></Space>}>
    {loading ? <div className="benchmark-loading">正在读取 Worker 状态…</div> : isError ? <Alert type="warning" showIcon message="Worker 状态暂不可用" description="控制面未返回执行资源信息，不影响查看基准库和历史实验。" action={<Button size="small" icon={<ReloadOutlined />} onClick={onRetry}>重试</Button>} /> : activeWorkers.length ? <>
      <div className="benchmark-worker-overview"><span><strong>{activeWorkers.length}</strong> 在线 Worker</span><span><strong>{kinds.length}</strong> 执行环境</span><span><strong>{totalCapacity}</strong> 广告容量</span></div>
      <div className="benchmark-worker-grid">{kinds.map((kind) => { const matching = activeWorkers.filter((worker) => worker.execution_kinds.includes(kind)); const capacity = matching.reduce((total, worker) => total + worker.capacity, 0); return <Card size="small" key={kind} className="benchmark-worker-card"><div className="benchmark-worker-card__header"><div><span className="benchmark-kicker">execution kind</span><Title level={5}>{executionKindLabel(kind)}</Title></div><Tag color="success">可用</Tag></div><div className="benchmark-worker-card__stats"><span><strong>{matching.length}</strong> Worker</span><span><strong>{capacity}</strong> 容量</span></div><Space wrap size={[4, 4]}>{matching.map((worker) => <Tag key={worker.name}>{worker.name}{worker.version ? ` · ${worker.version}` : ""}</Tag>)}</Space></Card>; })}</div>
      <div className="benchmark-worker-list">{activeWorkers.map((worker) => <div key={worker.name} className="benchmark-worker-row"><Space><span className="benchmark-worker-dot" /><Text strong>{worker.name}</Text><Tag>{worker.version || "未标记版本"}</Tag></Space><Space wrap><Text type="secondary">{worker.execution_kinds.map(executionKindLabel).join(" · ")}</Text><Text type="secondary">容量 {worker.capacity}</Text><Text type="secondary">心跳 {new Date(worker.last_seen_at).toLocaleString("zh-CN")}</Text></Space></div>)}</div>
    </> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前没有在线 Benchmark Worker" />}
  </PageSection>;
}

export function BenchmarkCatalogPage() {
  const { projectId = "" } = useParams<{ projectId: string }>();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const activeTab: BenchmarkTab = isBenchmarkTab(searchParams.get("tab")) ? searchParams.get("tab") as BenchmarkTab : "experiments";
  const [launchSuite, setLaunchSuite] = useState<BenchmarkSuite>();
  const [launchTemplate, setLaunchTemplate] = useState<BenchmarkRunTemplate>();
  const [registerSuiteOpen, setRegisterSuiteOpen] = useState(false);
  const [createChooserOpen, setCreateChooserOpen] = useState(false);
  const [taskSuiteId, setTaskSuiteId] = useState<string>();
  const suitesQuery = useBenchmarkSuites(projectId);
  const targetsQuery = useBenchmarkTargets(projectId);
  const catalogQuery = useBenchmarkCatalog(projectId);
  const experimentsQuery = useExperiments(projectId);
  const workersQuery = useBenchmarkWorkers(projectId);
  const suites = suitesQuery.data ?? [];
  const targets = targetsQuery.data ?? [];
  const catalog = catalogQuery.data;
  const templates = catalog?.run_templates ?? [];
  const selectedTaskSuite = suites.find((suite) => suite.id === taskSuiteId) ?? suites[0];
  const executionKinds = useMemo(() => new Set(suites.map((suite) => suite.execution_kind)).size, [suites]);

  const changeTab = (tab: string) => {
    const next = new URLSearchParams(searchParams);
    next.set("tab", isBenchmarkTab(tab) ? tab : "experiments");
    setSearchParams(next);
  };
  const openCreateChooser = () => setCreateChooserOpen(true);
  const selectTemplate = (template: BenchmarkRunTemplate) => { setCreateChooserOpen(false); setLaunchSuite(undefined); setLaunchTemplate(template); };
  const selectSuite = (suite: BenchmarkSuite) => { setCreateChooserOpen(false); setLaunchTemplate(undefined); setLaunchSuite(suite); };
  const createDrawerOpen = Boolean(launchSuite || launchTemplate);

  if (suitesQuery.isError) return <ErrorState title="无法加载基准套件" description="Benchmark catalog 尚不可用。请确认控制面已提供 suite API。" onRetry={() => suitesQuery.refetch()} />;

  const experimentContent = <BenchmarkExperiments experiments={experimentsQuery.data ?? []} loading={experimentsQuery.isLoading} onOpen={(experimentId) => navigate(`/projects/${projectId}/benchmarks/experiments/${experimentId}`)} onCreate={openCreateChooser} />;
  const catalogContent = <>
    <PageSection className="benchmark-workspace-section" title="已同步的 Run Template" description="推荐入口：Template 冻结 Agent artifact、模型/Agent/执行 profile、任务筛选和 Trial，创建后不会重新读取可变 catalog。" extra={<Space wrap><Tag color="blue">{catalog?.revision || "catalog 未同步"}</Tag><Badge count={templates.length} showZero color="#7b61ff" /></Space>}>
      {catalogQuery.isLoading ? <div className="benchmark-loading">正在读取 catalog snapshot…</div> : templates.length ? <div className="benchmark-suite-grid">{templates.map((template) => <RunTemplateCard key={template.id} template={template} catalog={catalog!} artifact={catalog?.artifacts.find((artifact) => artifact.id === template.agent_artifact_id)} onLaunch={selectTemplate} />)}</div> : <Alert type="info" showIcon message="当前项目尚未同步 Benchmark catalog" description="可以继续注册兼容版 Suite；正式运行请先同步版本化 Template。" />}
    </PageSection>
    <PageSection className="benchmark-workspace-section" title="兼容版 Benchmark Suite" description="Suite 是项目级基准定义，适合维护 legacy 流程；创建实验前仍会进行 Worker 和 Agent 预检。" extra={<Badge count={suites.length} showZero color="#2457d6" />}>
      {suitesQuery.isLoading ? <div className="benchmark-loading">正在读取基准套件…</div> : suites.length ? <div className="benchmark-suite-grid">{suites.map((suite) => <SuiteCard key={suite.id} suite={suite} onLaunch={selectSuite} />)}</div> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前项目还没有可运行的基准套件"><Button type="primary" icon={<PlusOutlined />} onClick={() => setRegisterSuiteOpen(true)}>注册第一个基准套件</Button></Empty>}
    </PageSection>
  </>;
  const tasksContent = <PageSection className="benchmark-workspace-section" title="题目目录与 Verifier" description="一次只查看一个 Suite 的题目，避免多个数据集目录重复铺开。详情只展示输入和安全的 verifier 摘要。" extra={<Button icon={<SettingOutlined />} onClick={() => setRegisterSuiteOpen(true)}>管理 Suite</Button>}>
    {suites.length ? <><div className="benchmark-task-selector"><Text strong>当前基准</Text><Select showSearch optionFilterProp="label" value={selectedTaskSuite?.id} onChange={setTaskSuiteId} options={suites.map((suite) => ({ value: suite.id, label: `${suite.name} · ${suite.version}` }))} /></div>{selectedTaskSuite ? <BenchmarkTaskCatalog suite={selectedTaskSuite} /> : null}</> : <Empty description="暂无题目目录" />}
  </PageSection>;
  const workerContent = <BenchmarkWorkers workers={workersQuery.data ?? []} loading={workersQuery.isLoading} isError={workersQuery.isError} onRetry={() => workersQuery.refetch()} />;

  return <div className="benchmark-page">
    <PageHeader eyebrow="离线评测 / 官方 Harness" title="基准评测" description="用固定版本、官方环境和独立试验管理 Agent 能力回归。" extra={<Space wrap><Button icon={<ExperimentOutlined />} onClick={() => navigate(`/projects/${projectId}/experiments`)}>查看通用实验</Button><Button icon={<PlusOutlined />} onClick={() => setRegisterSuiteOpen(true)}>注册基准套件</Button><Button type="primary" icon={<RocketOutlined />} onClick={openCreateChooser} disabled={!templates.length && !suites.length}>运行新评测</Button></Space>} />
    <section className="benchmark-hero benchmark-hero--compact" aria-label="Benchmark overview"><div className="benchmark-hero__copy"><span className="benchmark-kicker">Evidence, not a demo</span><h2>让每一次通过，都能回到同一套环境里复现。</h2><p>把实验记录、版本化基准、任务目录和执行资源放在同一个管理入口。</p></div><div className="benchmark-hero__ledger"><span><strong>{experimentsQuery.data?.filter(isBenchmarkExperiment).length ?? 0}</strong> Benchmark 实验</span><span><strong>{templates.length}</strong> 正式模板</span><span><strong>{executionKinds}</strong> 执行环境</span></div></section>
    <Tabs className="benchmark-workspace-tabs" activeKey={activeTab} onChange={changeTab} items={[{ key: "experiments", label: "实验记录", children: experimentContent }, { key: "catalog", label: "基准库", children: catalogContent }, { key: "tasks", label: "题目目录", children: tasksContent }, { key: "workers", label: "执行资源", children: workerContent }]} />
    <Modal title="选择评测基准" open={createChooserOpen} onCancel={() => setCreateChooserOpen(false)} footer={null} className="benchmark-create-chooser">
      <div className="benchmark-create-chooser__intro">
        <Text type="secondary">先选择要运行的固定版本，再配置 Agent、任务和预检。不会隐式使用列表中的第一项。</Text>
      </div>
      <div className="benchmark-create-choice-list">
        {templates.length ? <section className="benchmark-create-choice-group" aria-labelledby="benchmark-template-choices">
          <div className="benchmark-create-choice-group__heading"><div><span className="benchmark-kicker">recommended</span><Text strong id="benchmark-template-choices">正式 Run Template</Text></div><Tag color="purple">{templates.length} 个</Tag></div>
          {templates.map((template) => {
            const artifact = catalog?.artifacts.find((item) => item.id === template.agent_artifact_id);
            return <Card key={template.id} size="small" hoverable className="benchmark-create-choice" onClick={() => selectTemplate(template)}>
              <div className="benchmark-create-choice__body"><div className="benchmark-create-choice__copy"><Text strong>{template.display_name || template.id}</Text><Text type="secondary">{template.suite_id} · {artifact?.framework || "Agent"} · 每题 {template.trials_per_case} 次</Text><Text type="secondary" className="benchmark-create-choice__mono">{template.id}</Text></div><Button type="link" aria-label={`选择模板 ${template.display_name || template.id}`}>选择 →</Button></div>
            </Card>;
          })}
        </section> : null}
        {suites.length ? <section className="benchmark-create-choice-group" aria-labelledby="benchmark-suite-choices">
          <div className="benchmark-create-choice-group__heading"><div><span className="benchmark-kicker">legacy compatible</span><Text strong id="benchmark-suite-choices">兼容版 Benchmark Suite</Text></div><Tag color="blue">{suites.length} 个</Tag></div>
          {suites.map((suite) => <Card key={suite.id} size="small" hoverable className="benchmark-create-choice" onClick={() => selectSuite(suite)}>
            <div className="benchmark-create-choice__body"><div className="benchmark-create-choice__copy"><Text strong>{suite.name}</Text><Text type="secondary">{executionKindLabel(suite.execution_kind)} · {suite.version} · {suite.required_capabilities?.length || 0} 项能力</Text><Text type="secondary" className="benchmark-create-choice__mono">{suite.benchmark_key}</Text></div><Button type="link" aria-label={`选择 Suite ${suite.name}`}>选择 →</Button></div>
          </Card>)}
        </section> : null}
        {!templates.length && !suites.length ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无可运行的基准" /> : null}
      </div>
    </Modal>
    <Drawer title="注册基准套件" open={registerSuiteOpen} onClose={() => setRegisterSuiteOpen(false)} size="large" destroyOnHidden><RegisterBenchmarkSuitePanel projectId={projectId} onManageDatasets={() => { setRegisterSuiteOpen(false); navigate(`/projects/${projectId}/datasets`); }} onCreated={(suite) => { setRegisterSuiteOpen(false); selectSuite(suite); }} /></Drawer>
    <Drawer title="创建基准实验" open={createDrawerOpen} onClose={() => { setLaunchSuite(undefined); setLaunchTemplate(undefined); }} size="large" destroyOnHidden><CreateBenchmarkExperimentPanel projectId={projectId} suites={suites} targets={targets} catalog={catalog} initialSuiteId={launchSuite?.id} initialRunTemplateId={launchTemplate?.id} onCreated={(experimentId) => { setLaunchSuite(undefined); setLaunchTemplate(undefined); navigate(`/projects/${projectId}/benchmarks/experiments/${experimentId}`); }} /></Drawer>
  </div>;
}
