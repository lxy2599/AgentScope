import { useState } from "react";
import { useParams } from "react-router-dom";
import {
  Card,
  Table,
  Button,
  Modal,
  Input,
  InputNumber,
  Switch,
  Space,
  Typography,
  Tag,
  Popconfirm,
  App,
  Empty,
  Spin,
  Select,
  Divider,
} from "antd";
import {
  PlusOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import { useSamplingRules, useCreateSamplingRule, useUpdateSamplingRule, useDeleteSamplingRule } from "../../api/hooks";
import type { SamplingRule } from "../../api/types";

const { Title, Text } = Typography;

function samplingCondition(rule: SamplingRule): string {
  let filters: Record<string, number> = {};
  try {
    filters = JSON.parse(rule.filters || "{}") as Record<string, number>;
  } catch {
    return "配置无法解析";
  }
  switch (rule.strategy) {
    case "failure": return "运行失败或包含错误";
    case "high_cost": return `成本 ≥ ${filters.min_cost ?? 1}`;
    case "low_confidence": return `置信度 ≤ ${filters.max_confidence ?? 0.6}`;
    case "high_disagreement": return `分歧度 ≥ ${filters.min_disagreement ?? 0.3}`;
    case "combined": return `失败，或成本 ≥ ${filters.min_cost ?? 1}，或置信度 ≤ ${filters.max_confidence ?? 0.6}，或分歧度 ≥ ${filters.min_disagreement ?? 0.3}`;
    default: return "不附加风险条件";
  }
}

export function SamplingRulesPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const { message } = App.useApp();
  const { data: rules, isLoading } = useSamplingRules(projectId!);
  const createRule = useCreateSamplingRule(projectId!);
  const updateRule = useUpdateSamplingRule();
  const deleteRule = useDeleteSamplingRule();

  const [modalOpen, setModalOpen] = useState(false);
  const [ruleName, setRuleName] = useState("");
  const [sampleRate, setSampleRate] = useState(1.0);
  const [strategy, setStrategy] = useState<SamplingRule["strategy"]>("random");
  const [dailyBudget, setDailyBudget] = useState(0);
  const [minCost, setMinCost] = useState(1);
  const [maxConfidence, setMaxConfidence] = useState(0.6);
  const [minDisagreement, setMinDisagreement] = useState(0.3);
  const [stratifyByStatus, setStratifyByStatus] = useState(false);
  const [completedBudget, setCompletedBudget] = useState(0);
  const [failedBudget, setFailedBudget] = useState(0);

  const handleCreate = async () => {
    if (!ruleName.trim()) {
      message.warning("请输入规则名称");
      return;
    }
    try {
      const filters = JSON.stringify({
        ...(["high_cost", "combined"].includes(strategy) ? { min_cost: minCost } : {}),
        ...(["low_confidence", "combined"].includes(strategy) ? { max_confidence: maxConfidence } : {}),
        ...(["high_disagreement", "combined"].includes(strategy) ? { min_disagreement: minDisagreement } : {}),
      });
      const stratification = stratifyByStatus
        ? JSON.stringify({ field: "status", budgets: { completed: completedBudget, failed: failedBudget } })
        : "{}";
      await createRule.mutateAsync({
        name: ruleName,
        sample_rate: sampleRate,
        strategy,
        daily_budget: dailyBudget,
        filters,
        stratification,
      });
      message.success("规则创建成功");
      setModalOpen(false);
      setRuleName("");
      setSampleRate(1.0);
      setStrategy("random");
      setDailyBudget(0);
      setMinCost(1);
      setMaxConfidence(0.6);
      setMinDisagreement(0.3);
      setStratifyByStatus(false);
      setCompletedBudget(0);
      setFailedBudget(0);
    } catch {
      message.error("创建失败");
    }
  };

  const handleToggle = async (ruleId: string, enabled: boolean) => {
    try {
      await updateRule.mutateAsync({ ruleId, enabled });
      message.success(enabled ? "规则已启用" : "规则已禁用");
    } catch {
      message.error("更新失败");
    }
  };

  const handleDelete = async (ruleId: string) => {
    try {
      await deleteRule.mutateAsync(ruleId);
      message.success("规则已删除");
    } catch {
      message.error("删除失败");
    }
  };

  const columns = [
    {
      title: "策略",
      dataIndex: "strategy",
      key: "strategy",
      render: (value: string) => <Tag color="blue">{value}</Tag>,
    },
    {
      title: "每日预算",
      dataIndex: "daily_budget",
      key: "daily_budget",
      render: (value: number) => value === 0 ? "不限" : value,
    },
    {
      title: "规则名称",
      dataIndex: "name",
      key: "name",
    },
    {
      title: "采样率",
      dataIndex: "sample_rate",
      key: "sample_rate",
      render: (rate: number) => `${(rate * 100).toFixed(0)}%`,
    },
    {
      title: "状态",
      dataIndex: "enabled",
      key: "enabled",
      render: (_: boolean, record: SamplingRule) => (
        <Switch
          checked={record.enabled}
          onChange={(checked) => handleToggle(record.id, checked)}
          checkedChildren="启用"
          unCheckedChildren="禁用"
        />
      ),
    },
    {
      title: "过滤器",
      dataIndex: "strategy",
      key: "filters",
      render: (_: string, record: SamplingRule) => <Text>{samplingCondition(record)}</Text>,
    },
    {
      title: "操作",
      key: "action",
      render: (_: unknown, record: SamplingRule) => (
        <Popconfirm
          title="确定删除此规则？"
          onConfirm={() => handleDelete(record.id)}
          okText="确定"
          cancelText="取消"
        >
          <Button type="link" danger>删除</Button>
        </Popconfirm>
      ),
    },
  ];

  return (
    <Space orientation="vertical" size="large" style={{ width: "100%" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <div>
          <Title level={3} style={{ margin: 0 }}>
            <SettingOutlined /> 线上评测抽样规则
          </Title>
          <Text type="secondary">
            配置线上 Agent 运行完成后自动触发评测的规则
          </Text>
        </div>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setModalOpen(true)}>
          创建规则
        </Button>
      </div>

      <Card>
        {isLoading ? (
          <Spin />
        ) : !rules || rules.length === 0 ? (
          <Empty description="暂无抽样规则，请创建" image={Empty.PRESENTED_IMAGE_SIMPLE} />
        ) : (
          <Table
            dataSource={rules}
            columns={columns}
            rowKey="id"
            pagination={false}
            size="small"
          />
        )}
      </Card>

      <Modal
        title="创建抽样规则"
        open={modalOpen}
        onOk={handleCreate}
        onCancel={() => setModalOpen(false)}
        confirmLoading={createRule.isPending}
        okText="创建"
        cancelText="取消"
      >
        <Space orientation="vertical" style={{ width: "100%" }}>
          <div>
            <Text>规则名称</Text>
            <Input
              placeholder="如：50% 采样"
              value={ruleName}
              onChange={(e) => setRuleName(e.target.value)}
            />
          </div>
          <div>
            <Text>采样策略</Text>
            <Select
              value={strategy}
              onChange={setStrategy}
              style={{ width: "100%" }}
              options={[
                { value: "random", label: "随机" },
                { value: "failure", label: "失败 Run" },
                { value: "high_cost", label: "高成本" },
                { value: "low_confidence", label: "低置信" },
                { value: "high_disagreement", label: "高分歧" },
                { value: "combined", label: "组合风险" },
              ]}
            />
          </div>
          <div>
            <Text>采样率（0~1）</Text>
            <InputNumber
              min={0}
              max={1}
              step={0.1}
              value={sampleRate}
              onChange={(v) => setSampleRate(v ?? 1.0)}
              style={{ width: "100%" }}
            />
          </div>
          <div>
            <Text>每日预算（0 表示不限）</Text>
            <InputNumber
              min={0}
              value={dailyBudget}
              onChange={(value) => setDailyBudget(value ?? 0)}
              style={{ width: "100%" }}
            />
          </div>
          <div>
            {["high_cost", "combined"].includes(strategy) && (
              <div style={{ marginBottom: 12 }}>
                <Text>最低成本</Text>
                <InputNumber min={0} step={0.1} value={minCost} onChange={(value) => setMinCost(value ?? 1)} style={{ width: "100%" }} />
              </div>
            )}
            {["low_confidence", "combined"].includes(strategy) && (
              <div style={{ marginBottom: 12 }}>
                <Text>最大置信度</Text>
                <InputNumber min={0} max={1} step={0.05} value={maxConfidence} onChange={(value) => setMaxConfidence(value ?? 0.6)} style={{ width: "100%" }} />
              </div>
            )}
            {["high_disagreement", "combined"].includes(strategy) && (
              <div style={{ marginBottom: 12 }}>
                <Text>最小分歧度</Text>
                <InputNumber min={0} max={1} step={0.05} value={minDisagreement} onChange={(value) => setMinDisagreement(value ?? 0.3)} style={{ width: "100%" }} />
              </div>
            )}
            {strategy === "failure" && <Text type="secondary">将采样状态不是 completed 或包含错误信息的运行。</Text>}
            {strategy === "random" && <Text type="secondary">仅按上方采样率随机选择运行。</Text>}
          </div>

          <Divider plain>分层预算</Divider>
          <Space>
            <Switch checked={stratifyByStatus} onChange={setStratifyByStatus} />
            <Text>按运行状态分别限制预算</Text>
          </Space>
          {stratifyByStatus && (
            <Space style={{ width: "100%" }}>
              <div style={{ flex: 1 }}>
                <Text>已完成运行预算</Text>
                <InputNumber min={0} value={completedBudget} onChange={(value) => setCompletedBudget(value ?? 0)} style={{ width: "100%" }} />
              </div>
              <div style={{ flex: 1 }}>
                <Text>失败运行预算</Text>
                <InputNumber min={0} value={failedBudget} onChange={(value) => setFailedBudget(value ?? 0)} style={{ width: "100%" }} />
              </div>
            </Space>
          )}
        </Space>
      </Modal>
    </Space>
  );
}
