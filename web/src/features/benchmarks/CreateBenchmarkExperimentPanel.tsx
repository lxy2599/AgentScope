import { useEffect, useMemo, useState } from "react";
import {
  Alert,
  App,
  Button,
  Card,
  Descriptions,
  Form,
  Select,
  Space,
  Tag,
  Typography,
} from "antd";
import { CheckCircleOutlined, SafetyCertificateOutlined } from "@ant-design/icons";
import { useBenchmarkPreflight, useCreateBenchmarkExperiment } from "../../api/hooks";
import type { BenchmarkCatalog, BenchmarkPreflightInput, BenchmarkSuite, BenchmarkTarget } from "../../api/types";
import { executionKindLabel } from "./benchmark-utils";

const { Text, Title } = Typography;

interface BenchmarkFormValues {
  run_template_id?: string;
  suite_id?: string;
  target_name?: string;
  trials_per_case?: number;
}

interface Props {
  projectId: string;
  suites: BenchmarkSuite[];
  targets: BenchmarkTarget[];
  catalog?: BenchmarkCatalog;
  initialSuiteId?: string;
  initialRunTemplateId?: string;
  onCreated: (experimentId: string) => void;
}

function payloadSignature(payload: BenchmarkPreflightInput) {
  return JSON.stringify(payload);
}

export function CreateBenchmarkExperimentPanel({
  projectId,
  suites,
  targets,
  catalog,
  initialSuiteId,
  initialRunTemplateId,
  onCreated,
}: Props) {
  const [form] = Form.useForm<BenchmarkFormValues>();
  const preflight = useBenchmarkPreflight(projectId);
  const create = useCreateBenchmarkExperiment(projectId);
  const { message } = App.useApp();
  const [verifiedSignature, setVerifiedSignature] = useState<string>();
  const templateId = Form.useWatch("run_template_id", form);
  const suiteId = Form.useWatch("suite_id", form);
  const targetName = Form.useWatch("target_name", form);
  const trialsPerCase = Form.useWatch("trials_per_case", form) ?? 1;
  const selectedTemplate = useMemo(
    () => (catalog?.run_templates ?? []).find((template) => template.id === templateId),
    [catalog, templateId],
  );
  const selectedSuite = useMemo(() => suites.find((suite) => suite.id === suiteId), [suiteId, suites]);
  const selectedTarget = useMemo(() => targets.find((target) => target.name === targetName), [targetName, targets]);
  const artifact = useMemo(
    () => (catalog?.artifacts ?? []).find((item) => item.id === selectedTemplate?.agent_artifact_id),
    [catalog, selectedTemplate],
  );
  const currentPayload = useMemo<BenchmarkPreflightInput | undefined>(() => {
    if (selectedTemplate) return { run_template_id: selectedTemplate.id };
    if (!selectedSuite || !selectedTarget || !trialsPerCase) return undefined;
    return { suite_id: selectedSuite.id, target_name: selectedTarget.name, target_version: selectedTarget.version, trials_per_case: trialsPerCase };
  }, [selectedTemplate, selectedSuite, selectedTarget, trialsPerCase]);
  const isCurrentPreflight = Boolean(currentPayload && preflight.data && verifiedSignature === payloadSignature(currentPayload));

  useEffect(() => {
    if (initialRunTemplateId) form.setFieldsValue({ run_template_id: initialRunTemplateId, suite_id: undefined, target_name: undefined });
    else if (initialSuiteId) form.setFieldsValue({ suite_id: initialSuiteId, run_template_id: undefined });
  }, [form, initialRunTemplateId, initialSuiteId]);

  const runPreflight = async () => {
    if (!currentPayload) { await form.validateFields(); return; }
    try { await preflight.mutateAsync(currentPayload); setVerifiedSignature(payloadSignature(currentPayload)); }
    catch (error) { setVerifiedSignature(undefined); message.error(error instanceof Error ? error.message : "预检未完成，请检查 Worker 与模板。 "); }
  };

  const createExperiment = async () => {
    if (!currentPayload || !isCurrentPreflight || !preflight.data?.compatible) {
      message.warning("请先完成当前配置的兼容性预检。"); return;
    }
    const experiment = await create.mutateAsync(currentPayload);
    message.success("基准实验已创建；任务已 durable queue，等待外部 Worker 领取。");
    onCreated(experiment.id);
  };
  const blockers = isCurrentPreflight ? preflight.data?.blockers ?? [] : [];
  const warnings = isCurrentPreflight ? preflight.data?.warnings ?? [] : [];

  return (
    <div className="benchmark-create">
      <div className="benchmark-create__intro">
        <span className="benchmark-kicker">Launch a reproducible run</span>
        <Title level={3}>配置基准实验</Title>
        <Text type="secondary">优先选择已同步的 Run Template；它会冻结 artifact digest、profile、任务筛选和有效执行配置，而不是由浏览器输入凭据或命令。</Text>
      </div>
      <Form form={form} layout="vertical" initialValues={{ suite_id: initialSuiteId, run_template_id: initialRunTemplateId, trials_per_case: 1 }} onValuesChange={() => setVerifiedSignature(undefined)}>
        {(catalog?.run_templates ?? []).length ? (
          <Form.Item name="run_template_id" label="1. 选择已同步的 Run Template" extra="模板是推荐入口；创建后保存 catalog revision 和完整有效配置快照。">
            <Select allowClear showSearch optionFilterProp="label" placeholder="选择不可变 Benchmark 模板" onChange={(value) => { if (value) form.setFieldsValue({ suite_id: undefined, target_name: undefined }); }} options={(catalog?.run_templates ?? []).map((template) => ({ value: template.id, label: template.display_name || template.id }))} />
          </Form.Item>
        ) : null}
        {selectedTemplate ? (
          <Card size="small" className="benchmark-create__suite-summary">
            <Space orientation="vertical" size={5} style={{ width: "100%" }}>
              <Space wrap><Text strong>{selectedTemplate.display_name || selectedTemplate.id}</Text><Tag color="blue">{selectedTemplate.suite_id}</Tag><Tag>{artifact?.framework || "artifact"}</Tag></Space>
              <Descriptions size="small" column={1}>
                <Descriptions.Item label="Agent artifact">{selectedTemplate.agent_artifact_id}</Descriptions.Item>
                <Descriptions.Item label="镜像 digest"><Text code>{artifact?.image_digest || "未解析"}</Text></Descriptions.Item>
                <Descriptions.Item label="源码 commit">{artifact?.source_commit || "未标记"}</Descriptions.Item>
                <Descriptions.Item label="模型 / Agent / 执行 profile">{selectedTemplate.model_profile_id} · {selectedTemplate.agent_profile_id} · {selectedTemplate.execution_profile_id}</Descriptions.Item>
                <Descriptions.Item label="任务筛选"><Text code>{JSON.stringify(selectedTemplate.task_selection)}</Text></Descriptions.Item>
                <Descriptions.Item label="独立试验">每任务 {selectedTemplate.trials_per_case} 次</Descriptions.Item>
              </Descriptions>
            </Space>
          </Card>
        ) : (
          <>
            <Form.Item name="suite_id" label="1. 选择基准套件" rules={[{ required: true, message: "请选择要运行的基准套件" }]}>
              <Select showSearch optionFilterProp="label" placeholder="选择已固定版本的 Benchmark" options={suites.map((suite) => ({ value: suite.id, label: `${suite.name} · ${suite.version} · ${executionKindLabel(suite.execution_kind)}` }))} />
            </Form.Item>
            {selectedSuite && <Card size="small" className="benchmark-create__suite-summary"><Space orientation="vertical" size={5} style={{ width: "100%" }}><Space wrap><Text strong>{selectedSuite.name}</Text><Tag color="blue">{selectedSuite.version}</Tag><Tag>{executionKindLabel(selectedSuite.execution_kind)}</Tag></Space><Text type="secondary">{selectedSuite.description || "此套件未提供描述。"}</Text></Space></Card>}
            <Form.Item name="target_name" label="2. 选择可运行的 Agent" rules={[{ required: true, message: "请选择一个已注册且可运行的 Agent" }]}>
              <Select showSearch optionFilterProp="label" placeholder="例如：openhands / openclaw" options={targets.map((target) => ({ value: target.name, label: `${target.name}${target.version ? ` · ${target.version}` : ""}`, disabled: target.status === "offline" || target.status === "inactive" }))} />
            </Form.Item>
            {selectedTarget && <Descriptions size="small" column={1} className="benchmark-create__target-summary"><Descriptions.Item label="目标版本">{selectedTarget.version || "未标记"}</Descriptions.Item><Descriptions.Item label="能力">{selectedTarget.capabilities?.length ? selectedTarget.capabilities.join(" · ") : "等待预检确认"}</Descriptions.Item></Descriptions>}
            <Form.Item name="trials_per_case" label="3. 每个任务的独立试验次数" rules={[{ required: true, message: "请选择试验次数" }]}><Select options={[1, 3, 5].map((value) => ({ value, label: `${value} 次${value === 1 ? "（快速验证）" : "（估计 pass@k）"}` }))} /></Form.Item>
          </>
        )}
        <Card size="small" className="benchmark-preflight"><Space orientation="vertical" size={12} style={{ width: "100%" }}>
          <Space align="start"><SafetyCertificateOutlined className="benchmark-preflight__icon" /><div><Text strong>兼容性预检</Text><div><Text type="secondary">检查 worker、官方 harness、能力和将创建的 Job 数。</Text></div></div></Space>
          <Button onClick={runPreflight} loading={preflight.isPending} disabled={!currentPayload}>运行预检</Button>
          {isCurrentPreflight && preflight.data && <Alert showIcon type={preflight.data.compatible ? "success" : "error"} icon={preflight.data.compatible ? <CheckCircleOutlined /> : undefined} message={preflight.data.compatible ? "可以创建实验" : "当前配置无法运行"} description={<Space orientation="vertical" size={3}><span>{preflight.data.estimated_jobs} 个独立 Job · {executionKindLabel(preflight.data.execution_kind)}</span>{preflight.data.catalog_revision ? <span>Catalog revision：<Text code>{preflight.data.catalog_revision}</Text></span> : null}{preflight.data.agent_artifact_id ? <span>Artifact：<Text code>{preflight.data.agent_artifact_id}</Text></span> : null}{preflight.data.matched_capabilities?.length ? <span>已匹配：{preflight.data.matched_capabilities.join(" · ")}</span> : null}{preflight.data.missing_capabilities?.length ? <span>缺失：{preflight.data.missing_capabilities.join(" · ")}</span> : null}{blockers.map((blocker) => <span key={blocker}>{blocker}</span>)}{warnings.map((warning) => <span key={warning}>注意：{warning}</span>)}</Space>} />}
        </Space></Card>
        <Button type="primary" size="large" block style={{ marginTop: 18 }} loading={create.isPending} disabled={!isCurrentPreflight || !preflight.data?.compatible} onClick={createExperiment}>创建并加入队列</Button>
      </Form>
    </div>
  );
}
