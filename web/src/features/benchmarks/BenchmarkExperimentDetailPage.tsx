import { useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  Alert,
  App,
  Button,
  Empty,
  Input,
  Modal,
  Progress,
  Space,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import {
  DownloadOutlined,
  EyeOutlined,
  FolderOpenOutlined,
  LinkOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  StopOutlined,
} from "@ant-design/icons";
import {
  useBenchmarkCancellationAudit,
  useBenchmarkExperimentReport,
  useCancelBenchmarkExperiment,
  useLaunchBenchmarkExperiment,
} from "../../api/hooks";
import type {
  BenchmarkArtifact,
  BenchmarkExperimentConfigSnapshot,
  BenchmarkExperimentLaunch,
  BenchmarkTrialResult,
} from "../../api/types";
import { DataTable } from "../../components/ui/DataTable";
import { ErrorState } from "../../components/ui/ErrorState";
import { PageHeader } from "../../components/ui/PageHeader";
import { PageSection } from "../../components/ui/PageSection";
import { StatusBadge } from "../../components/ui/StatusBadge";
import { TechnicalId } from "../../components/ui/TechnicalId";
import {
  displayMetricValue,
  executionKindLabel,
  formatPercent,
} from "./benchmark-utils";

const { Text } = Typography;

function ArtifactLinks({
  artifacts = [],
}: {
  artifacts?: BenchmarkArtifact[];
}) {
  if (!artifacts.length) return <Text type="secondary">—</Text>;
  return (
    <Space size={[2, 4]} wrap>
      {artifacts.map((artifact, index) => {
        const href = artifact.url ?? artifact.uri;
        return href ? (
          <Tooltip
            key={`${artifact.name}-${index}`}
            title={artifact.kind || "评测制品"}
          >
            <a
              href={href}
              target="_blank"
              rel="noreferrer"
              className="benchmark-artifact-link"
            >
              <DownloadOutlined /> {artifact.name}
            </a>
          </Tooltip>
        ) : (
          <Tag key={`${artifact.name}-${index}`}>{artifact.name}</Tag>
        );
      })}
    </Space>
  );
}

function ResultTag({
  passed,
  valid,
}: Pick<BenchmarkTrialResult, "passed" | "valid">) {
  if (valid === false) return <Tag color="warning">结果无效</Tag>;
  if (passed === true) return <Tag color="success">通过</Tag>;
  if (passed === false) return <Tag color="error">未通过</Tag>;
  return <Tag>待验证</Tag>;
}

function SnapshotTags({ values }: { values?: string[] }) {
  if (!values?.length) return <Text type="secondary">—</Text>;
  return (
    <Space size={[2, 4]} wrap>
      {values.map((value) => (
        <Tag key={value}>{value}</Tag>
      ))}
    </Space>
  );
}

function BenchmarkConfigSnapshot({
  snapshot,
}: {
  snapshot?: BenchmarkExperimentConfigSnapshot;
}) {
  if (!snapshot) return null;
  const artifact = snapshot.agent_artifact;
  const profiles = snapshot.profiles;
  const selection = snapshot.task_selection;
  const profileLabel = (profile?: { id?: string; framework?: string }) => {
    if (!profile?.id) return "未记录";
    return profile.framework
      ? `${profile.id} · ${profile.framework}`
      : profile.id;
  };

  return (
    <PageSection
      title="不可变 Catalog 快照"
      description="以下身份信息从创建实验时持久化的 benchmark_config snapshot 投影而来；原始 profile 配置、执行覆盖项和任何凭据均不会返回浏览器。"
    >
      <div className="benchmark-fact-list">
        <span>
          <strong>Catalog revision</strong>
          {snapshot.catalog_revision || "未记录"}
        </span>
        <span>
          <strong>Run Template</strong>
          {snapshot.run_template_id || "未记录"}
        </span>
        <span>
          <strong>独立试验</strong>每任务 {snapshot.trials_per_case ?? "未记录"}{" "}
          次
        </span>
        <span>
          <strong>Agent artifact</strong>
          {artifact?.id || "未记录"}
        </span>
        <span>
          <strong>镜像 digest</strong>
          {artifact?.image_digest ? (
            <TechnicalId value={artifact.image_digest} />
          ) : (
            "未记录"
          )}
        </span>
        <span>
          <strong>源码 commit</strong>
          {artifact?.source_commit ? (
            <TechnicalId value={artifact.source_commit} />
          ) : (
            "未记录"
          )}
        </span>
        <span>
          <strong>模型 Profile</strong>
          {profileLabel(profiles?.model)}
        </span>
        <span>
          <strong>Agent Profile</strong>
          {profileLabel(profiles?.agent)}
        </span>
        <span>
          <strong>执行 Profile</strong>
          {profileLabel(profiles?.execution)}
        </span>
        <span>
          <strong>任务选择模式</strong>
          {selection?.mode || "未记录"}
        </span>
        {selection?.count !== undefined ? (
          <span>
            <strong>抽样数量</strong>
            {selection.count}
          </span>
        ) : null}
        {selection?.seed !== undefined ? (
          <span>
            <strong>抽样种子</strong>
            {selection.seed}
          </span>
        ) : null}
        {selection?.source_experiment ? (
          <span>
            <strong>失败任务来源实验</strong>
            <TechnicalId value={selection.source_experiment} />
          </span>
        ) : null}
        <span>
          <strong>包含任务</strong>
          <SnapshotTags values={selection?.include} />
        </span>
        <span>
          <strong>排除任务</strong>
          <SnapshotTags values={selection?.exclude} />
        </span>
        <span>
          <strong>最终选中 Case</strong>
          <SnapshotTags values={snapshot.selected_case_ids} />
        </span>
      </div>
    </PageSection>
  );
}

function launchStatusLabel(launch?: BenchmarkExperimentLaunch) {
  switch (launch?.status?.toLowerCase()) {
    case "queued":
      return "已加入队列";
    case "not_started":
    case "ready":
      return "等待 Worker";
    case "unavailable":
      return "没有可用 Worker";
    case "failed":
      return "队列请求失败";
    default:
      return launch?.status || "启动信息未提供";
  }
}

export function BenchmarkExperimentDetailPage() {
  const { projectId = "", experimentId = "" } = useParams<{
    projectId: string;
    experimentId: string;
  }>();
  const navigate = useNavigate();
  const reportQuery = useBenchmarkExperimentReport(projectId, experimentId);
  const launchMutation = useLaunchBenchmarkExperiment(projectId, experimentId);
  const cancellationMutation = useCancelBenchmarkExperiment(
    projectId,
    experimentId,
  );
  const cancellationAuditQuery = useBenchmarkCancellationAudit(
    projectId,
    experimentId,
  );
  const { message } = App.useApp();
  const [launchConfirmOpen, setLaunchConfirmOpen] = useState(false);
  const [launchError, setLaunchError] = useState<string>();
  const [cancelOpen, setCancelOpen] = useState(false);
  const [cancelReason, setCancelReason] = useState("");
  const [cancelError, setCancelError] = useState<string>();
  const [detailTrial, setDetailTrial] = useState<BenchmarkTrialResult>();
  const report = reportQuery.data;

  const metricEntries = useMemo(
    () => Object.entries(report?.official_metrics ?? {}),
    [report?.official_metrics],
  );
  const failureEntries = useMemo(
    () => Object.entries(report?.failure_attribution ?? {}).sort(([, left], [, right]) => right - left),
    [report?.failure_attribution],
  );

  if (reportQuery.isLoading)
    return <div className="benchmark-loading">正在装载官方结果与试验记录…</div>;
  if (reportQuery.isError || !report) {
    return (
      <ErrorState
        title="无法加载基准实验"
        description="报告尚不可用，或当前账户无权查看这个实验。"
        onRetry={() => reportQuery.refetch()}
      />
    );
  }

  const trialsPerCase = report.experiment.trials_per_case ?? 1;
  const officialScore =
    report.quality.official_score ?? report.experiment.aggregate_score;
  const completedJobs = report.execution.succeeded + report.execution.failed;
  const totalJobs =
    report.execution.total_jobs ??
    report.execution.queued + report.execution.running + completedJobs;
  const completionPercent = totalJobs
    ? Math.round((completedJobs / totalJobs) * 100)
    : 0;
  const launch = report.launch;
  const launchInProgress = launchMutation.isPending;
  const canQueue = !launchInProgress;
  const canCancel = ["queued", "running", "pending", "retrying"].includes(
    report.experiment.status.toLowerCase(),
  );
  const unavailableReason =
    launch?.unavailable_reason ??
    launch?.message ??
    "当前没有注册可处理该执行类型的 benchmark Worker；任务仍保留在 durable eval_jobs 队列中。";

  const confirmLaunch = async () => {
    setLaunchError(undefined);
    try {
      const result = await launchMutation.mutateAsync();
      message.success(
        result.message || "任务已加入 durable 队列，正在等待外部 Worker 领取。",
      );
      setLaunchConfirmOpen(false);
      void reportQuery.refetch();
    } catch (error) {
      const errorMessage =
        error instanceof Error
          ? error.message
          : "无法提交 benchmark 队列请求。";
      setLaunchError(errorMessage);
      message.error(errorMessage);
    }
  };

  const confirmCancel = async () => {
    setCancelError(undefined);
    try {
      const result = await cancellationMutation.mutateAsync(cancelReason);
      message.success(`已取消 ${result.cancelled_jobs} 个 queue/running job。`);
      setCancelOpen(false);
      setCancelReason("");
      void reportQuery.refetch();
      void cancellationAuditQuery.refetch();
    } catch (error) {
      const errorMessage =
        error instanceof Error ? error.message : "无法取消 benchmark 实验。";
      setCancelError(errorMessage);
      message.error(errorMessage);
    }
  };

  return (
    <div className="benchmark-page">
      <PageHeader
        eyebrow={`${report.suite.harness_kind} / ${report.suite.version}`}
        title={`${report.suite.name} · ${report.experiment.target_name}`}
        description="任务进入 durable 队列后，由已注册的外部 Worker 执行；官方 verifier 的得分、试验数据、追踪与制品会集中保存在同一份报告中。"
        onBack={() => navigate(`/projects/${projectId}/benchmarks`)}
        extra={
          <Space wrap>
            <Button
              icon={<ReloadOutlined />}
              onClick={() => reportQuery.refetch()}
              loading={reportQuery.isFetching}
            >
              刷新状态
            </Button>
            {canCancel ? (
              <Button
                danger
                icon={<StopOutlined />}
                onClick={() => setCancelOpen(true)}
                disabled={cancellationMutation.isPending}
              >
                取消实验
              </Button>
            ) : null}
            <Button
              type="primary"
              icon={<PlayCircleOutlined />}
              disabled={!canQueue}
              loading={launchInProgress}
              onClick={() => {
                setLaunchError(undefined);
                setLaunchConfirmOpen(true);
              }}
            >
              {launchInProgress ? "正在加入队列" : "加入队列"}
            </Button>
            <StatusBadge status={report.experiment.status} />
          </Space>
        }
      />

      <PageSection
        title="队列与 Worker"
        description="AgentScope 只创建并观察 durable eval_jobs；外部常驻 Worker 领取任务。浏览器不会收集、传输或保存任何模型、Agent 或访问凭据。"
        extra={
          <StatusBadge
            status={launch?.status}
            label={launchStatusLabel(launch)}
          />
        }
      >
        <div className="benchmark-local-launch">
          <div>
            <Text strong>
              {launchInProgress
                ? "正在提交队列请求"
                : launch?.available
                  ? "Worker 已就绪"
                  : "等待 Worker 注册"}
            </Text>
            <div>
              <Text type="secondary">
                {launch?.available || launchInProgress
                  ? launch?.message || "状态会在 Worker 领取任务后自动刷新。"
                  : unavailableReason}
              </Text>
            </div>
          </div>
          <Button
            type="primary"
            icon={<PlayCircleOutlined />}
            disabled={!canQueue}
            loading={launchInProgress}
            onClick={() => {
              setLaunchError(undefined);
              setLaunchConfirmOpen(true);
            }}
          >
            {launchInProgress ? "正在加入队列" : "确认加入队列"}
          </Button>
        </div>
        {!launch?.available && !launchInProgress && (
          <Alert
            className="benchmark-local-launch__notice"
            type="info"
            showIcon
            message="当前没有可用 Worker"
            description={unavailableReason}
          />
        )}
      </PageSection>

      <section
        className="benchmark-result-board"
        aria-label="Benchmark outcome"
      >
        <div className="benchmark-result-board__score">
          <span className="benchmark-kicker">Official score</span>
          <strong>
            {officialScore === undefined ? "—" : officialScore.toFixed(3)}
          </strong>
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
          <span>
            执行进度 · {completedJobs}/{totalJobs || "—"} Job
          </span>
          <Progress
            percent={completionPercent}
            size="small"
            status={
              report.execution.failed
                ? "exception"
                : report.execution.running
                  ? "active"
                  : "normal"
            }
          />
        </div>
      </section>

      <div className="surface-grid surface-grid--2">
        <PageSection
          title="实验快照"
          description="结果始终关联到固定 suite、target 版本和执行环境。"
        >
          <div className="benchmark-fact-list">
            <span>
              <strong>目标版本</strong>
              {report.experiment.target_version || "未标记"}
            </span>
            <span>
              <strong>执行环境</strong>
              {executionKindLabel(report.suite.execution_kind)}
            </span>
            <span>
              <strong>独立试验</strong>每任务 {trialsPerCase} 次
            </span>
            <span>
              <strong>技术 ID</strong>
              <TechnicalId value={report.experiment.id} />
            </span>
          </div>
        </PageSection>
        <PageSection
          title="执行状态"
          description="基础设施失败与官方未通过会被分别保留，避免混淆结果。"
        >
          <div className="benchmark-execution-grid">
            <span>
              <strong>{report.execution.queued}</strong>排队
            </span>
            <span>
              <strong>{report.execution.running}</strong>运行
            </span>
            <span>
              <strong>{report.execution.succeeded}</strong>已完成
            </span>
            <span>
              <strong>{report.execution.failed}</strong>执行失败
            </span>
          </div>
        </PageSection>
      </div>

      <BenchmarkConfigSnapshot snapshot={report.benchmark_config_snapshot} />

      <div className="surface-grid surface-grid--2">
        <PageSection
          title="过程与效率"
          description="结果层之外，补充执行轨迹的时延与成本信号；样本不足时明确显示缺失，而不是用零伪造。"
        >
          <div className="benchmark-fact-list">
            <span><strong>平均时延</strong>{report.efficiency?.avg_latency_ms === undefined ? "—" : `${report.efficiency.avg_latency_ms.toFixed(0)}ms`}</span>
            <span><strong>P95 时延</strong>{report.efficiency?.p95_latency_ms === undefined ? "—" : `${report.efficiency.p95_latency_ms}ms`}</span>
            <span><strong>总成本</strong>{report.efficiency?.total_cost === undefined ? "—" : displayMetricValue(report.efficiency.total_cost)}</span>
            <span><strong>平均成本</strong>{report.efficiency?.avg_cost === undefined ? "—" : displayMetricValue(report.efficiency.avg_cost)}</span>
            <span><strong>有效 / 无效试验</strong>{report.quality.valid_trials ?? 0} / {report.quality.invalid_trials ?? 0}</span>
          </div>
        </PageSection>
        <PageSection
          title="失败归因"
          description="把官方 verifier 未通过与基础设施错误分开，帮助下一轮迭代定位在 Agent、环境还是判分链路。"
        >
          {failureEntries.length ? (
            <Space wrap>
              {failureEntries.map(([kind, count]) => <Tag key={kind} color={kind === "verifier_failed" ? "warning" : "error"}>{kind} · {count}</Tag>)}
            </Space>
          ) : <Text type="secondary">暂无失败或错误归因</Text>}
        </PageSection>
      </div>

      <PageSection
        title="官方指标"
        description="这些数值由 benchmark verifier 产出；平台不会改写其判定。"
        extra={<Tag color="blue">{metricEntries.length} 项</Tag>}
      >
        {metricEntries.length ? (
          <div className="benchmark-metric-grid">
            {metricEntries.map(([key, value]) => (
              <div className="benchmark-metric" key={key}>
                <span>{key}</span>
                <strong>{displayMetricValue(value)}</strong>
              </div>
            ))}
          </div>
        ) : (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description="官方 runner 尚未返回额外指标"
          />
        )}
      </PageSection>

      <PageSection
        title="任务试验"
        description="每一行是一个独立 trial；可直接进入 Trace 或打开官方产物，定位失败原因。"
        extra={
          <Space>
            <FolderOpenOutlined />
            <Text type="secondary">{report.trials.length} 条记录</Text>
          </Space>
        }
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
                render: (_: unknown, trial) => (
                  <Space orientation="vertical" size={1}>
                    <Text strong>{trial.task_id || "未命名任务"}</Text>
                    <Text type="secondary">
                      Trial {trial.trial_index + 1}
                      {trial.seed !== undefined ? ` · seed ${trial.seed}` : ""}
                    </Text>
                  </Space>
                ),
              },
              {
                title: "执行",
                dataIndex: "status",
                render: (status: string) => <StatusBadge status={status} />,
              },
              {
                title: "官方结果",
                key: "result",
                render: (_: unknown, trial) => (
                  <Space orientation="vertical" size={2}>
                    <ResultTag passed={trial.passed} valid={trial.valid} />
                    <Text>
                      {trial.official_score === undefined
                        ? "—"
                        : trial.official_score.toFixed(3)}
                    </Text>
                  </Space>
                ),
              },
              {
                title: "时延",
                dataIndex: "latency_ms",
                render: (value?: number) =>
                  value === undefined ? "—" : `${value}ms`,
              },
              {
                title: "错误分类",
                key: "error",
                render: (_: unknown, trial) =>
                  trial.error_type ? (
                    <Tooltip title={trial.error_message}>
                      <Tag color="error">{trial.error_type}</Tag>
                    </Tooltip>
                  ) : (
                    <Text type="secondary">—</Text>
                  ),
              },
              {
                title: "审计",
                key: "trace",
                render: (_: unknown, trial) =>
                  trial.run_id ? (
                    <Button
                      type="link"
                      size="small"
                      icon={<EyeOutlined />}
                      onClick={() => navigate(`/runs/${trial.run_id}`)}
                    >
                      Trace
                    </Button>
                  ) : (
                    <Text type="secondary">—</Text>
                  ),
              },
              {
                title: "详情",
                key: "details",
                render: (_: unknown, trial) => (
                  <Button
                    type="link"
                    size="small"
                    onClick={() => setDetailTrial(trial)}
                  >
                    查看过程
                  </Button>
                ),
              },
              {
                title: "制品",
                key: "artifacts",
                render: (_: unknown, trial) => (
                  <ArtifactLinks artifacts={trial.artifacts} />
                ),
              },
            ]}
          />
        ) : (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description="Runner 还没有提交试验记录"
          />
        )}
      </PageSection>

      <PageSection
        title="取消审计"
        description="取消只会终止 queued/running job；每个实际变更都保留项目管理员可见的理由和原状态。"
        extra={
          <Button
            size="small"
            onClick={() => cancellationAuditQuery.refetch()}
            loading={cancellationAuditQuery.isFetching}
          >
            加载审计记录
          </Button>
        }
      >
        {cancellationAuditQuery.data?.length ? (
          <Space orientation="vertical" size={6} style={{ width: "100%" }}>
            {cancellationAuditQuery.data.map((event) => (
              <Alert
                key={event.id}
                type="warning"
                showIcon
                message={`${event.eval_job_id}：${event.previous_status} → cancelled`}
                description={
                  <Space orientation="vertical" size={1}>
                    <span>{event.reason}</span>
                    <Text type="secondary">{event.created_at}</Text>
                  </Space>
                }
              />
            ))}
          </Space>
        ) : (
          <Text type="secondary">
            尚未加载取消审计；没有记录时代表该实验尚未发生可审计的 job 取消。
          </Text>
        )}
      </PageSection>

      <Button
        icon={<LinkOutlined />}
        onClick={() => navigate(`/experiments/${report.experiment.id}`)}
      >
        打开通用实验报告
      </Button>

      <Modal
        title={`Trial 详情 · ${detailTrial?.task_id ?? ""}`}
        open={Boolean(detailTrial)}
        onCancel={() => setDetailTrial(undefined)}
        footer={
          detailTrial?.run_id ? (
            <Button
              type="primary"
              icon={<EyeOutlined />}
              onClick={() => navigate(`/runs/${detailTrial.run_id}`)}
            >
              打开完整 Trace
            </Button>
          ) : null
        }
        width={880}
      >
        {detailTrial ? (
          <Space orientation="vertical" size={14} style={{ width: "100%" }}>
            <Alert
              type={
                detailTrial.passed
                  ? "success"
                  : detailTrial.error_type
                    ? "error"
                    : "info"
              }
              showIcon
              message={
                detailTrial.official_score === undefined
                  ? "任务仍在运行或等待官方 verifier"
                  : `官方 Harbor 评分：${detailTrial.official_score.toFixed(3)}`
              }
              description={
                detailTrial.error_message ||
                "最终判定以 Harbor official verifier 为准。"
              }
            />
            <div className="benchmark-trial-detail__section">
              <Text strong>题目</Text>
              <pre className="benchmark-trial-detail__text">
                {detailTrial.question || "任务题面尚不可用。"}
              </pre>
            </div>
            <div className="benchmark-trial-detail__section">
              <Text strong>Agent 最终答复</Text>
              <pre className="benchmark-trial-detail__text">
                {detailTrial.answer || "Agent 尚未返回最终答复。"}
              </pre>
            </div>
            <Text type="secondary">
              中间的模型思考、工具调用与环境操作保存在关联
              Trace；题面和答复中的凭据模式会自动脱敏。
            </Text>
          </Space>
        ) : null}
      </Modal>

      <Modal
        title="取消基准实验"
        open={cancelOpen}
        onCancel={() => !cancellationMutation.isPending && setCancelOpen(false)}
        onOk={confirmCancel}
        okButtonProps={{ danger: true, disabled: !cancelReason.trim() }}
        confirmLoading={cancellationMutation.isPending}
        okText="确认取消"
        cancelText="保留队列"
      >
        <Space orientation="vertical" size={12} style={{ width: "100%" }}>
          <Alert
            type="warning"
            showIcon
            message="此操作会取消当前 queued/running Job"
            description="晚到的 Worker 结果不会重新激活已取消的 Job。请填写不包含凭据的简短审计理由。"
          />
          <Input.TextArea
            value={cancelReason}
            onChange={(event) => setCancelReason(event.target.value)}
            maxLength={512}
            autoSize={{ minRows: 3, maxRows: 6 }}
            placeholder="例如：替换为新的不可变镜像快照"
          />
          {cancelError ? (
            <Alert
              type="error"
              showIcon
              message="取消未完成"
              description={cancelError}
            />
          ) : null}
        </Space>
      </Modal>

      <Modal
        title="确认将基准实验加入队列"
        open={launchConfirmOpen}
        onCancel={() =>
          !launchMutation.isPending && setLaunchConfirmOpen(false)
        }
        onOk={confirmLaunch}
        confirmLoading={launchMutation.isPending}
        okText="加入队列"
        cancelText="暂不加入"
      >
        <Space orientation="vertical" size={12} style={{ width: "100%" }}>
          <Text>
            将确认 <Text strong>{report.suite.name}</Text> 的 durable eval_jobs
            保持在队列中，等待外部 Worker 领取并运行{" "}
            {report.experiment.target_name}。
          </Text>
          <Alert
            type="info"
            showIcon
            message="凭据不会经过浏览器"
            description="模型、Agent 和基础设施凭据由外部 Worker 的受控运行时配置提供；本页面不提供凭据输入、展示或持久化。"
          />
          {launchError && (
            <Alert
              type="error"
              showIcon
              message="队列请求未完成"
              description={launchError}
            />
          )}
        </Space>
      </Modal>
    </div>
  );
}
