import { useCallback, useEffect, useRef, useState } from "react";
import {
  Alert,
  App,
  AutoComplete,
  Button,
  Card,
  Col,
  Empty,
  Form,
  Input,
  Row,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from "antd";
import { ExperimentOutlined } from "@ant-design/icons";
import {
  useAgents,
  useCreateExperiment,
  useDatasetCases,
  useDatasets,
  useEvaluators,
  useMetricDefinitions,
} from "../../api/hooks";
import type { DatasetCase, MetricDefinition } from "../../api/types";
import { displayJsonValue, evaluatorSummary, formatDateTime, shortId } from "../../utils/display";

const { Text, Title } = Typography;

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

interface ExperimentFormValues {
  dataset_id: string;
  target_name: string;
  target_version?: string;
  evaluator_id?: string;
}

export function CreateExperimentPanel({ projectId, datasetId, onCreated }: Props) {
  const create = useCreateExperiment(projectId);
  const { data: datasets = [] } = useDatasets(projectId);
  const { data: evaluators = [] } = useEvaluators(projectId);
  const { data: metricDefs = [] } = useMetricDefinitions(projectId);
  const { data: agents = [] } = useAgents(projectId);
  const [form] = Form.useForm<ExperimentFormValues>();
  const selectedDatasetId = Form.useWatch("dataset_id", form);
  const { data: cases = [], isLoading: casesLoading } = useDatasetCases(selectedDatasetId || "");
  const { message } = App.useApp();

  const [selectedCaseIds, setSelectedCaseIds] = useState<React.Key[]>([]);
  const [useMultiEval, setUseMultiEval] = useState(false);
  const [selectedMetrics, setSelectedMetrics] = useState<MetricDefinition[]>([]);
  const autoSelectedDataset = useRef("");

  useEffect(() => {
    if (!datasetId) return;
    autoSelectedDataset.current = "";
    setSelectedCaseIds([]);
    form.setFieldValue("dataset_id", datasetId);
  }, [datasetId, form]);

  useEffect(() => {
    if (!selectedDatasetId || casesLoading || autoSelectedDataset.current === selectedDatasetId) return;
    setSelectedCaseIds(cases.map((item) => item.id));
    autoSelectedDataset.current = selectedDatasetId;
  }, [cases, casesLoading, selectedDatasetId]);

  const changeDataset = useCallback((value: string) => {
    autoSelectedDataset.current = "";
    setSelectedCaseIds([]);
    form.setFieldValue("dataset_id", value);
  }, [form]);

  const handleToggleMultiEval = useCallback((checked: boolean) => {
    setUseMultiEval(checked);
    if (checked) {
      form.setFieldValue("evaluator_id", undefined);
    } else {
      setSelectedMetrics([]);
    }
  }, [form]);

  const handleMetricToggle = useCallback((metric: MetricDefinition, checked: boolean) => {
    setSelectedMetrics((previous) => checked
      ? [...previous.filter((item) => item.id !== metric.id), metric]
      : previous.filter((item) => item.id !== metric.id));
  }, []);

  const buildEvaluatorConfig = useCallback(() => {
    if (!useMultiEval || selectedMetrics.length === 0) return undefined;
    const metrics = selectedMetrics.map((metric) => {
      let ruleType = "exact";
      let config: Record<string, unknown> = {};
      try {
        if (metric.config) {
          config = JSON.parse(metric.config) as Record<string, unknown>;
          ruleType = String(config.type || "exact");
        }
      } catch {
        // Keep safe defaults for legacy metric definitions.
      }
      return {
        type: ruleType,
        metric_name: metric.name,
        metric_kind: metric.kind,
        weight: Number(config.weight || 1),
        ...(ruleType === "latency_threshold" ? { max_ms: Number(config.max_ms || 5000) } : {}),
        ...(ruleType === "required_tool_called" ? { tool_name: String(config.tool_name || "") } : {}),
        ...(config.value ? { value: config.value } : {}),
        ...(config.pattern ? { pattern: config.pattern } : {}),
      };
    });
    return {
      name: `experiment-evaluator-${Date.now()}`,
      type: "multi",
      aggregation: "weighted_average",
      pass_threshold: 0.7,
      multi_metrics: metrics,
    };
  }, [selectedMetrics, useMultiEval]);

  const handleSubmit = async (values: ExperimentFormValues) => {
    if (selectedCaseIds.length === 0) {
      message.warning("请至少选择一个评测用例");
      return;
    }
    if (useMultiEval && selectedMetrics.length === 0) {
      message.warning("请至少选择一个评测指标，或关闭多维评测");
      return;
    }
    try {
      const body: Record<string, unknown> = {
        dataset_id: values.dataset_id,
        target_name: values.target_name.trim(),
        target_version: values.target_version?.trim() || undefined,
        case_ids: selectedCaseIds.map(String),
      };
      if (values.evaluator_id) body.evaluator_id = values.evaluator_id;
      const evaluatorConfig = buildEvaluatorConfig();
      if (evaluatorConfig) body.evaluator_config = evaluatorConfig;

      await create.mutateAsync(body as {
        dataset_id: string;
        evaluator_id?: string;
        target_name: string;
        target_version?: string;
        case_ids: string[];
        evaluator_config?: Record<string, unknown>;
      });
      form.resetFields();
      autoSelectedDataset.current = "";
      setSelectedCaseIds([]);
      setSelectedMetrics([]);
      setUseMultiEval(false);
      if (datasetId) form.setFieldValue("dataset_id", datasetId);
      message.success("实验已创建，评测任务正在排队");
      onCreated?.();
    } catch (error: unknown) {
      message.error(error instanceof Error ? error.message : "创建实验失败");
    }
  };

  const caseColumns = [
    {
      title: "用例输入",
      dataIndex: "input",
      key: "input",
      ellipsis: true,
      render: (value: string) => <Text>{displayJsonValue(value)}</Text>,
    },
    {
      title: "期望输出",
      dataIndex: "expected",
      key: "expected",
      ellipsis: true,
      render: (value?: string) => <Text type={value ? undefined : "secondary"}>{displayJsonValue(value)}</Text>,
    },
  ];

  return (
    <Card
      title={<Space><ExperimentOutlined /><span>创建实验</span></Space>}
      styles={{ body: { paddingTop: 18 } }}
    >
      <Form<ExperimentFormValues>
        form={form}
        layout="vertical"
        onFinish={handleSubmit}
        requiredMark={false}
        initialValues={{ dataset_id: datasetId }}
      >
        <Row gutter={20}>
          <Col xs={24} lg={14}>
            <Title level={5} style={{ marginTop: 0 }}>1. 选择评测样本</Title>
            <Form.Item
              name="dataset_id"
              label="数据集"
              rules={[{ required: true, message: "请选择数据集" }]}
            >
              <Select
                showSearch
                optionFilterProp="label"
                placeholder="按名称选择数据集"
                onChange={changeDataset}
                options={datasets.map((dataset) => ({
                  value: dataset.id,
                  label: dataset.description ? `${dataset.name} — ${dataset.description}` : dataset.name,
                }))}
                notFoundContent="当前项目还没有数据集"
              />
            </Form.Item>

            {selectedDatasetId ? (
              <Table<DatasetCase>
                rowKey="id"
                size="small"
                loading={casesLoading}
                dataSource={cases}
                columns={caseColumns}
                pagination={false}
                scroll={{ y: 260 }}
                rowSelection={{
                  selectedRowKeys: selectedCaseIds,
                  onChange: setSelectedCaseIds,
                }}
                locale={{ emptyText: <Empty description="这个数据集还没有用例" image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
              />
            ) : (
              <Empty description="先选择数据集，再勾选参与实验的用例" image={Empty.PRESENTED_IMAGE_SIMPLE} />
            )}
            {selectedDatasetId && cases.length > 0 && (
              <Text type="secondary" style={{ display: "block", marginTop: 8 }}>
                已选择 {selectedCaseIds.length}/{cases.length} 条用例；新数据集默认全选
              </Text>
            )}
          </Col>

          <Col xs={24} lg={10}>
            <Title level={5} style={{ marginTop: 0 }}>2. 指定运行目标</Title>
            <Form.Item
              name="target_name"
              label="目标 Agent / Target"
              rules={[{ required: true, message: "请选择或输入 Target 名称" }]}
              extra="可从已注册 Agent 中选择，也可以输入 Runner 中注册的自定义 Target"
            >
              <AutoComplete
                placeholder="例如：support-agent"
                options={agents.map((agent) => ({ value: agent.name, label: agent.name }))}
                filterOption={(input, option) => String(option?.value || "").toLowerCase().includes(input.toLowerCase())}
              />
            </Form.Item>
            <Form.Item name="target_version" label="Target 版本（可选）">
              <Input placeholder="例如：1.0.0" />
            </Form.Item>

            <Title level={5}>3. 选择评测方式</Title>
            <Form.Item name="evaluator_id" label="复用已有 Evaluator">
              <Select
                allowClear
                showSearch
                optionFilterProp="label"
                placeholder="留空则只记录执行状态"
                onChange={(value) => { if (value) handleToggleMultiEval(false); }}
                options={(Array.isArray(evaluators) ? evaluators : []).map((evaluator: any) => ({
                  value: evaluator.id,
                  label: `${evaluator.name} · ${evaluatorSummary(evaluator.config)} · ${formatDateTime(evaluator.created_at)} · ${shortId(evaluator.id)}`,
                }))}
              />
            </Form.Item>

            <Card
              size="small"
              title={<Space><Text strong>从指标组合评测</Text><Switch checked={useMultiEval} onChange={handleToggleMultiEval} /></Space>}
              style={{ marginBottom: 16 }}
            >
              {useMultiEval ? (
                metricDefs.length > 0 ? (
                  <Space wrap size={[8, 8]}>
                    {metricDefs.map((metric) => {
                      const selected = selectedMetrics.some((item) => item.id === metric.id);
                      return (
                        <Tag.CheckableTag
                          key={metric.id}
                          checked={selected}
                          onChange={(checked) => handleMetricToggle(metric, checked)}
                        >
                          {metric.name} · {METRIC_KIND_OPTIONS[metric.kind] || metric.kind}
                        </Tag.CheckableTag>
                      );
                    })}
                  </Space>
                ) : (
                  <Alert type="warning" showIcon title="暂无评测指标" description="请先在评测配置中心创建指标。" />
                )
              ) : (
                <Text type="secondary">需要组合多个指标时再打开；开启后不会同时使用上面的已有 Evaluator。</Text>
              )}
            </Card>

            <Alert
              type="info"
              showIcon
              title={`${selectedCaseIds.length} 条用例 · ${useMultiEval ? `${selectedMetrics.length} 个指标` : "复用或默认评测"}`}
              description="创建后，每条用例会生成一个可独立领取和重试的 Eval Job。"
              style={{ marginBottom: 16 }}
            />
            <Button type="primary" htmlType="submit" loading={create.isPending} block size="large">
              创建并排队评测
            </Button>
          </Col>
        </Row>
      </Form>
    </Card>
  );
}
