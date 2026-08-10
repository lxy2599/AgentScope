import { App, Alert, Button, Card, Form, Input, Select, Space, Typography } from "antd";
import { CodeOutlined, DatabaseOutlined } from "@ant-design/icons";
import { useState } from "react";
import { useCreateBenchmarkSuite, useDatasets } from "../../api/hooks";
import type { BenchmarkSuite, CreateBenchmarkSuiteInput } from "../../api/types";

const { Paragraph, Text, Title } = Typography;

const IDENTIFIER_PATTERN = /^[a-z][a-z0-9_-]{0,63}$/;
const SENSITIVE_CONFIG_KEY_PATTERN = /(?:api_?key|secret|token|password|credential|access_?key|private_?key|authorization)/i;

const HARBOR_TEMPLATE_CONFIG = {
  dataset: "terminal-bench@2.0",
  jobs_dir: "./.agentscope/harbor-jobs",
  concurrency: 1,
  timeout_s: 3600,
};

const HARBOR_TEMPLATE_VALUES = {
  benchmark_key: "terminal_bench",
  name: "Terminal-Bench 2.0",
  version: "2.0",
  harness_kind: "harbor",
  execution_kind: "harbor",
  required_capabilities: ["shell"],
  default_execution_config: JSON.stringify(HARBOR_TEMPLATE_CONFIG, null, 2),
};

interface RegisterBenchmarkSuiteValues {
  dataset_id: string;
  benchmark_key: string;
  name: string;
  description?: string;
  version: string;
  harness_kind: string;
  execution_kind: string;
  source_uri?: string;
  source_commit?: string;
  default_execution_config: string;
  required_capabilities: string[];
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function findSensitiveConfigKey(value: unknown, path = ""): string | undefined {
  if (Array.isArray(value)) {
    return value.map((entry, index) => findSensitiveConfigKey(entry, `${path}[${index}]`)).find(Boolean);
  }
  if (!isRecord(value)) return undefined;

  for (const [key, nestedValue] of Object.entries(value)) {
    const keyPath = path ? `${path}.${key}` : key;
    if (SENSITIVE_CONFIG_KEY_PATTERN.test(key)) return keyPath;
    const nestedMatch = findSensitiveConfigKey(nestedValue, keyPath);
    if (nestedMatch) return nestedMatch;
  }
  return undefined;
}

function parseSafeExecutionConfig(raw: string): Record<string, unknown> {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error("默认执行配置必须是合法的 JSON 对象");
  }
  if (!isRecord(parsed)) {
    throw new Error("默认执行配置必须是 JSON 对象");
  }
  const sensitiveKey = findSensitiveConfigKey(parsed);
  if (sensitiveKey) {
    throw new Error(`默认执行配置不能包含凭据字段：${sensitiveKey}`);
  }
  return parsed;
}

function trimOptional(value?: string) {
  const trimmed = value?.trim();
  return trimmed || undefined;
}

interface Props {
  projectId: string;
  onCreated: (suite: BenchmarkSuite) => void;
  onManageDatasets: () => void;
}

export function RegisterBenchmarkSuitePanel({ projectId, onCreated, onManageDatasets }: Props) {
  const [form] = Form.useForm<RegisterBenchmarkSuiteValues>();
  const [templateApplied, setTemplateApplied] = useState(true);
  const { message } = App.useApp();
  const datasetsQuery = useDatasets(projectId);
  const createSuite = useCreateBenchmarkSuite(projectId);
  const datasets = datasetsQuery.data ?? [];

  const applyHarborTemplate = () => {
    form.setFieldsValue(HARBOR_TEMPLATE_VALUES);
    setTemplateApplied(true);
  };

  const registerSuite = async () => {
    try {
      const values = await form.validateFields();
      const request: CreateBenchmarkSuiteInput = {
        dataset_id: values.dataset_id,
        benchmark_key: values.benchmark_key.trim(),
        name: values.name.trim(),
        description: trimOptional(values.description),
        version: values.version.trim(),
        harness_kind: values.harness_kind.trim(),
        execution_kind: values.execution_kind.trim(),
        source_uri: trimOptional(values.source_uri),
        source_commit: trimOptional(values.source_commit),
        default_execution_config: parseSafeExecutionConfig(values.default_execution_config),
        required_capabilities: values.required_capabilities.map((capability) => capability.trim()),
      };
      const suite = await createSuite.mutateAsync(request);
      message.success("基准套件已注册");
      onCreated(suite);
    } catch (error) {
      if (error instanceof Error && error.message.startsWith("默认执行配置")) {
        message.error(error.message);
        return;
      }
      // Ant Design wraps async Form.Item validator failures in a plain
      // validation object (rather than preserving the validator Error). Keep
      // the credential rejection visible as an explicit UI message, while the
      // field itself still renders its normal inline validation state.
      const formError = error as {
        errorFields?: Array<{ errors?: unknown }>;
      };
      const executionConfigError = formError.errorFields
        ?.flatMap((field) => Array.isArray(field.errors) ? field.errors : [])
        .find(
          (candidate): candidate is string =>
            typeof candidate === "string" &&
            candidate.startsWith("默认执行配置"),
        );
      if (executionConfigError) {
        message.error(executionConfigError);
        return;
      }
      if (error instanceof Error && error.message !== "") {
        message.error(error.message);
        return;
      }
      message.error("注册基准套件失败");
    }
  };

  return (
    <div className="benchmark-register">
      <div className="benchmark-register__intro">
        <span className="benchmark-kicker">Project-owned suite</span>
        <Title level={3}>注册基准套件</Title>
        <Paragraph type="secondary">
          固定套件版本、数据集与执行参数后，后续实验会保存快照以便复现。
        </Paragraph>
      </div>

      <Card size="small" className="benchmark-register__template">
        <Space orientation="vertical" size={9} style={{ width: "100%" }}>
          <Space align="start">
            <CodeOutlined className="benchmark-register__template-icon" />
            <div>
              <Text strong>Harbor 内置模板</Text>
              <div><Text type="secondary">包含官方 CLI、Terminal-Bench、OpenHands 和容器运行所需的非敏感参数。</Text></div>
            </div>
          </Space>
          <Button onClick={applyHarborTemplate}>填充 Harbor 模板</Button>
        </Space>
      </Card>

      {!datasetsQuery.isLoading && !datasets.length && (
        <Alert
          className="benchmark-register__dataset-alert"
          type="warning"
          showIcon
          title="注册前需要一个项目数据集"
          description="基准套件只引用已有数据集，不会在此页创建或复制测试用例。"
          action={<Button size="small" icon={<DatabaseOutlined />} onClick={onManageDatasets}>前往数据集</Button>}
        />
      )}

      <Alert
        className="benchmark-register__credential-notice"
        type="info"
        showIcon
        title="凭据只由服务端运行环境提供"
        description="此表单不会收集、展示或保存 API Key、访问令牌、密码或其他凭据；配置中的此类字段会被拒绝。"
      />

      <Form
        form={form}
        layout="vertical"
        requiredMark={false}
        initialValues={HARBOR_TEMPLATE_VALUES}
        onValuesChange={() => setTemplateApplied(false)}
      >
        <Form.Item
          name="dataset_id"
          label="关联数据集"
          rules={[{ required: true, message: "请选择一个已有数据集" }]}
          extra="一个套件绑定一个项目数据集；任务与评分结果会以该数据集为边界。"
        >
          <Select
            showSearch
            optionFilterProp="label"
            placeholder={datasetsQuery.isLoading ? "正在加载数据集…" : "选择已有数据集"}
            loading={datasetsQuery.isLoading}
            disabled={!datasets.length}
            options={datasets.map((dataset) => ({
              value: dataset.id,
              label: dataset.description ? `${dataset.name} · ${dataset.description}` : dataset.name,
            }))}
          />
        </Form.Item>

        <Form.Item
          name="benchmark_key"
          label="Benchmark Key"
          rules={[
            { required: true, message: "请输入 Benchmark Key" },
            { pattern: IDENTIFIER_PATTERN, message: "使用小写字母开头，仅包含小写字母、数字、_ 或 -" },
          ]}
        >
          <Input placeholder="例如：terminal_bench" />
        </Form.Item>

        <Form.Item name="name" label="套件名称" rules={[{ required: true, whitespace: true, message: "请输入套件名称" }]}>
          <Input placeholder="例如：Terminal-Bench 2.0" />
        </Form.Item>

        <Form.Item name="description" label="说明（可选）">
          <Input.TextArea rows={3} placeholder="说明这套任务验证的能力范围与使用场景。" />
        </Form.Item>

        <Form.Item name="version" label="固定版本" rules={[{ required: true, whitespace: true, message: "请输入套件版本" }]}>
          <Input placeholder="例如：2.0" />
        </Form.Item>

        <Form.Item
          name="harness_kind"
          label="Harness Kind"
          rules={[
            { required: true, message: "请输入 Harness Kind" },
            { pattern: IDENTIFIER_PATTERN, message: "使用小写字母开头，仅包含小写字母、数字、_ 或 -" },
          ]}
        >
          <Input placeholder="例如：harbor" />
        </Form.Item>

        <Form.Item
          name="execution_kind"
          label="Execution Kind"
          rules={[
            { required: true, message: "请输入 Execution Kind" },
            { pattern: IDENTIFIER_PATTERN, message: "使用小写字母开头，仅包含小写字母、数字、_ 或 -" },
          ]}
          extra="Runner 会按此标识匹配官方 Harness 执行器。"
        >
          <Input placeholder="例如：harbor" />
        </Form.Item>

        <Form.Item
          name="required_capabilities"
          label="必需能力"
          rules={[
            { required: true, type: "array", min: 1, message: "至少声明一项必需能力" },
            {
              validator: async (_rule, value: string[] | undefined) => {
                if ((value ?? []).every((capability) => IDENTIFIER_PATTERN.test(capability.trim()))) return;
                throw new Error("能力标识使用小写字母开头，仅包含小写字母、数字、_ 或 -");
              },
            },
          ]}
          extra="例如 shell、docker、browser。预检会用这些能力筛选可运行的 Agent。"
        >
          <Select mode="tags" tokenSeparators={[",", " "]} placeholder="输入能力后按 Enter" />
        </Form.Item>

        <Form.Item
          name="default_execution_config"
          label="默认执行配置（JSON）"
          rules={[
            { required: true, message: "请输入默认执行配置" },
            {
              validator: async (_rule, value: string | undefined) => {
                parseSafeExecutionConfig(value ?? "");
              },
            },
          ]}
          extra={templateApplied ? "当前使用 Harbor 内置模板；模型标识可调整，但访问凭据仅从服务端环境注入。" : "仅填写可复现的运行参数；凭据字段会被拒绝。"}
        >
          <Input.TextArea className="benchmark-register__config" rows={12} spellCheck={false} />
        </Form.Item>

        <Form.Item name="source_uri" label="来源地址（可选）">
          <Input placeholder="例如：https://github.com/laude-institute/harbor" />
        </Form.Item>

        <Form.Item name="source_commit" label="来源 Commit（可选）">
          <Input placeholder="例如：固定的 Git commit SHA" />
        </Form.Item>

        <Button
          type="primary"
          size="large"
          block
          loading={createSuite.isPending}
          disabled={!datasets.length}
          onClick={registerSuite}
        >
          注册基准套件
        </Button>
      </Form>
    </div>
  );
}
