import { useMemo, useState } from "react";
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
import {
  useBenchmarkPreflight,
  useCreateBenchmarkExperiment,
} from "../../api/hooks";
import type {
  BenchmarkPreflightInput,
  BenchmarkSuite,
  BenchmarkTarget,
} from "../../api/types";
import { executionKindLabel } from "./benchmark-utils";

const { Text, Title } = Typography;

interface BenchmarkFormValues {
  suite_id: string;
  target_name: string;
  trials_per_case: number;
}

interface Props {
  projectId: string;
  suites: BenchmarkSuite[];
  targets: BenchmarkTarget[];
  initialSuiteId?: string;
  onCreated: (experimentId: string) => void;
}

function payloadSignature(payload: BenchmarkPreflightInput) {
  return JSON.stringify(payload);
}

export function CreateBenchmarkExperimentPanel({
  projectId,
  suites,
  targets,
  initialSuiteId,
  onCreated,
}: Props) {
  const [form] = Form.useForm<BenchmarkFormValues>();
  const preflight = useBenchmarkPreflight(projectId);
  const create = useCreateBenchmarkExperiment(projectId);
  const { message } = App.useApp();
  const [verifiedSignature, setVerifiedSignature] = useState<string>();
  const suiteId = Form.useWatch("suite_id", form);
  const targetName = Form.useWatch("target_name", form);
  const trialsPerCase = Form.useWatch("trials_per_case", form) ?? 1;
  const selectedSuite = useMemo(() => suites.find((suite) => suite.id === suiteId), [suiteId, suites]);
  const selectedTarget = useMemo(() => targets.find((target) => target.name === targetName), [targetName, targets]);
  const currentPayload = useMemo<BenchmarkPreflightInput | undefined>(() => {
    if (!selectedSuite || !selectedTarget || !trialsPerCase) return undefined;
    return {
      suite_id: selectedSuite.id,
      target_name: selectedTarget.name,
      target_version: selectedTarget.version,
      trials_per_case: trialsPerCase,
    };
  }, [selectedSuite, selectedTarget, trialsPerCase]);
  const isCurrentPreflight = Boolean(
    currentPayload
      && preflight.data
      && verifiedSignature === payloadSignature(currentPayload),
  );

  const runPreflight = async () => {
    if (!currentPayload) {
      await form.validateFields();
      return;
    }
    try {
      await preflight.mutateAsync(currentPayload);
      setVerifiedSignature(payloadSignature(currentPayload));
    } catch (error) {
      setVerifiedSignature(undefined);
      message.error(error instanceof Error ? error.message : "预检未完成，请检查 Runner 与目标 Agent。");
    }
  };

  const createExperiment = async () => {
    const values = await form.validateFields();
    if (!currentPayload || !isCurrentPreflight || !preflight.data?.compatible) {
      message.warning("请先完成当前配置的兼容性预检。");
      return;
    }
    const experiment = await create.mutateAsync({
      ...currentPayload,
      suite_id: values.suite_id,
      target_name: values.target_name,
      trials_per_case: values.trials_per_case,
    });
    message.success("基准实验已创建。请在详情页确认并启动本机执行。");
    onCreated(experiment.id);
  };

  const blockers = isCurrentPreflight ? preflight.data?.blockers ?? [] : [];
  const warnings = isCurrentPreflight ? preflight.data?.warnings ?? [] : [];

  return (
    <div className="benchmark-create">
      <div className="benchmark-create__intro">
        <span className="benchmark-kicker">Launch a reproducible run</span>
        <Title level={3}>配置基准实验</Title>
        <Text type="secondary">先确认官方环境与 Agent 能力匹配，再创建独立试验；创建后由你在详情页确认本机启动。</Text>
      </div>

      <Form
        form={form}
        layout="vertical"
        initialValues={{ suite_id: initialSuiteId, trials_per_case: 1 }}
        onValuesChange={() => setVerifiedSignature(undefined)}
      >
        <Form.Item
          name="suite_id"
          label="1. 选择基准套件"
          rules={[{ required: true, message: "请选择要运行的基准套件" }]}
        >
          <Select
            showSearch
            optionFilterProp="label"
            placeholder="选择已固定版本的 Benchmark"
            options={suites.map((suite) => ({
              value: suite.id,
              label: `${suite.name} · ${suite.version} · ${executionKindLabel(suite.execution_kind)}`,
            }))}
          />
        </Form.Item>

        {selectedSuite && (
          <Card size="small" className="benchmark-create__suite-summary">
            <Space orientation="vertical" size={5} style={{ width: "100%" }}>
              <Space wrap>
                <Text strong>{selectedSuite.name}</Text>
                <Tag color="blue">{selectedSuite.version}</Tag>
                <Tag>{executionKindLabel(selectedSuite.execution_kind)}</Tag>
              </Space>
              <Text type="secondary">{selectedSuite.description || "此套件未提供描述。"}</Text>
              <Text type="secondary">
                要求：{selectedSuite.required_capabilities?.length ? selectedSuite.required_capabilities.join(" · ") : "无额外声明能力"}
              </Text>
            </Space>
          </Card>
        )}

        <Form.Item
          name="target_name"
          label="2. 选择可运行的 Agent"
          rules={[{ required: true, message: "请选择一个已注册且可运行的 Agent" }]}
          extra="只显示控制面已发现的可运行目标；版本随实验快照保存。"
        >
          <Select
            showSearch
            optionFilterProp="label"
            placeholder="例如：openhands / openclaw"
            options={targets.map((target) => ({
              value: target.name,
              label: `${target.name}${target.version ? ` · ${target.version}` : ""}${target.framework ? ` · ${target.framework}` : ""}`,
              disabled: target.status === "offline" || target.status === "inactive",
            }))}
          />
        </Form.Item>

        {selectedTarget && (
          <Descriptions size="small" column={1} className="benchmark-create__target-summary">
            <Descriptions.Item label="目标版本">{selectedTarget.version || "未标记"}</Descriptions.Item>
            <Descriptions.Item label="能力">{selectedTarget.capabilities?.length ? selectedTarget.capabilities.join(" · ") : "等待预检确认"}</Descriptions.Item>
          </Descriptions>
        )}

        <Form.Item
          name="trials_per_case"
          label="3. 每个任务的独立试验次数"
          rules={[{ required: true, message: "请选择试验次数" }]}
          extra="试验次数用于 pass@k；它不同于基础设施失败后的自动重试。"
        >
          <Select options={[1, 3, 5].map((value) => ({ value, label: `${value} 次${value === 1 ? "（快速验证）" : "（估计 pass@k）"}` }))} />
        </Form.Item>

        <Card size="small" className="benchmark-preflight">
          <Space orientation="vertical" size={12} style={{ width: "100%" }}>
            <Space align="start">
              <SafetyCertificateOutlined className="benchmark-preflight__icon" />
              <div>
                <Text strong>兼容性预检</Text>
                <div><Text type="secondary">检查官方 harness、Runner、必需能力和本次将创建的 Job 数。</Text></div>
              </div>
            </Space>
            <Button onClick={runPreflight} loading={preflight.isPending} disabled={!currentPayload}>
              运行预检
            </Button>
            {isCurrentPreflight && preflight.data && (
              <Alert
                showIcon
                type={preflight.data.compatible ? "success" : "error"}
                icon={preflight.data.compatible ? <CheckCircleOutlined /> : undefined}
                message={preflight.data.compatible ? "可以创建实验" : "当前配置无法运行"}
                description={
                  <Space orientation="vertical" size={3}>
                    <span>{preflight.data.estimated_jobs} 个独立 Job · {executionKindLabel(preflight.data.execution_kind)}</span>
                    {preflight.data.matched_capabilities?.length ? <span>已匹配：{preflight.data.matched_capabilities.join(" · ")}</span> : null}
                    {preflight.data.missing_capabilities?.length ? <span>缺失：{preflight.data.missing_capabilities.join(" · ")}</span> : null}
                    {blockers.map((blocker) => <span key={blocker}>{blocker}</span>)}
                    {warnings.map((warning) => <span key={warning}>注意：{warning}</span>)}
                  </Space>
                }
              />
            )}
          </Space>
        </Card>

        <Button
          type="primary"
          size="large"
          block
          style={{ marginTop: 18 }}
          loading={create.isPending}
          disabled={!isCurrentPreflight || !preflight.data?.compatible}
          onClick={createExperiment}
        >
          创建基准实验
        </Button>
      </Form>
    </div>
  );
}
