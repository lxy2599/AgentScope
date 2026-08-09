import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Alert, App, Button, Empty, Modal, Progress, Space, Tag, Tooltip, Typography } from "antd";
import { DownloadOutlined, EyeOutlined, FolderOpenOutlined, LinkOutlined, PlayCircleOutlined, ReloadOutlined } from "@ant-design/icons";
import { useBenchmarkExperimentReport, useLaunchBenchmarkExperiment } from "../../api/hooks";
import type { BenchmarkArtifact, BenchmarkExperimentLaunch, BenchmarkTrialResult } from "../../api/types";
import { DataTable } from "../../components/ui/DataTable";
import { ErrorState } from "../../components/ui/ErrorState";
import { PageHeader } from "../../components/ui/PageHeader";
import { PageSection } from "../../components/ui/PageSection";
import { StatusBadge } from "../../components/ui/StatusBadge";
import { TechnicalId } from "../../components/ui/TechnicalId";
import { displayMetricValue, executionKindLabel, formatPercent } from "./benchmark-utils";

const { Text } = Typography;

function ArtifactLinks({ artifacts = [] }: { artifacts?: BenchmarkArtifact[] }) {
  if (!artifacts.length) return <Text type="secondary">—</Text>;
  return (
    <Space size={[2, 4]} wrap>
      {artifacts.map((artifact, index) => {
        const href = artifact.url ?? artifact.uri;
        return href ? (
          <Tooltip key={`${artifact.name}-${index}`} title={artifact.kind || "评测制品"}>
            <a href={href} target="_blank" rel="noreferrer" className="benchmark-artifact-link">
              <DownloadOutlined /> {artifact.name}
            </a>
          </Tooltip>
        ) : <Tag key={`${artifact.name}-${index}`}>{artifact.name}</Tag>;
      })}
    </Space>
  );
}

function ResultTag({ passed, valid }: Pick<BenchmarkTrialResult, "passed" | "valid">) {
  if (valid === false) return <Tag color="warning">结果无效</Tag>;
  if (passed === true) return <Tag color="success">通过</Tag>;
  if (passed === false) return <Tag color="error">未通过</Tag>;
  return <Tag>待验证</Tag>;
}

const ACTIVE_LAUNCH_STATUSES = new Set(["starting", "launching", "running"]);

function launchStatusLabel(launch?: BenchmarkExperimentLaunch) {
  switch (launch?.status?.toLowerCase()) {
    case "not_started":
    case "ready":
      return "等待确认";
    case "starting":
    case "launching":
      return "正在启动";
    case "running":
      return "本机执行中";
    case "completed":
      return "本机执行已结束";
    case "failed":
      return "本机启动失败";
    case "unavailable":
      return "本机启动不可用";
    default:
      return launch?.status || "启动信息未提供";
  }
}

export function BenchmarkExperimentDetailPage() {
  const { projectId = "", experimentId = "" } = useParams<{ projectId: string; experimentId: string }>();
  const navigate = useNavigate();
  const reportQuery = useBenchmarkExperimentReport(projectId, experimentId);
  const launchMutation = useLaunchBenchmarkExperiment(projectId, experimentId);
  const { message } = App.useApp();
  const [launchConfirmOpen, setLaunchConfirmOpen] = useState(false);
  const [launchError, setLaunchError] = useState<string>();
  const report = reportQuery.data;

  const metricEntries = useMemo(
    () => Object.entries(report?.official_metrics ?? {}),
    [report?.official_metrics],
  );

  if (reportQuery.isLoading) return <div className="benchmark-loading">正在装载官方结果与试验记录…</div>;
  if (reportQuery.isError || !report) {
    return <ErrorState title="无法加载基准实验" description="报告尚不可用，或当前账户无权查看这个实验。" onRetry={() => reportQuery.refetch()} />;
  }

  const trialsPerCase = report.experiment.trials_per_case ?? 1;
  const officialScore = report.quality.official_score ?? report.experiment.aggregate_score;
  const completedJobs = report.execution.succeeded + report.execution.failed;
  const totalJobs = report.execution.total_jobs ?? report.execution.queued + report.execution.running + completedJobs;
  const completionPercent = totalJobs ? Math.round((completedJobs / totalJobs) * 100) : 0;
  const launch = report.launch;
  const launchStatus = launch?.status?.toLowerCase();
  const launchInProgress = Boolean(launchStatus && ACTIVE_LAUNCH_STATUSES.has(launchStatus));
  const canLaunchLocally = launch?.available === true && !launchInProgress;
  const unavailableReason = launch?.unavailable_reason
    ?? launch?.message
    ?? "当前控制面尚未提供此实验的本机启动能力。";

  const confirmLaunch = async () => {
    setLaunchError(undefined);
    try {
      const result = await launchMutation.mutateAsync();
      message.success(result.message || "已提交本机启动请求，正在等待执行器就绪。");
      setLaunchConfirmOpen(false);
      void reportQuery.refetch();
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : "无法提交本机启动请求。";
      setLaunchError(errorMessage);
      message.error(errorMessage);
    }
  };

  return (
    <div className="benchmark-page">
      <PageHeader
        eyebrow={`${report.suite.harness_kind} / ${report.suite.version}`}
        title={`${report.suite.name} · ${report.experiment.target_name}`}
        description="确认本机执行后，官方 verifier 的得分、试验数据、追踪与制品会集中保存在同一份报告中。"
        onBack={() => navigate(`/projects/${projectId}/benchmarks`)}
        extra={
          <Space wrap>
            <Button icon={<ReloadOutlined />} onClick={() => reportQuery.refetch()} loading={reportQuery.isFetching}>
              刷新状态
            </Button>
            <Button
              type="primary"
              icon={<PlayCircleOutlined />}
              disabled={!canLaunchLocally}
              loading={launchInProgress}
              onClick={() => {
                setLaunchError(undefined);
                setLaunchConfirmOpen(true);
              }}
            >
              {launchInProgress ? "本机启动中" : "在本机启动"}
            </Button>
            <StatusBadge status={report.experiment.status} />
          </Space>
        }
      />

      <PageSection
        title="本机执行"
        description="启动由当前 AgentScope 服务主机处理；浏览器不会收集、传输或保存任何模型、Agent 或访问凭据。"
        extra={<StatusBadge status={launch?.status} label={launchStatusLabel(launch)} />}
      >
        <div className="benchmark-local-launch">
          <div>
            <Text strong>{launchInProgress ? "执行器正在准备或运行" : canLaunchLocally ? "本机执行器已就绪" : "当前不能从此页面启动"}</Text>
            <div><Text type="secondary">{canLaunchLocally || launchInProgress ? launch?.message || "状态会在执行期间自动刷新。" : unavailableReason}</Text></div>
          </div>
          <Button
            type="primary"
            icon={<PlayCircleOutlined />}
            disabled={!canLaunchLocally}
            loading={launchInProgress}
            onClick={() => {
              setLaunchError(undefined);
              setLaunchConfirmOpen(true);
            }}
          >
            {launchInProgress ? "本机启动中" : "确认并启动"}
          </Button>
        </div>
        {!canLaunchLocally && !launchInProgress && (
          <Alert
            className="benchmark-local-launch__notice"
            type="info"
            showIcon
            message="本地启动当前不可用"
            description={unavailableReason}
          />
        )}
      </PageSection>

      <section className="benchmark-result-board" aria-label="Benchmark outcome">
        <div className="benchmark-result-board__score">
          <span className="benchmark-kicker">Official score</span>
          <strong>{officialScore === undefined ? "—" : officialScore.toFixed(3)}</strong>
          <span>{report.quality.valid_trials ?? 0} 个有效试验</span>
        </div>
        <div className="benchmark-result-board__metric">
          <span>Pass@1</span>
          <strong>{formatPercent(report.quality.pass_at_1)}</strong>
        </div>
        <div className="benchmark-result-board__metric">
          <span>Pass@{trialsPerCase}</span>
          <strong>{formatPercent(report.quality.pass_at_k)}</strong>
        </div>
        <div className="benchmark-result-board__progress">
          <span>执行进度 · {completedJobs}/{totalJobs || "—"} Job</span>
          <Progress percent={completionPercent} size="small" status={report.execution.failed ? "exception" : report.execution.running ? "active" : "normal"} />
        </div>
      </section>

      <div className="surface-grid surface-grid--2">
        <PageSection title="实验快照" description="结果始终关联到固定 suite、target 版本和执行环境。">
          <div className="benchmark-fact-list">
            <span><strong>目标版本</strong>{report.experiment.target_version || "未标记"}</span>
            <span><strong>执行环境</strong>{executionKindLabel(report.suite.execution_kind)}</span>
            <span><strong>独立试验</strong>每任务 {trialsPerCase} 次</span>
            <span><strong>技术 ID</strong><TechnicalId value={report.experiment.id} /></span>
          </div>
        </PageSection>
        <PageSection title="执行状态" description="基础设施失败与官方未通过会被分别保留，避免混淆结果。">
          <div className="benchmark-execution-grid">
            <span><strong>{report.execution.queued}</strong>排队</span>
            <span><strong>{report.execution.running}</strong>运行</span>
            <span><strong>{report.execution.succeeded}</strong>已完成</span>
            <span><strong>{report.execution.failed}</strong>执行失败</span>
          </div>
        </PageSection>
      </div>

      <PageSection
        title="官方指标"
        description="这些数值由 benchmark verifier 产出；平台不会改写其判定。"
        extra={<Tag color="blue">{metricEntries.length} 项</Tag>}
      >
        {metricEntries.length ? (
          <div className="benchmark-metric-grid">
            {metricEntries.map(([key, value]) => <div className="benchmark-metric" key={key}><span>{key}</span><strong>{displayMetricValue(value)}</strong></div>)}
          </div>
        ) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="官方 runner 尚未返回额外指标" />}
      </PageSection>

      <PageSection
        title="任务试验"
        description="每一行是一个独立 trial；可直接进入 Trace 或打开官方产物，定位失败原因。"
        extra={<Space><FolderOpenOutlined /><Text type="secondary">{report.trials.length} 条记录</Text></Space>}
      >
        {report.trials.length ? (
          <DataTable<BenchmarkTrialResult>
            rowKey="id"
            dataSource={report.trials}
            pagination={{ pageSize: 12 }}
            columns={[
              {
                title: "任务 / 试验",
                key: "task",
                render: (_: unknown, trial) => <Space orientation="vertical" size={1}><Text strong>{trial.task_id || "未命名任务"}</Text><Text type="secondary">Trial {trial.trial_index + 1}{trial.seed !== undefined ? ` · seed ${trial.seed}` : ""}</Text></Space>,
              },
              { title: "执行", dataIndex: "status", render: (status: string) => <StatusBadge status={status} /> },
              { title: "官方结果", key: "result", render: (_: unknown, trial) => <Space orientation="vertical" size={2}><ResultTag passed={trial.passed} valid={trial.valid} /><Text>{trial.official_score === undefined ? "—" : trial.official_score.toFixed(3)}</Text></Space> },
              { title: "时延", dataIndex: "latency_ms", render: (value?: number) => value === undefined ? "—" : `${value}ms` },
              { title: "错误分类", key: "error", render: (_: unknown, trial) => trial.error_type ? <Tooltip title={trial.error_message}><Tag color="error">{trial.error_type}</Tag></Tooltip> : <Text type="secondary">—</Text> },
              { title: "审计", key: "trace", render: (_: unknown, trial) => trial.run_id ? <Button type="link" size="small" icon={<EyeOutlined />} onClick={() => navigate(`/runs/${trial.run_id}`)}>Trace</Button> : <Text type="secondary">—</Text> },
              { title: "制品", key: "artifacts", render: (_: unknown, trial) => <ArtifactLinks artifacts={trial.artifacts} /> },
            ]}
          />
        ) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Runner 还没有提交试验记录" />}
      </PageSection>

      <Button icon={<LinkOutlined />} onClick={() => navigate(`/experiments/${report.experiment.id}`)}>打开通用实验报告</Button>

      <Modal
        title="确认在本机启动基准实验"
        open={launchConfirmOpen}
        onCancel={() => !launchMutation.isPending && setLaunchConfirmOpen(false)}
        onOk={confirmLaunch}
        confirmLoading={launchMutation.isPending}
        okText="启动本机执行"
        cancelText="暂不启动"
      >
        <Space orientation="vertical" size={12} style={{ width: "100%" }}>
          <Text>将在当前 AgentScope 服务主机上启动 <Text strong>{report.suite.name}</Text> 的官方执行环境，并运行 {report.experiment.target_name}。</Text>
          <Alert
            type="info"
            showIcon
            message="凭据不会经过浏览器"
            description="模型、Agent 和基础设施凭据必须已在服务主机的受控配置中就绪；本页面不提供凭据输入、展示或持久化。"
          />
          {launchError && <Alert type="error" showIcon message="启动请求未完成" description={launchError} />}
        </Space>
      </Modal>
    </div>
  );
}
