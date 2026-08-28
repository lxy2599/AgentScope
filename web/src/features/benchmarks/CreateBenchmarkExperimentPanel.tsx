import { useEffect, useMemo, useState } from "react";
import {
  Alert,
  App,
  Button,
  Card,
  Checkbox,
  Descriptions,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Tag,
  Typography,
} from "antd";
import { CheckCircleOutlined, SafetyCertificateOutlined } from "@ant-design/icons";
import { useBenchmarkPreflight, useCreateBenchmarkExperiment, useDatasetCases } from "../../api/hooks";
import type { BenchmarkCatalog, BenchmarkPreflightInput, BenchmarkRunTemplate, BenchmarkSuite, BenchmarkTarget, DatasetCase } from "../../api/types";
import { executionKindLabel } from "./benchmark-utils";

const { Text, Title } = Typography;

interface BenchmarkFormValues {
  run_template_id?: string;
  suite_id?: string;
  target_name?: string;
  trials_per_case?: number;
  case_ids?: string[];
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

function metadataObject(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

function taskID(benchmarkCase: DatasetCase) {
  const configured = metadataObject(benchmarkCase.metadata).task_id;
  return typeof configured === "string" && configured.trim() ? configured : benchmarkCase.id;
}

function textValue(value: unknown, fallback: string) {
  if (typeof value === "string" && value.trim()) return value;
  try {
    const encoded = JSON.stringify(value, null, 2);
    return encoded && encoded !== "undefined" ? encoded : fallback;
  } catch {
    return fallback;
  }
}

function suiteForTemplate(template: BenchmarkRunTemplate, suites: BenchmarkSuite[]) {
  return suites.find((suite) => {
    if (suite.id === template.suite_id || suite.benchmark_key === template.suite_id) return true;
    return metadataObject(suite.metadata).catalog_suite_id === template.suite_id;
  });
}

function casesAllowedByTemplate(template: BenchmarkRunTemplate | undefined, benchmarkCase: DatasetCase) {
  if (!template) return true;
  const selection = template.task_selection ?? {};
  if (selection.mode !== "ids") return true;
  const include = Array.isArray(selection.include) ? selection.include : [];
  return include.includes(taskID(benchmarkCase));
}

function verifierDetails(benchmarkCase: DatasetCase) {
  const metadata = metadataObject(benchmarkCase.metadata);
  const verifier = metadataObject(metadata.verifier);
  return {
    entrypoint: typeof verifier.entrypoint === "string" ? verifier.entrypoint : "tests/test.sh",
    command: typeof verifier.command === "string" ? verifier.command : "由题目镜像执行官方 verifier",
    checks: Array.isArray(verifier.checks) ? verifier.checks.filter((item): item is string => typeof item === "string") : [],
    dependencies: Array.isArray(verifier.dependencies) ? verifier.dependencies.filter((item): item is string => typeof item === "string") : [],
    files: Array.isArray(verifier.files) ? verifier.files.filter((item): item is string => typeof item === "string") : [],
    runtimePolicy: typeof verifier.runtime_install_policy === "string" ? verifier.runtime_install_policy : "静态 verifier 依赖在题目镜像构建阶段安装；运行时不执行 pip/uv/npm 安装。",
  };
}

function environmentDetails(benchmarkCase: DatasetCase) {
  const environment = metadataObject(metadataObject(benchmarkCase.metadata).environment);
  const numberValue = (key: string) => typeof environment[key] === "number" ? environment[key] as number : undefined;
  const memory = numberValue("memory_mb");
  const storage = numberValue("storage_mb");
  return {
    cpus: numberValue("cpus"),
    memory: memory ? `${(memory / 1024).toFixed(memory % 1024 === 0 ? 0 : 1)} GB` : undefined,
    storage: storage ? `${(storage / 1024).toFixed(storage % 1024 === 0 ? 0 : 1)} GB` : undefined,
    gpus: numberValue("gpus"),
    buildTimeout: numberValue("build_timeout_sec"),
  };
}

function BenchmarkTaskPicker({
  cases,
  selectedCaseIDs,
  onChange,
}: {
  cases: DatasetCase[];
  selectedCaseIDs: string[];
  onChange: (caseIDs: string[]) => void;
}) {
  const [detailCase, setDetailCase] = useState<DatasetCase>();
  const [search, setSearch] = useState("");
  const filteredCases = useMemo(() => cases.filter((benchmarkCase) => {
    const metadata = metadataObject(benchmarkCase.metadata);
    const haystack = [taskID(benchmarkCase), metadata.category, metadata.subcategory, ...(Array.isArray(metadata.tags) ? metadata.tags : [])]
      .filter((value): value is string => typeof value === "string")
      .join(" ")
      .toLowerCase();
    return haystack.includes(search.trim().toLowerCase());
  }), [cases, search]);
  const options = filteredCases.map((benchmarkCase) => ({
    label: (
      <Space size={8}>
        <Text code>{taskID(benchmarkCase)}</Text>
        <Button type="link" size="small" onClick={(event) => { event.preventDefault(); event.stopPropagation(); setDetailCase(benchmarkCase); }}>
          查看题目与 verifier
        </Button>
      </Space>
    ),
    value: benchmarkCase.id,
  }));
  return (
    <Card size="small" className="benchmark-task-picker">
      <Space orientation="vertical" size={8} style={{ width: "100%" }}>
        <Space wrap>
          <Text strong>选择要运行的题目</Text>
          <Tag color={selectedCaseIDs.length ? "blue" : "red"}>{selectedCaseIDs.length}/{cases.length} 已选择</Tag>
        </Space>
        <Text type="secondary">每个勾选题目会创建独立 Job；取消勾选只会缩小本次实验，不会修改不可变模板。</Text>
        {cases.length ? <>
          <Input.Search allowClear placeholder="按任务 ID、分类或标签筛选" onChange={(event) => setSearch(event.target.value)} />
          {filteredCases.length ? <Checkbox.Group options={options} value={selectedCaseIDs} onChange={(values) => onChange(values.map(String))} /> : <Text type="secondary">没有匹配的题目。</Text>}
        </> : <Alert type="warning" showIcon message="当前套件还没有同步题目" description="请先导入数据集 case；没有 case 时无法创建 benchmark 实验。" />}
      </Space>
      <Modal title={`题目详情 · ${detailCase ? taskID(detailCase) : ""}`} open={Boolean(detailCase)} onCancel={() => setDetailCase(undefined)} footer={null} width={760}>
        {detailCase ? (() => {
          const details = verifierDetails(detailCase);
          const metadata = metadataObject(detailCase.metadata);
          const environment = environmentDetails(detailCase);
          return (
            <Space orientation="vertical" size={14} style={{ width: "100%" }}>
              <Descriptions size="small" column={1} bordered>
                <Descriptions.Item label="任务 ID"><Text code>{taskID(detailCase)}</Text></Descriptions.Item>
                <Descriptions.Item label="数据库 case ID"><Text code>{detailCase.id}</Text></Descriptions.Item>
                <Descriptions.Item label="分类 / 标签">{[metadata.category, metadata.subcategory, ...(Array.isArray(metadata.tags) ? metadata.tags : [])].filter((item): item is string => typeof item === "string" && item.trim().length > 0).map((item) => <Tag key={item}>{item}</Tag>)}</Descriptions.Item>
                <Descriptions.Item label="输入题目"><pre className="benchmark-task-detail__pre">{textValue(detailCase.input, "未提供")}</pre></Descriptions.Item>
              </Descriptions>
              <Card size="small" title="任务资源与时间预算">
                <Descriptions size="small" column={2}>
                  <Descriptions.Item label="CPU">{environment.cpus ? `${environment.cpus} vCPU` : "未声明"}</Descriptions.Item>
                  <Descriptions.Item label="内存">{environment.memory || "未声明"}</Descriptions.Item>
                  <Descriptions.Item label="磁盘">{environment.storage || "未声明"}</Descriptions.Item>
                  <Descriptions.Item label="GPU">{environment.gpus === undefined ? "未声明" : environment.gpus}</Descriptions.Item>
                  <Descriptions.Item label="环境构建超时">{environment.buildTimeout ? `${environment.buildTimeout}s` : "未声明"}</Descriptions.Item>
                  <Descriptions.Item label="Verifier 超时">{metadata.verifier_timeout_sec ? `${metadata.verifier_timeout_sec}s` : "未声明"}</Descriptions.Item>
                </Descriptions>
              </Card>
              <Card size="small" title="Verifier 检查方式">
                <Descriptions size="small" column={1}>
                  <Descriptions.Item label="入口文件"><Text code>{details.entrypoint}</Text></Descriptions.Item>
                  <Descriptions.Item label="执行命令"><Text code>{details.command}</Text></Descriptions.Item>
                  <Descriptions.Item label="Verifier 文件">{details.files.length ? details.files.map((file) => <Tag key={file}>{file}</Tag>) : "未同步文件清单"}</Descriptions.Item>
                  <Descriptions.Item label="检查项目">{details.checks.length ? <ul>{details.checks.map((check) => <li key={check}>{check}</li>)}</ul> : "官方 verifier 完成后读取 reward.txt/reward.json 判定结果"}</Descriptions.Item>
                  <Descriptions.Item label="预装依赖">{details.dependencies.length ? details.dependencies.map((dependency) => <Tag key={dependency}>{dependency}</Tag>) : "依赖信息未同步；由题目 verifier 镜像提供"}</Descriptions.Item>
                  <Descriptions.Item label="安装策略">{details.runtimePolicy}</Descriptions.Item>
                </Descriptions>
              </Card>
              {Object.keys(metadata).length ? <details><summary>其他非敏感元数据</summary><pre className="benchmark-task-detail__pre">{textValue(metadata, "{}")}</pre></details> : null}
            </Space>
          );
        })() : null}
      </Modal>
    </Card>
  );
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
  const selectedCaseIDs = Form.useWatch("case_ids", form) ?? [];
  const trialsPerCase = Form.useWatch("trials_per_case", form) ?? 1;
  const selectedTemplate = useMemo(() => (catalog?.run_templates ?? []).find((template) => template.id === templateId), [catalog, templateId]);
  const selectedSuite = useMemo(() => suites.find((suite) => suite.id === suiteId), [suiteId, suites]);
  const templateSuite = useMemo(() => selectedTemplate ? suiteForTemplate(selectedTemplate, suites) : undefined, [selectedTemplate, suites]);
  const activeSuite = selectedSuite ?? templateSuite;
  const selectedTarget = useMemo(() => targets.find((target) => target.name === targetName), [targetName, targets]);
  const casesQuery = useDatasetCases(activeSuite?.dataset_id ?? "");
  const availableCases = useMemo(() => (casesQuery.data ?? []).filter((benchmarkCase) => casesAllowedByTemplate(selectedTemplate, benchmarkCase)), [casesQuery.data, selectedTemplate]);
  const allowedCaseIDs = useMemo(() => availableCases.map((benchmarkCase) => benchmarkCase.id), [availableCases]);
  const artifact = useMemo(() => (catalog?.artifacts ?? []).find((item) => item.id === selectedTemplate?.agent_artifact_id), [catalog, selectedTemplate]);
  const currentPayload = useMemo<BenchmarkPreflightInput | undefined>(() => {
    if (selectedTemplate) return { run_template_id: selectedTemplate.id, case_ids: selectedCaseIDs };
    if (!selectedSuite || !selectedTarget || !trialsPerCase) return undefined;
    return { suite_id: selectedSuite.id, target_name: selectedTarget.name, target_version: selectedTarget.version, trials_per_case: trialsPerCase, case_ids: selectedCaseIDs.length ? selectedCaseIDs : undefined };
  }, [selectedTemplate, selectedSuite, selectedTarget, trialsPerCase, selectedCaseIDs]);
  const isCurrentPreflight = Boolean(currentPayload && preflight.data && verifiedSignature === payloadSignature(currentPayload));

  useEffect(() => {
    if (initialRunTemplateId) form.setFieldsValue({ run_template_id: initialRunTemplateId, suite_id: undefined, target_name: undefined, case_ids: undefined });
    else if (initialSuiteId) form.setFieldsValue({ suite_id: initialSuiteId, run_template_id: undefined, case_ids: undefined });
  }, [form, initialRunTemplateId, initialSuiteId]);

  useEffect(() => {
    if (activeSuite && availableCases.length && (!selectedCaseIDs.length || selectedCaseIDs.some((caseID) => !allowedCaseIDs.includes(caseID)))) {
      form.setFieldsValue({ case_ids: allowedCaseIDs });
    }
  }, [activeSuite, availableCases.length, allowedCaseIDs, form, selectedCaseIDs]);

  const runPreflight = async () => {
    if (!currentPayload) { await form.validateFields(); return; }
    try { await preflight.mutateAsync(currentPayload); setVerifiedSignature(payloadSignature(currentPayload)); }
    catch (error) { setVerifiedSignature(undefined); message.error(error instanceof Error ? error.message : "预检未完成，请检查 Worker 与模板。"); }
  };

  const createExperiment = async () => {
    if (!currentPayload || !isCurrentPreflight || !preflight.data?.compatible) {
      message.warning("请先完成当前配置的兼容性预检。"); return;
    }
    const experiment = await create.mutateAsync(currentPayload);
    message.success("基准实验已创建；任务已进入 durable queue，等待外部 Worker 领取。");
    onCreated(experiment.id);
  };
  const blockers = isCurrentPreflight ? preflight.data?.blockers ?? [] : [];
  const warnings = isCurrentPreflight ? preflight.data?.warnings ?? [] : [];

  return (
    <div className="benchmark-create">
      <div className="benchmark-create__intro">
        <span className="benchmark-kicker">Launch a reproducible run</span>
        <Title level={3}>配置基准实验</Title>
        <Text type="secondary">优先选择已同步的 Run Template；它会冻结 artifact digest、profile、任务筛选和有效执行配置。你可以在允许范围内选择本次实际运行的题目。</Text>
      </div>
      <Form form={form} layout="vertical" initialValues={{ suite_id: initialSuiteId, run_template_id: initialRunTemplateId, trials_per_case: 1 }} onValuesChange={() => setVerifiedSignature(undefined)}>
        {(catalog?.run_templates ?? []).length ? (
          <Form.Item name="run_template_id" label="1. 选择已同步的 Run Template" extra="模板是推荐入口；创建后保存 catalog revision 和完整有效配置快照。">
            <Select allowClear showSearch optionFilterProp="label" placeholder="选择不可变 Benchmark 模板" onChange={(value) => { if (value) form.setFieldsValue({ suite_id: undefined, target_name: undefined, case_ids: undefined }); }} options={(catalog?.run_templates ?? []).map((template) => ({ value: template.id, label: template.display_name || template.id }))} />
          </Form.Item>
        ) : null}
        {selectedTemplate ? (
          <>
            <Card size="small" className="benchmark-create__template-summary">
              <Space orientation="vertical" size={8} style={{ width: "100%" }}>
                <Space wrap><Text strong>{selectedTemplate.display_name || selectedTemplate.id}</Text><Tag color="purple">不可变模板</Tag><Tag>{templateSuite?.name || selectedTemplate.suite_id}</Tag></Space>
                <Descriptions size="small" column={1}>
                  <Descriptions.Item label="Agent artifact">{selectedTemplate.agent_artifact_id}</Descriptions.Item>
                  <Descriptions.Item label="镜像 digest"><Text code>{artifact?.image_digest || "未解析"}</Text></Descriptions.Item>
                  <Descriptions.Item label="模型 / Agent / 执行 profile">{selectedTemplate.model_profile_id} · {selectedTemplate.agent_profile_id} · {selectedTemplate.execution_profile_id}</Descriptions.Item>
                  <Descriptions.Item label="模板任务筛选"><Text code>{JSON.stringify(selectedTemplate.task_selection)}</Text></Descriptions.Item>
                </Descriptions>
              </Space>
            </Card>
            <BenchmarkTaskPicker cases={availableCases} selectedCaseIDs={selectedCaseIDs} onChange={(caseIDs) => form.setFieldsValue({ case_ids: caseIDs })} />
          </>
        ) : (
          <>
            <Form.Item name="suite_id" label="1. 选择基准套件" rules={[{ required: true, message: "请选择要运行的基准套件" }]}><Select showSearch optionFilterProp="label" placeholder="选择已固定版本的 Benchmark" options={suites.map((suite) => ({ value: suite.id, label: `${suite.name} · ${suite.version} · ${executionKindLabel(suite.execution_kind)}` }))} /></Form.Item>
            {selectedSuite ? <Card size="small" className="benchmark-create__suite-summary"><Space orientation="vertical" size={5} style={{ width: "100%" }}><Space wrap><Text strong>{selectedSuite.name}</Text><Tag color="blue">{selectedSuite.version}</Tag><Tag>{executionKindLabel(selectedSuite.execution_kind)}</Tag></Space><Text type="secondary">{selectedSuite.description || "此套件未提供描述。"}</Text></Space></Card> : null}
            <BenchmarkTaskPicker cases={availableCases} selectedCaseIDs={selectedCaseIDs} onChange={(caseIDs) => form.setFieldsValue({ case_ids: caseIDs })} />
            <Form.Item name="target_name" label="2. 选择可运行的 Agent" rules={[{ required: true, message: "请选择一个已注册且可运行的 Agent" }]}><Select showSearch optionFilterProp="label" placeholder="例如：openhands / openclaw" options={targets.map((target) => ({ value: target.name, label: `${target.name}${target.version ? ` · ${target.version}` : ""}`, disabled: target.status === "offline" || target.status === "inactive" }))} /></Form.Item>
            {selectedTarget ? <Descriptions size="small" column={1} className="benchmark-create__target-summary"><Descriptions.Item label="目标版本">{selectedTarget.version || "未标记"}</Descriptions.Item><Descriptions.Item label="能力">{selectedTarget.capabilities?.length ? selectedTarget.capabilities.join(" · ") : "等待预检确认"}</Descriptions.Item></Descriptions> : null}
          </>
        )}
        <Form.Item name="trials_per_case" label={selectedTemplate ? "2. 每个任务的独立试验次数" : "3. 每个任务的独立试验次数"} rules={[{ required: true, message: "请选择试验次数" }]}><Select options={[1, 3, 5].map((value) => ({ value, label: `${value} 次${value === 1 ? "（快速验证）" : "（估计 pass@k）"}` }))} /></Form.Item>
        <Card size="small" className="benchmark-preflight"><Space orientation="vertical" size={12} style={{ width: "100%" }}><Space align="start"><SafetyCertificateOutlined className="benchmark-preflight__icon" /><div><Text strong>兼容性预检</Text><div><Text type="secondary">检查 worker、官方 harness、能力和将创建的 Job 数。</Text></div></div></Space><Button onClick={runPreflight} loading={preflight.isPending} disabled={!currentPayload}>运行预检</Button>{isCurrentPreflight && preflight.data ? <Alert showIcon type={preflight.data.compatible ? "success" : "error"} icon={preflight.data.compatible ? <CheckCircleOutlined /> : undefined} message={preflight.data.compatible ? "可以创建实验" : "当前配置无法运行"} description={<Space orientation="vertical" size={3}><span>{preflight.data.estimated_jobs} 个独立 Job · {executionKindLabel(preflight.data.execution_kind)}</span>{preflight.data.catalog_revision ? <span>Catalog revision：<Text code>{preflight.data.catalog_revision}</Text></span> : null}{preflight.data.agent_artifact_id ? <span>Artifact：<Text code>{preflight.data.agent_artifact_id}</Text></span> : null}{preflight.data.matched_capabilities?.length ? <span>已匹配：{preflight.data.matched_capabilities.join(" · ")}</span> : null}{preflight.data.missing_capabilities?.length ? <span>缺失：{preflight.data.missing_capabilities.join(" · ")}</span> : null}{blockers.map((blocker) => <span key={blocker}>{blocker}</span>)}{warnings.map((warning) => <span key={warning}>注意：{warning}</span>)}</Space>} /> : null}</Space></Card>
        <Button type="primary" size="large" block style={{ marginTop: 18 }} loading={create.isPending} disabled={!isCurrentPreflight || !preflight.data?.compatible} onClick={createExperiment}>创建并加入队列</Button>
      </Form>
    </div>
  );
}
