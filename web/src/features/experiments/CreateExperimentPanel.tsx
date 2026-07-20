import { useEffect, useState, useCallback } from "react";
import { Form, Input, Button, Card, Select, Switch, Tag, Space, Typography, App } from "antd";
import { ExperimentOutlined } from "@ant-design/icons";
import { useCreateExperiment, useEvaluators, useMetricDefinitions } from "../../api/hooks";
import type { MetricDefinition } from "../../api/types";

const { Text } = Typography;

interface Props {
  projectId: string;
  datasetId?: string;
  onCreated?: () => void;
}

const METRIC_KIND_OPTIONS: Record<string, string> = {
  final_answer: "最终答案",
  trajectory: "轨迹",
  tool_correctness: "工具调用",
  rag_groundedness: "RAG 相关性",
  safety: "安全",
  efficiency: "效率",
  helpfulness: "有帮助性",
};

export function CreateExperimentPanel({ projectId, datasetId, onCreated }: Props) {
  const create = useCreateExperiment(projectId);
  const { data: evaluators } = useEvaluators(projectId);
  const { data: metricDefs } = useMetricDefinitions(projectId);
  const [form] = Form.useForm();
  const { message } = App.useApp();

  const [useMultiEval, setUseMultiEval] = useState(false);
  const [selectedMetrics, setSelectedMetrics] = useState<MetricDefinition[]>([]);

  // Sync datasetId prop to form field
  useEffect(() => {
    form.setFieldsValue({ dataset_id: datasetId ?? "" });
  }, [datasetId, form]);

  const handleToggleMultiEval = useCallback((checked: boolean) => {
    setUseMultiEval(checked);
    if (!checked) {
      setSelectedMetrics([]);
    }
  }, []);

  const handleMetricToggle = useCallback(
    (metric: MetricDefinition, checked: boolean) => {
      if (checked) {
        setSelectedMetrics((prev) => [...prev, metric]);
      } else {
        setSelectedMetrics((prev) => prev.filter((m) => m.id !== metric.id));
      }
    },
    [],
  );

  const buildEvaluatorConfig = useCallback(() => {
    if (!useMultiEval || selectedMetrics.length === 0) return undefined;

    const metrics = selectedMetrics.map((m) => {
      let ruleType = "exact";
      let config: Record<string, any> = {};
      try {
        if (m.config) {
          config = JSON.parse(m.config);
          ruleType = config.type || "exact";
        }
      } catch {
        // use defaults
      }

      return {
        type: ruleType,
        metric_name: m.name,
        metric_kind: m.kind,
        weight: config.weight || 1.0,
        ...(ruleType === "latency_threshold" ? { max_ms: config.max_ms || 5000 } : {}),
        ...(ruleType === "required_tool_called" ? { tool_name: config.tool_name || "" } : {}),
        ...(config.value ? { value: config.value } : {}),
        ...(config.pattern ? { pattern: config.pattern } : {}),
      };
    });

    return {
      name: "multi-eval-config",
      type: "multi",
      aggregation: "weighted_average",
      pass_threshold: 0.7,
      multi_metrics: metrics,
    };
  }, [useMultiEval, selectedMetrics]);

  const handleSubmit = async (values: {
    dataset_id: string;
    target_name: string;
    target_version?: string;
    case_ids: string;
    evaluator_id?: string;
  }) => {
    const ids = values.case_ids
      ? values.case_ids
          .split("\n")
          .map((s) => s.trim())
          .filter(Boolean)
      : [];

    if (ids.length === 0) {
      message.warning("请至少输入一个用例 ID");
      return;
    }

    try {
      const body: Record<string, any> = {
        dataset_id: values.dataset_id.trim(),
        target_name: values.target_name.trim(),
        target_version: values.target_version?.trim() || undefined,
        case_ids: ids,
      };

      // If using existing evaluator, pass its ID
      if (values.evaluator_id) {
        body.evaluator_id = values.evaluator_id;
      }

      // If building a multi-evaluator config, pass it inline
      const evalConfig = buildEvaluatorConfig();
      if (evalConfig) {
        body.evaluator_config = evalConfig;
      }

      await create.mutateAsync(body as any);
      form.resetFields();
      setSelectedMetrics([]);
      setUseMultiEval(false);
      message.success("实验创建成功");
      onCreated?.();
    } catch (err: unknown) {
      message.error(err instanceof Error ? err.message : "创建实验失败");
    }
  };

  return (
    <Card
      title={
        <span>
          <ExperimentOutlined style={{ marginRight: 8 }} />
          创建实验
        </span>
      }
    >
      <Form
        form={form}
        layout="vertical"
        onFinish={handleSubmit}
        requiredMark={false}
        initialValues={{ dataset_id: datasetId || "" }}
      >
        <Form.Item
          name="dataset_id"
          label="数据集 ID"
          rules={[{ required: true, message: "请输入数据集 ID" }]}
          help="请填写数据集的 UUID"
        >
          <Input placeholder="d123e4567-e89b-12d3-a456-426614174000" />
        </Form.Item>

        <Form.Item
          name="target_name"
          label="目标 Agent"
          rules={[{ required: true, message: "请输入目标 Agent 名称" }]}
        >
          <Input placeholder="例如：support-agent" />
        </Form.Item>

        <Form.Item name="target_version" label="版本（可选）">
          <Input placeholder="例如：1.0.0" />
        </Form.Item>

        <Form.Item
          name="case_ids"
          label="用例 ID 列表"
          rules={[{ required: true, message: "请至少输入一个用例 ID" }]}
          extra="每行输入一个用例 ID"
        >
          <Input.TextArea
            rows={4}
            placeholder={"case-uuid-1\ncase-uuid-2\ncase-uuid-3"}
          />
        </Form.Item>

        {/* Existing evaluator selector */}
        <Form.Item name="evaluator_id" label="选择已有 Evaluator（可选）">
          <Select
            allowClear
            placeholder="不选择则使用默认评估"
            options={(evaluators || []).map((ev: any) => ({
              value: ev.id,
              label: `${ev.name} (${ev.type})`,
            }))}
          />
        </Form.Item>

        {/* Multi-dimensional evaluator config */}
        <Card
          size="small"
          title={
            <Space>
              <Text strong>多维评测配置</Text>
              <Switch checked={useMultiEval} onChange={handleToggleMultiEval} />
            </Space>
          }
          style={{ marginBottom: 16 }}
        >
          {useMultiEval ? (
            <div>
              <Text type="secondary" style={{ display: "block", marginBottom: 8 }}>
                选择要使用的评测指标（从已创建的 Metric Definitions 中选择）
              </Text>
              {!metricDefs || metricDefs.length === 0 ? (
                <Text type="warning">
                  ⚠ 暂无可用评测指标。请先在"评测配置中心"创建指标。
                </Text>
              ) : (
                <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
                  {metricDefs.map((m) => {
                    const isSelected = selectedMetrics.some((sm) => sm.id === m.id);
                    return (
                      <Tag
                        key={m.id}
                        color={isSelected ? "blue" : "default"}
                        style={{ cursor: "pointer", padding: "4px 8px" }}
                        onClick={() => handleMetricToggle(m, !isSelected)}
                      >
                        {m.name}
                        <Text style={{ fontSize: 10, marginLeft: 4 }} type="secondary">
                          ({METRIC_KIND_OPTIONS[m.kind] || m.kind})
                        </Text>
                      </Tag>
                    );
                  })}
                </div>
              )}

              {selectedMetrics.length > 0 && (
                <div style={{ marginTop: 8 }}>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    已选择 {selectedMetrics.length} 个指标，将使用加权平均计算综合分
                  </Text>
                </div>
              )}
            </div>
          ) : (
            <Text type="secondary">
              {useMultiEval ? "" : "未配置多维评测，将只记录执行成功状态。切换开关可启用多维评测。"}
            </Text>
          )}
        </Card>

        <Button type="primary" htmlType="submit" loading={create.isPending}>
          创建实验
        </Button>
      </Form>
    </Card>
  );
}
