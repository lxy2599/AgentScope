import { Alert, Space, Spin, Table, Tag, Typography } from "antd";
import { WarningOutlined } from "@ant-design/icons";
import { useProjectAnomalies } from "../../api/hooks";
import type {
  OperationalAnomaly,
  OperationalAnomalyStatus,
  OperationalMetric,
} from "../../api/types";

const { Text, Title } = Typography;

const metricLabels: Record<OperationalMetric, string> = {
  run_latency_ms: "运行延迟",
  llm_total_tokens: "LLM Token",
  llm_cost: "LLM 成本",
  llm_ttft_ms: "首 Token 延迟",
  tool_error_rate: "工具错误率",
  ingest_reject_rate: "采集拒绝率",
};

const statusLabels: Record<
  OperationalAnomalyStatus,
  { label: string; color: string }
> = {
  anomaly: { label: "异常", color: "error" },
  normal: { label: "正常", color: "success" },
  insufficient_baseline: { label: "基线不足", color: "warning" },
};

function formatValue(value: number) {
  return value.toLocaleString("zh-CN", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

export default function AnomalyPanel({ projectId }: { projectId: string }) {
  const { data, isLoading, error } = useProjectAnomalies(projectId);

  if (isLoading) {
    return <Spin size="small" />;
  }
  if (error) {
    return (
      <Alert
        type="warning"
        showIcon
        message="运行异常数据暂不可用"
        description={error instanceof Error ? error.message : "无法读取异常检测结果"}
      />
    );
  }

  return (
    <section aria-labelledby="anomaly-heading">
      <Space orientation="vertical" size={2} style={{ marginBottom: 16 }}>
        <Title id="anomaly-heading" level={5} style={{ margin: 0 }}>
          <WarningOutlined style={{ marginRight: 8 }} />
          运行异常
        </Title>
        <Text type="secondary">按 Agent 版本和环境对比滚动基线</Text>
      </Space>
      <Table<OperationalAnomaly>
        rowKey="id"
        size="small"
        scroll={{ x: 980 }}
        dataSource={data ?? []}
        pagination={{ pageSize: 8, hideOnSinglePage: true }}
        locale={{ emptyText: "当前窗口没有异常检测记录" }}
        columns={[
          {
            title: "状态",
            dataIndex: "status",
            fixed: "left",
            width: 96,
            render: (value: OperationalAnomalyStatus) => (
              <Tag color={statusLabels[value].color}>{statusLabels[value].label}</Tag>
            ),
          },
          {
            title: "Agent",
            width: 180,
            render: (_, row) => (
              <Space orientation="vertical" size={0}>
                <Text>{row.group.agent_name}</Text>
                <Text type="secondary">
                  {row.group.agent_version} · {row.group.environment}
                </Text>
              </Space>
            ),
          },
          {
            title: "指标",
            width: 130,
            render: (_, row) => metricLabels[row.group.metric],
          },
          {
            title: "当前值",
            dataIndex: "observed_value",
            align: "right",
            render: formatValue,
          },
          {
            title: "基线中位数",
            dataIndex: "baseline_median",
            align: "right",
            render: (value: number, row) =>
              row.status === "insufficient_baseline" ? "—" : formatValue(value),
          },
          {
            title: "偏差",
            dataIndex: "deviation_score",
            align: "right",
            render: (value: number, row) =>
              row.status === "insufficient_baseline" ? "—" : `${formatValue(value)} MAD`,
          },
          {
            title: "样本 / 基线",
            align: "right",
            render: (_, row) => `${row.sample_count} / ${row.baseline_sample_count}`,
          },
          {
            title: "窗口结束",
            dataIndex: "window_end",
            width: 170,
            render: (value: string) => new Date(value).toLocaleString("zh-CN"),
          },
        ]}
      />
    </section>
  );
}
