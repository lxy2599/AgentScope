import { useCallback, useState } from "react";
import { useParams } from "react-router-dom";
import {
  Card,
  Table,
  Button,
  Modal,
  Form,
  Input,
  Select,
  InputNumber,
  Switch,
  Typography,
  Space,
  Spin,
  Empty,
  Tag,
  Popconfirm,
  App,
  Divider,
} from "antd";
import {
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  MinusCircleOutlined,
} from "@ant-design/icons";
import {
  useMetricDefinitions,
  useCreateMetricDefinition,
  useUpdateMetricDefinition,
  useDeleteMetricDefinition,
} from "../../api/hooks";
import type { MetricDefinition, MetricDefinitionConfig } from "../../api/types";

const { Title, Text } = Typography;

const METRIC_KIND_OPTIONS = [
  { value: "final_answer", label: "最终答案 (final_answer)" },
  { value: "trajectory", label: "轨迹 (trajectory)" },
  { value: "tool_correctness", label: "工具调用 (tool_correctness)" },
  { value: "argument_correctness", label: "论据正确性 (argument_correctness)" },
  { value: "rag_groundedness", label: "RAG 相关性 (rag_groundedness)" },
  { value: "safety", label: "安全 (safety)" },
  { value: "efficiency", label: "效率 (efficiency)" },
  { value: "helpfulness", label: "有帮助性 (helpfulness)" },
];

const EVAL_RULE_OPTIONS = [
  { value: "exact", label: "精确匹配 (exact)" },
  { value: "contains", label: "包含 (contains)" },
  { value: "not_contains", label: "不包含 (not_contains)" },
  { value: "regex", label: "正则 (regex)" },
  { value: "numeric_threshold", label: "数值阈值 (numeric_threshold)" },
  { value: "latency_threshold", label: "延迟阈值 (latency_threshold)" },
  { value: "tool_sequence", label: "工具序列 (tool_sequence)" },
  { value: "required_tool_called", label: "必需工具调用 (required_tool_called)" },
  { value: "llm_judge", label: "LLM Judge (llm_judge)" },
];

interface MetricFormValues {
  name: string;
  kind: string;
  rule_type: string;
  rule_value?: string;
  weight: number;
  pass_threshold: number;
  enabled: boolean;
  veto: boolean;
  judge_provider?: string;
  judge_model?: string;
  rubric_dimensions?: { name: string; weight: number; description: string }[];
  rubric_essential_items?: string;
  rubric_veto_items?: string;
}

export function MetricConfigPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const { data: metrics, isLoading } = useMetricDefinitions(projectId!);
  const createMetric = useCreateMetricDefinition(projectId!);
  const updateMetric = useUpdateMetricDefinition(projectId!);
  const deleteMetric = useDeleteMetricDefinition(projectId!);

  const [modalOpen, setModalOpen] = useState(false);
  const [editingMetric, setEditingMetric] = useState<MetricDefinition | null>(null);
  const [form] = Form.useForm<MetricFormValues>();
  const { message } = App.useApp();

  const handleOpenCreate = useCallback(() => {
    setEditingMetric(null);
    form.resetFields();
    form.setFieldsValue({
      weight: 1.0,
      pass_threshold: 0.7,
      enabled: true,
      veto: false,
      rubric_dimensions: [{ name: "", weight: 1.0, description: "" }],
    });
    setModalOpen(true);
  }, [form]);

  const parseConfig = (config: string | undefined): MetricDefinitionConfig => {
    try {
      if (config) return JSON.parse(config);
    } catch {
      // ignore
    }
    return {};
  };

  const handleOpenEdit = useCallback(
    (metric: MetricDefinition) => {
      setEditingMetric(metric);
      const cfg: Record<string, any> = parseConfig(metric.config);
      form.setFieldsValue({
        name: metric.name,
        kind: metric.kind,
        rule_type: cfg.type || "exact",
        rule_value: cfg.value || cfg.pattern || cfg.tool_name || cfg.field || cfg.max_ms?.toString() || "",
        weight: cfg.weight ?? 1.0,
        pass_threshold: cfg.pass_threshold ?? 0.7,
        enabled: cfg.enabled !== false,
        veto: cfg.veto ?? false,
        judge_provider: cfg.judge_provider || "",
        judge_model: cfg.judge_model || "",
        rubric_dimensions: cfg.rubric?.dimensions?.map((d: { name: string; weight: number; description?: string }) => ({
          name: d.name,
          weight: d.weight,
          description: d.description || "",
        })) || [{ name: "", weight: 1.0, description: "" }],
        rubric_essential_items: cfg.rubric?.essential_items?.join("\n") || "",
        rubric_veto_items: cfg.rubric?.veto_items?.join("\n") || "",
      });
      setModalOpen(true);
    },
    [form],
  );

  const handleDelete = useCallback(
    async (id: string) => {
      try {
        await deleteMetric.mutateAsync(id);
        message.success("评测指标已删除");
      } catch {
        message.error("删除失败");
      }
    },
    [deleteMetric, message],
  );

  const handleSubmit = useCallback(
    async (values: MetricFormValues) => {
      const configObj: Record<string, any> = {
        type: values.rule_type,
        weight: values.weight,
        pass_threshold: values.pass_threshold,
        enabled: values.enabled,
      };

      // Add veto flag
      if (values.veto) {
        configObj.veto = true;
      }

      // Add judge config for llm_judge type
      if (values.rule_type === "llm_judge") {
        if (values.judge_provider) configObj.judge_provider = values.judge_provider;
        if (values.judge_model) configObj.judge_model = values.judge_model;

        // Build rubric structure
        if (values.rubric_dimensions && values.rubric_dimensions.length > 0) {
          const dimensions = values.rubric_dimensions.filter((d) => d.name.trim());
          if (dimensions.length > 0) {
            configObj.rubric = {
              dimensions: dimensions.map((d) => ({
                name: d.name,
                weight: d.weight,
                description: d.description,
              })),
            };

            // Add optional rubric fields
            if (values.rubric_essential_items?.trim()) {
              configObj.rubric.essential_items = values.rubric_essential_items
                .split("\n")
                .map((s: string) => s.trim())
                .filter(Boolean);
            }
            if (values.rubric_veto_items?.trim()) {
              configObj.rubric.veto_items = values.rubric_veto_items
                .split("\n")
                .map((s: string) => s.trim())
                .filter(Boolean);
            }
          }
        }
      } else {
        // For rule-based evaluators, set rule value
        if (values.rule_value) {
          if (values.rule_type === "latency_threshold") {
            configObj.max_ms = Number(values.rule_value);
          } else if (values.rule_type === "numeric_threshold") {
            configObj.value = Number(values.rule_value);
          } else if (values.rule_type === "tool_sequence") {
            configObj.tools = values.rule_value.split(",").map((s: string) => s.trim());
          } else if (values.rule_type === "required_tool_called") {
            configObj.tool_name = values.rule_value;
          } else {
            configObj.value = values.rule_value;
          }
        }
      }

      const configStr = JSON.stringify(configObj);

      try {
        if (editingMetric) {
          await updateMetric.mutateAsync({
            id: editingMetric.id,
            name: values.name,
            kind: values.kind,
            config: configStr,
            weight: values.weight,
            pass_threshold: values.pass_threshold,
            enabled: values.enabled,
          });
          message.success("评测指标已更新");
        } else {
          await createMetric.mutateAsync({
            name: values.name,
            kind: values.kind,
            config: configStr,
            weight: values.weight,
            pass_threshold: values.pass_threshold,
            enabled: values.enabled,
          });
          message.success("评测指标已创建");
        }
        setModalOpen(false);
      } catch (err: unknown) {
        message.error(err instanceof Error ? err.message : "操作失败");
      }
    },
    [editingMetric, createMetric, updateMetric, message],
  );

  const columns = [
    {
      title: "名称",
      dataIndex: "name",
      key: "name",
    },
    {
      title: "类型",
      dataIndex: "kind",
      key: "kind",
      render: (kind: string) => {
        const opt = METRIC_KIND_OPTIONS.find((o) => o.value === kind);
        return <Tag>{opt?.label || kind}</Tag>;
      },
    },
    {
      title: "规则",
      dataIndex: "config",
      key: "config",
      ellipsis: true,
      render: (config: string | undefined) => {
        if (!config) return <Text type="secondary">—</Text>;
        try {
          const cfg = JSON.parse(config);
          const type = cfg.type || "—";
          const weight = cfg.weight ? `w=${cfg.weight}` : "";
          const veto = cfg.veto ? "⚠否决" : "";
          return (
            <Space size={4}>
              <Text code style={{ fontSize: 11 }}>{type}</Text>
              {weight && <Text type="secondary" style={{ fontSize: 11 }}>{weight}</Text>}
              {veto && <Tag color="error" style={{ fontSize: 10, lineHeight: "16px" }}>否决</Tag>}
            </Space>
          );
        } catch {
          return <Text type="secondary">—</Text>;
        }
      },
    },
    {
      title: "状态",
      key: "enabled",
      render: (_: unknown, record: MetricDefinition) => {
        let enabled = true;
        try {
          if (record.config) {
            const cfg = JSON.parse(record.config);
            if (cfg.enabled === false) enabled = false;
          }
        } catch { /* ignore */ }
        return enabled
          ? <Tag icon={<CheckCircleOutlined />} color="success">启用</Tag>
          : <Tag icon={<CloseCircleOutlined />} color="error">禁用</Tag>;
      },
    },
    {
      title: "创建时间",
      dataIndex: "created_at",
      key: "created_at",
      render: (date: string) => new Date(date).toLocaleString("zh-CN"),
    },
    {
      title: "操作",
      key: "action",
      render: (_: unknown, record: MetricDefinition) => (
        <Space>
          <Button
            type="link"
            size="small"
            icon={<EditOutlined />}
            onClick={() => handleOpenEdit(record)}
          >
            编辑
          </Button>
          <Popconfirm
            title="确认删除"
            description="确定要删除这个评测指标吗？"
            onConfirm={() => handleDelete(record.id)}
            okText="删除"
            cancelText="取消"
          >
            <Button type="link" size="small" danger icon={<DeleteOutlined />}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <Space orientation="vertical" size="large" style={{ width: "100%" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <div>
          <Title level={3} style={{ margin: 0 }}>
            评测配置中心
          </Title>
          <Text type="secondary" style={{ fontSize: 13 }}>
            管理评测指标定义：类型、权重、阈值、Rubric 维度、否决项
          </Text>
        </div>
        <Button type="primary" icon={<PlusOutlined />} onClick={handleOpenCreate}>
          新增评测指标
        </Button>
      </div>

      <Card>
        {isLoading ? (
          <div style={{ textAlign: "center", padding: 32 }}>
            <Spin />
          </div>
        ) : !metrics || metrics.length === 0 ? (
          <Empty description="暂无评测指标，点击上方按钮创建" image={Empty.PRESENTED_IMAGE_SIMPLE} />
        ) : (
          <Table
            dataSource={metrics}
            columns={columns}
            rowKey="id"
            pagination={false}
            size="small"
          />
        )}
      </Card>

      {/* Create/Edit Modal */}
      <Modal
        title={editingMetric ? "编辑评测指标" : "新增评测指标"}
        open={modalOpen}
        onOk={form.submit}
        onCancel={() => setModalOpen(false)}
        confirmLoading={createMetric.isPending || updateMetric.isPending}
        okText={editingMetric ? "更新" : "创建"}
        cancelText="取消"
        width={720}
      >
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSubmit}
          requiredMark={false}
        >
          <Form.Item
            name="name"
            label="指标名称"
            rules={[
              { required: true, message: "请输入指标名称" },
              { max: 100, message: "名称不能超过100个字符" },
            ]}
          >
            <Input placeholder="例如：final-answer-quality" />
          </Form.Item>

          <Form.Item
            name="kind"
            label="指标类型"
            rules={[{ required: true, message: "请选择指标类型" }]}
          >
            <Select options={METRIC_KIND_OPTIONS} placeholder="选择指标类型" />
          </Form.Item>

          <Form.Item
            name="rule_type"
            label="评测规则"
            rules={[{ required: true, message: "请选择评测规则" }]}
          >
            <Select options={EVAL_RULE_OPTIONS} placeholder="选择评测规则" />
          </Form.Item>

          {/* Rule value (shown for non-llm_judge) */}
          <Form.Item
            noStyle
            shouldUpdate={(prev, curr) => prev.rule_type !== curr.rule_type}
          >
            {({ getFieldValue }) => {
              const ruleType = getFieldValue("rule_type");
              if (!ruleType || ruleType === "exact" || ruleType === "llm_judge") return null;
              const label = ruleType === "latency_threshold" ? "最大延迟 (ms)" :
                ruleType === "numeric_threshold" ? "阈值数值" :
                ruleType === "tool_sequence" ? "工具序列 (逗号分隔)" :
                ruleType === "required_tool_called" ? "工具名称" :
                ruleType === "regex" ? "正则表达式" : "匹配值";
              return (
                <Form.Item
                  name="rule_value"
                  label={label}
                  rules={[{ required: true, message: `请输入${label}` }]}
                >
                  <Input placeholder={`输入${label}`} />
                </Form.Item>
              );
            }}
          </Form.Item>

          {/* LLM Judge config (shown for llm_judge rule_type) */}
          <Form.Item
            noStyle
            shouldUpdate={(prev, curr) => prev.rule_type !== curr.rule_type}
          >
            {({ getFieldValue }) => {
              const ruleType = getFieldValue("rule_type");
              if (ruleType !== "llm_judge") return null;

              return (
                <>
                  <Divider plain>LLM Judge 配置</Divider>
                  <Space size="large" style={{ width: "100%" }} align="start">
                    <Form.Item
                      name="judge_provider"
                      label="Judge Provider"
                      tooltip="LLM-as-Judge 的 API 提供商"
                    >
                      <Select
                        style={{ width: 180 }}
                        placeholder="选择 provider"
                        options={[
                          { value: "openai-compatible", label: "OpenAI Compatible" },
                          { value: "anthropic-compatible", label: "Anthropic Compatible" },
                        ]}
                      />
                    </Form.Item>
                    <Form.Item
                      name="judge_model"
                      label="Judge Model"
                      tooltip="用于评测的模型名称"
                    >
                      <Input placeholder="例如：gpt-4o" style={{ width: 200 }} />
                    </Form.Item>
                  </Space>

                  <Divider plain>Rubric 维度</Divider>
                  <Form.List name="rubric_dimensions">
                    {(fields, { add, remove }) => (
                      <>
                        {fields.map(({ key, name, ...restField }) => (
                          <Space key={key} style={{ display: "flex", marginBottom: 8 }} align="baseline">
                            <Form.Item
                              {...restField}
                              name={[name, "name"]}
                              rules={[{ required: true, message: "维度名必填" }]}
                            >
                              <Input placeholder="维度名称" style={{ width: 160 }} />
                            </Form.Item>
                            <Form.Item
                              {...restField}
                              name={[name, "weight"]}
                              rules={[{ required: true, message: "权重必填" }]}
                            >
                              <InputNumber
                                min={0}
                                max={1}
                                step={0.1}
                                placeholder="权重"
                                style={{ width: 100 }}
                              />
                            </Form.Item>
                            <Form.Item {...restField} name={[name, "description"]}>
                              <Input placeholder="描述" style={{ width: 200 }} />
                            </Form.Item>
                            <MinusCircleOutlined onClick={() => remove(name)} />
                          </Space>
                        ))}
                        <Button type="dashed" onClick={() => add({ name: "", weight: 1.0, description: "" })} block icon={<PlusOutlined />}>
                          添加维度
                        </Button>
                      </>
                    )}
                  </Form.List>

                  <Space size="large" style={{ width: "100%" }} align="start">
                    <Form.Item
                      name="rubric_essential_items"
                      label="必需项 (每行一个)"
                      tooltip="Agent 行为中必须满足的条件"
                    >
                      <Input.TextArea rows={3} style={{ width: 280 }} placeholder="必须正确识别意图&#10;必须使用正确工具" />
                    </Form.Item>
                    <Form.Item
                      name="rubric_veto_items"
                      label="否决项 (每行一个)"
                      tooltip="一旦触发，整体测评直接不通过"
                    >
                      <Input.TextArea rows={3} style={{ width: 280 }} placeholder="不得包含有害内容&#10;不得泄露敏感信息" />
                    </Form.Item>
                  </Space>
                </>
              );
            }}
          </Form.Item>

          <Divider plain>权重与阈值</Divider>
          <Space size="large">
            <Form.Item
              name="weight"
              label="权重"
              rules={[
                { required: true, message: "请输入权重" },
                { type: "number", min: 0, max: 1, message: "权重必须在 0-1 之间" },
              ]}
            >
              <InputNumber min={0} max={1} step={0.1} style={{ width: 120 }} />
            </Form.Item>

            <Form.Item
              name="pass_threshold"
              label="通过阈值"
              rules={[
                { required: true, message: "请输入阈值" },
                { type: "number", min: 0, max: 1, message: "阈值必须在 0-1 之间" },
              ]}
            >
              <InputNumber min={0} max={1} step={0.05} style={{ width: 120 }} />
            </Form.Item>

            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>

            <Form.Item
              name="veto"
              label="否决项"
              valuePropName="checked"
              tooltip="开启后，该指标不通过则整体评测不通过"
            >
              <Switch />
            </Form.Item>
          </Space>
        </Form>
      </Modal>
    </Space>
  );
}
