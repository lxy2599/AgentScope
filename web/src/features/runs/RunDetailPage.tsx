import { useState } from "react";
import { useParams } from "react-router-dom";
import {
  Card,
  Descriptions,
  Table,
  Tag,
  Tabs,
  Typography,
  Spin,
  Empty,
  Collapse,
  Space,
  Statistic,
  Row,
  Col,
} from "antd";
import { useRunDetail } from "../../api/hooks";
import { SpanTree } from "./SpanTree";
import { EventTable } from "./EventTable";
import type { LLMCallRecord, ToolCallRecord, RetrievalCallRecord, SkillCallRecord } from "../../api/types";

const { Title, Text } = Typography;

const statusTag = (status: string) => {
  const map: Record<string, { color: string; text: string }> = {
    pending: { color: "default", text: "待开始" },
    running: { color: "processing", text: "运行中" },
    completed: { color: "success", text: "已完成" },
    succeeded: { color: "success", text: "成功" },
    failed: { color: "error", text: "失败" },
    error: { color: "error", text: "错误" },
  };
  const s = map[status] || { color: "default", text: status };
  return <Tag color={s.color}>{s.text}</Tag>;
};

// ---- LLM calls table columns ----
const llmColumns = [
  { title: "Provider", dataIndex: "provider", key: "provider" },
  { title: "模型", dataIndex: "model", key: "model" },
  { title: "输入摘要", dataIndex: "input_summary", key: "input_summary", ellipsis: true },
  { title: "输出摘要", dataIndex: "output_summary", key: "output_summary", ellipsis: true },
  { title: "输入 Tokens", dataIndex: "input_tokens", key: "input_tokens", render: (v: number | null) => v ?? "—" },
  { title: "输出 Tokens", dataIndex: "output_tokens", key: "output_tokens", render: (v: number | null) => v ?? "—" },
  { title: "总 Tokens", dataIndex: "total_tokens", key: "total_tokens", render: (v: number | null) => v ?? "—" },
  { title: "成本", dataIndex: "cost", key: "cost", render: (v: number | null) => v != null ? `$${v.toFixed(6)}` : "—" },
  { title: "延迟", dataIndex: "duration_ms", key: "duration_ms", render: (v: number | null) => v != null ? `${v}ms` : "—" },
  { title: "状态", dataIndex: "status", key: "status", render: (v: string) => statusTag(v) },
];

// ---- Tool calls table columns ----
const toolColumns = [
  { title: "工具名称", dataIndex: "tool_name", key: "tool_name" },
  { title: "类型", dataIndex: "tool_type", key: "tool_type", render: (v: string) => v || "—" },
  { title: "参数摘要", dataIndex: "arguments_summary", key: "arguments_summary", ellipsis: true },
  { title: "结果摘要", dataIndex: "result_summary", key: "result_summary", ellipsis: true },
  {
    title: "状态", dataIndex: "success", key: "success",
    render: (v: boolean) => v ? <Tag color="success">成功</Tag> : <Tag color="error">失败</Tag>,
  },
  { title: "重试次数", dataIndex: "retry_count", key: "retry_count" },
  { title: "风险等级", dataIndex: "side_effect_risk", key: "side_effect_risk", render: (v: string) => v || "—" },
  { title: "延迟", dataIndex: "duration_ms", key: "duration_ms", render: (v: number | null) => v != null ? `${v}ms` : "—" },
];

// ---- Retrieval calls table columns ----
const retrievalColumns = [
  { title: "知识库", dataIndex: "knowledge_base", key: "knowledge_base" },
  { title: "查询", dataIndex: "query_text", key: "query_text", ellipsis: true },
  { title: "请求 TopK", dataIndex: "top_k", key: "top_k", render: (v: number | null) => v ?? "—" },
  { title: "返回数量", dataIndex: "returned_count", key: "returned_count", render: (v: number | null) => v ?? "—" },
  { title: "文档 ID", dataIndex: "document_ids", key: "document_ids", render: (v: string[] | undefined) => v?.join(", ") || "—" },
  { title: "分数", dataIndex: "scores", key: "scores", render: (v: number[] | undefined) => v?.map(s => s.toFixed(3)).join(", ") || "—" },
  { title: "延迟", dataIndex: "duration_ms", key: "duration_ms", render: (v: number | null) => v != null ? `${v}ms` : "—" },
];

// ---- Skill calls table columns ----
const skillColumns = [
  { title: "Skill 名称", dataIndex: "skill_name", key: "skill_name" },
  { title: "版本", dataIndex: "skill_version", key: "skill_version", render: (v: string) => v || "—" },
  { title: "输入摘要", dataIndex: "input_summary", key: "input_summary", ellipsis: true },
  { title: "输出摘要", dataIndex: "output_summary", key: "output_summary", ellipsis: true },
  {
    title: "状态", dataIndex: "success", key: "success",
    render: (v: boolean) => v ? <Tag color="success">成功</Tag> : <Tag color="error">失败</Tag>,
  },
  { title: "延迟", dataIndex: "duration_ms", key: "duration_ms", render: (v: number | null) => v != null ? `${v}ms` : "—" },
];

export function RunDetailPage() {
  const { runId } = useParams<{ runId: string }>();
  const { data, isLoading, error } = useRunDetail(runId ?? "");
  const [activeTab, setActiveTab] = useState("overview");

  if (isLoading) {
    return (
      <div style={{ textAlign: "center", padding: 64 }}>
        <Spin size="large" />
      </div>
    );
  }

  if (error) {
    return (
      <Card>
        <Text type="danger">
          {error instanceof Error ? error.message : "加载运行记录失败"}
        </Text>
      </Card>
    );
  }

  if (!data) {
    return (
      <Card>
        <Empty description="未找到该运行记录" image={Empty.PRESENTED_IMAGE_SIMPLE} />
      </Card>
    );
  }

  const { run, spans, events, llm_calls, tool_calls, retrieval_calls, skill_calls } = data;

  // Count tokens and cost
  const totalInputTokens = (llm_calls ?? []).reduce((s, c) => s + (c.input_tokens ?? 0), 0);
  const totalOutputTokens = (llm_calls ?? []).reduce((s, c) => s + (c.output_tokens ?? 0), 0);
  const totalTokens = (llm_calls ?? []).reduce((s, c) => s + (c.total_tokens ?? 0), 0);
  const totalCost = (llm_calls ?? []).reduce((s, c) => s + (c.cost ?? 0), 0);
  const totalDurationMs = run.latency_ms ?? (llm_calls ?? []).reduce((s, c) => s + (c.duration_ms ?? 0), 0);

  const collapseItems = [
    ...(run.input_summary
      ? [
          {
            key: "input",
            label: "输入",
            children: (
              <pre style={{ margin: 0, fontSize: 12, whiteSpace: "pre-wrap", wordBreak: "break-all" }}>
                {run.input_summary}
              </pre>
            ),
          },
        ]
      : []),
    ...(run.output_summary
      ? [
          {
            key: "output",
            label: "输出",
            children: (
              <pre style={{ margin: 0, fontSize: 12, whiteSpace: "pre-wrap", wordBreak: "break-all" }}>
                {run.output_summary}
              </pre>
            ),
          },
        ]
      : []),
    ...(run.error
      ? [
          {
            key: "error",
            label: "错误",
            children: (
              <pre style={{ margin: 0, fontSize: 12, whiteSpace: "pre-wrap", wordBreak: "break-all", color: "#ff4d4f" }}>
                {run.error}
              </pre>
            ),
          },
        ]
      : []),
  ];

  const hasDeepTrace = (llm_calls && llm_calls.length > 0) ||
    (tool_calls && tool_calls.length > 0) ||
    (retrieval_calls && retrieval_calls.length > 0) ||
    (skill_calls && skill_calls.length > 0);

  const tabItems = [
    ...(hasDeepTrace
      ? [
          {
            key: "overview",
            label: "调用概览",
            children: (
              <Row gutter={[16, 16]}>
                <Col span={6}>
                  <Card size="small"><Statistic title="LLM 调用" value={llm_calls?.length ?? 0} suffix="次" /></Card>
                </Col>
                <Col span={6}>
                  <Card size="small"><Statistic title="工具调用" value={tool_calls?.length ?? 0} suffix="次" /></Card>
                </Col>
                <Col span={6}>
                  <Card size="small"><Statistic title="知识库检索" value={retrieval_calls?.length ?? 0} suffix="次" /></Card>
                </Col>
                <Col span={6}>
                  <Card size="small"><Statistic title="Skill 调用" value={skill_calls?.length ?? 0} suffix="次" /></Card>
                </Col>
                <Col span={6}>
                  <Card size="small"><Statistic title="输入 Tokens" value={totalInputTokens} /></Card>
                </Col>
                <Col span={6}>
                  <Card size="small"><Statistic title="输出 Tokens" value={totalOutputTokens} /></Card>
                </Col>
                <Col span={6}>
                  <Card size="small"><Statistic title="总 Tokens" value={totalTokens} /></Card>
                </Col>
                <Col span={6}>
                  <Card size="small"><Statistic title="总成本" value={`$${totalCost.toFixed(6)}`} /></Card>
                </Col>
                <Col span={6}>
                  <Card size="small"><Statistic title="总耗时" value={`${totalDurationMs}ms`} /></Card>
                </Col>
              </Row>
            ),
          },
          {
            key: "llm",
            label: `LLM 调用 (${llm_calls?.length ?? 0})`,
            children: (
              <Table
                dataSource={llm_calls ?? []}
                columns={llmColumns}
                rowKey="span_id"
                size="small"
                locale={{ emptyText: "暂无 LLM 调用记录" }}
                pagination={false}
              />
            ),
          },
          {
            key: "tool",
            label: `工具调用 (${tool_calls?.length ?? 0})`,
            children: (
              <Table
                dataSource={tool_calls ?? []}
                columns={toolColumns}
                rowKey="span_id"
                size="small"
                locale={{ emptyText: "暂无工具调用记录" }}
                pagination={false}
              />
            ),
          },
          {
            key: "retrieval",
            label: `知识库检索 (${retrieval_calls?.length ?? 0})`,
            children: (
              <Table
                dataSource={retrieval_calls ?? []}
                columns={retrievalColumns}
                rowKey="span_id"
                size="small"
                locale={{ emptyText: "暂无知识库检索记录" }}
                pagination={false}
              />
            ),
          },
          {
            key: "skill",
            label: `Skill 调用 (${skill_calls?.length ?? 0})`,
            children: (
              <Table
                dataSource={skill_calls ?? []}
                columns={skillColumns}
                rowKey="span_id"
                size="small"
                locale={{ emptyText: "暂无 Skill 调用记录" }}
                pagination={false}
              />
            ),
          },
        ]
      : []),
    {
      key: "spans",
      label: `原始 Span (${spans.length})`,
      children: spans.length > 0 ? <SpanTree spans={spans} /> : <Empty description="暂无 Span" image={Empty.PRESENTED_IMAGE_SIMPLE} />,
    },
    ...(events.length > 0
      ? [
          {
            key: "events",
            label: `事件 (${events.length})`,
            children: <EventTable events={events} />,
          },
        ]
      : []),
  ];

  return (
    <Space orientation="vertical" size="large" style={{ width: "100%" }}>
      {/* Header */}
      <Card>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start" }}>
          <div>
            <Title level={3} style={{ margin: 0 }}>
              运行诊断
            </Title>
            <Text type="secondary" style={{ fontSize: 13 }}>
              {run.agent_name}
            </Text>
          </div>
          {statusTag(run.status)}
        </div>
      </Card>

      {/* Basic info */}
      <Card title="基本信息">
        <Descriptions column={2} size="small">
          <Descriptions.Item label="Agent 名称">
            {run.agent_name}
          </Descriptions.Item>
          <Descriptions.Item label="状态">
            {statusTag(run.status)}
          </Descriptions.Item>
          <Descriptions.Item label="来源">
            {run.source}
          </Descriptions.Item>
          <Descriptions.Item label="版本">
            {run.agent_version || "—"}
          </Descriptions.Item>
          <Descriptions.Item label="Trace ID">
            <Text code style={{ fontSize: 12 }}>
              {run.trace_id}
            </Text>
          </Descriptions.Item>
          <Descriptions.Item label="延迟">
            {run.latency_ms != null ? `${run.latency_ms}ms` : "—"}
          </Descriptions.Item>
        </Descriptions>

        {collapseItems.length > 0 && (
          <Collapse
            items={collapseItems}
            style={{ marginTop: 16 }}
            size="small"
          />
        )}
      </Card>

      {/* Tabs for Deep Trace */}
      <Card>
        <Tabs activeKey={activeTab} onChange={setActiveTab} items={tabItems} />
      </Card>
    </Space>
  );
}
