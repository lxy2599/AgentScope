import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Alert, Badge, Button, Drawer, Empty, Space, Tag, Typography } from "antd";
import { ExperimentOutlined, PlayCircleOutlined, PlusOutlined, RocketOutlined } from "@ant-design/icons";
import { useBenchmarkCatalog, useBenchmarkSuites, useBenchmarkTargets } from "../../api/hooks";
import type { BenchmarkAgentArtifact, BenchmarkCatalog, BenchmarkRunTemplate, BenchmarkSuite } from "../../api/types";
import { ErrorState } from "../../components/ui/ErrorState";
import { PageHeader } from "../../components/ui/PageHeader";
import { PageSection } from "../../components/ui/PageSection";
import { CreateBenchmarkExperimentPanel } from "./CreateBenchmarkExperimentPanel";
import { RegisterBenchmarkSuitePanel } from "./RegisterBenchmarkSuitePanel";
import { executionKindLabel, shortCommit } from "./benchmark-utils";

const { Paragraph, Text, Title } = Typography;

function SuiteCard({ suite, onLaunch }: { suite: BenchmarkSuite; onLaunch: (suite: BenchmarkSuite) => void }) {
  return (
    <article className="benchmark-suite-card">
      <div className="benchmark-suite-card__masthead">
        <div>
          <span className="benchmark-kicker">{suite.harness_kind}</span>
          <Title level={4}>{suite.name}</Title>
        </div>
        <Tag color="geekblue">{suite.version}</Tag>
      </div>
      <Paragraph className="benchmark-suite-card__description" ellipsis={{ rows: 3, expandable: "collapsible" }}>
        {suite.description || "此套件尚未补充说明。"}
      </Paragraph>
      <div className="benchmark-suite-card__facts">
        <span><strong>执行</strong>{executionKindLabel(suite.execution_kind)}</span>
        <span><strong>来源</strong>{shortCommit(suite.source_commit)}</span>
        <span><strong>能力</strong>{suite.required_capabilities?.length || 0} 项</span>
      </div>
      {suite.required_capabilities?.length ? (
        <Space wrap size={[4, 4]} className="benchmark-suite-card__capabilities">
          {suite.required_capabilities.map((capability) => <Tag key={capability}>{capability}</Tag>)}
        </Space>
      ) : <Text type="secondary" className="benchmark-suite-card__capabilities">没有额外能力声明</Text>}
      <Button type="primary" icon={<PlayCircleOutlined />} onClick={() => onLaunch(suite)}>
        配置评测
      </Button>
    </article>
  );
}

function RunTemplateCard({ template, artifact, catalog, onLaunch }: { template: BenchmarkRunTemplate; artifact?: BenchmarkAgentArtifact; catalog: BenchmarkCatalog; onLaunch: (template: BenchmarkRunTemplate) => void }) {
  const profile = (id: string) => catalog.profiles.find((item) => item.id === id);
  const model = profile(template.model_profile_id);
  const execution = profile(template.execution_profile_id);
  return (
    <article className="benchmark-suite-card">
      <div className="benchmark-suite-card__masthead"><div><span className="benchmark-kicker">immutable run template</span><Title level={4}>{template.display_name || template.id}</Title></div><Tag color="purple">{artifact?.framework || "Agent"}</Tag></div>
      <div className="benchmark-suite-card__facts">
        <span><strong>Artifact</strong>{template.agent_artifact_id}</span>
        <span><strong>模型</strong>{model?.display_name || template.model_profile_id}</span>
        <span><strong>Trial</strong>每任务 {template.trials_per_case} 次</span>
      </div>
      <Space wrap size={[4, 4]} className="benchmark-suite-card__capabilities">
        <Tag>{template.suite_id}</Tag><Tag>{execution?.display_name || template.execution_profile_id}</Tag>
        {artifact?.image_digest ? <Tag color="blue">{artifact.image_digest.slice(0, 19)}…</Tag> : null}
      </Space>
      <Button type="primary" icon={<PlayCircleOutlined />} onClick={() => onLaunch(template)}>使用模板创建</Button>
    </article>
  );
}

export function BenchmarkCatalogPage() {
  const { projectId = "" } = useParams<{ projectId: string }>();
  const navigate = useNavigate();
  const [launchSuite, setLaunchSuite] = useState<BenchmarkSuite>();
  const [launchTemplate, setLaunchTemplate] = useState<BenchmarkRunTemplate>();
  const [registerSuiteOpen, setRegisterSuiteOpen] = useState(false);
  const suitesQuery = useBenchmarkSuites(projectId);
  const targetsQuery = useBenchmarkTargets(projectId);
  const catalogQuery = useBenchmarkCatalog(projectId);
  const suites = suitesQuery.data ?? [];
  const targets = targetsQuery.data ?? [];
  const catalog = catalogQuery.data;
  const templates = catalog?.run_templates ?? [];
  const executionKinds = useMemo(() => new Set(suites.map((suite) => suite.execution_kind)).size, [suites]);

  if (suitesQuery.isError) {
    return <ErrorState title="无法加载基准套件" description="Benchmark catalog 尚不可用。请确认控制面已提供 suite API。" onRetry={() => suitesQuery.refetch()} />;
  }

  return (
    <div className="benchmark-page">
      <PageHeader
        eyebrow="离线评测 / 官方 Harness"
        title="基准评测"
        description="以官方环境、固定版本和独立试验验证 Agent 的真实任务能力。"
        extra={
          <Space wrap>
            <Button icon={<ExperimentOutlined />} onClick={() => navigate(`/projects/${projectId}/experiments`)}>查看通用实验</Button>
            <Button icon={<PlusOutlined />} onClick={() => setRegisterSuiteOpen(true)}>注册基准套件</Button>
            <Button type="primary" icon={<RocketOutlined />} onClick={() => templates.length ? setLaunchTemplate(templates[0]) : setLaunchSuite(suites[0])} disabled={!templates.length && !suites.length}>创建基准实验</Button>
          </Space>
        }
      />

      <section className="benchmark-hero" aria-label="Benchmark overview">
        <div className="benchmark-hero__copy">
          <span className="benchmark-kicker">Evidence, not a demo</span>
          <h2>让每一次通过，都能回到同一套环境里复现。</h2>
          <p>套件固定任务与 verifier；平台负责预检、排队、追踪、制品和长期回归。</p>
        </div>
        <div className="benchmark-hero__ledger" aria-label="Benchmark summary">
          <span><strong>{templates.length}</strong> 同步模板</span>
          <span><strong>{suites.length}</strong> 已注册套件</span>
          <span><strong>{executionKinds}</strong> 执行环境</span>
        </div>
      </section>

      <PageSection
        title="已同步的 Run Template"
        description="推荐入口：模板冻结 Agent artifact digest、模型/Agent/执行 profile、任务筛选和 trial，创建实验后不会重新读取可变 catalog。"
        extra={<Space><Tag color="blue">{catalog?.revision || "catalog 未同步"}</Tag><Badge count={templates.length} showZero color="#7b61ff" /></Space>}
      >
        {catalogQuery.isLoading ? <div className="benchmark-loading">正在读取 Git catalog snapshot…</div> : templates.length ? (
          <div className="benchmark-suite-grid">
            {templates.map((template) => <RunTemplateCard key={template.id} template={template} catalog={catalog!} artifact={catalog?.artifacts.find((artifact) => artifact.id === template.agent_artifact_id)} onLaunch={setLaunchTemplate} />)}
          </div>
        ) : <Alert type="info" showIcon message="当前项目尚未同步 Benchmark catalog" description="可以继续注册 legacy suite；正式 OpenHands/OpenClaw Harbor 运行请先通过 benchmarkctl catalog-sync 同步版本化模板。" />}
      </PageSection>

      <PageSection
        title="可运行的基准套件"
        description="选择一个固定版本，随后在预检中确认目标 Agent、官方 harness 和 Runner 均已就绪。"
        extra={<Badge count={suites.length} showZero color="#2457d6" />}
      >
        {suitesQuery.isLoading ? <div className="benchmark-loading">正在读取 Benchmark catalog…</div> : suites.length ? (
          <div className="benchmark-suite-grid">
            {suites.map((suite) => <SuiteCard key={suite.id} suite={suite} onLaunch={setLaunchSuite} />)}
          </div>
        ) : (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description="当前项目还没有可运行的基准套件"
          >
            <Space orientation="vertical" size={10}>
              <Text type="secondary">先注册 Harbor、τ² 或企业内部套件，再从这里创建实验并在详情页启动。</Text>
              <Button type="primary" icon={<PlusOutlined />} onClick={() => setRegisterSuiteOpen(true)}>注册第一个基准套件</Button>
            </Space>
          </Empty>
        )}
      </PageSection>

      <Drawer
        title="注册基准套件"
        open={registerSuiteOpen}
        onClose={() => setRegisterSuiteOpen(false)}
        size="large"
        destroyOnHidden
      >
        <RegisterBenchmarkSuitePanel
          projectId={projectId}
          onManageDatasets={() => {
            setRegisterSuiteOpen(false);
            navigate(`/projects/${projectId}/datasets`);
          }}
          onCreated={(suite) => {
            setRegisterSuiteOpen(false);
            setLaunchSuite(suite);
          }}
        />
      </Drawer>

      <Drawer
        title="创建基准实验"
        open={Boolean(launchSuite || launchTemplate)}
        onClose={() => { setLaunchSuite(undefined); setLaunchTemplate(undefined); }}
        size="large"
        destroyOnHidden
      >
        <CreateBenchmarkExperimentPanel
          projectId={projectId}
          suites={suites}
          targets={targets}
          catalog={catalog}
          initialSuiteId={launchSuite?.id}
          initialRunTemplateId={launchTemplate?.id}
          onCreated={(experimentId) => {
            setLaunchSuite(undefined);
            setLaunchTemplate(undefined);
            navigate(`/projects/${projectId}/benchmarks/experiments/${experimentId}`);
          }}
        />
      </Drawer>
    </div>
  );
}
