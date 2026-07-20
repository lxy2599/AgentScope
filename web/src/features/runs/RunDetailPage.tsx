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
  Timeline,
  Tooltip,
} from "antd";
import {
  CheckCircleOutlined,
  CloseCircleOutlined,
  LineChartOutlined,
  BulbOutlined,
  CheckSquareOutlined,
  RobotOutlined,
  ToolOutlined,
  DatabaseOutlined,
  ThunderboltOutlined,
} from "@ant-design/icons";
import { useRunDetail, useEvalScoresByRun } from "../../api/hooks";
import { SpanTree } from "./SpanTree";
import { EventTable } from "./EventTable";
import type { ThoughtRecord, FinalAnswerRecord, LLMCallRecord, ToolCallRecord, RetrievalCallRecord, SkillCallRecord } from "../../api/types";

const { Title, Text, Paragraph } = Typography;

// Metric kind display helpers
const METRIC_KIND_LABELS: Record<string, string> = {
  final_answer: "最终答案",
  trajectory: "轨迹",
  tool_correctness: "工具调用",
  argument_correctness: "论据正确性",
  rag_groundedness: "RAG 相关性",
  safety: "安全",
  efficiency: "效率",
};

const METRIC_KIND_COLORS: Record<string, string> = {
  final_answer: "#1890ff",
  trajectory: "#722ed1",
  tool_correctness: "#13c2c2",
  argument_correctness: "#fa8c16",
  rag_groundedness: "#52c41a",
  safety: "#eb2f96",
  efficiency: "#faad14",
};

const AGENT_KIND_CONFIG: Record<string, { color: string; icon: React.ReactNode; label: string }> = {
  THOUGHT: { color: "#fa8c16", icon: <BulbOutlined />, label: "思考" },
  FINAL_ANSWER: { color: "#1890ff", icon: <CheckSquareOutlined />, label: "最终答案" },
  LLM: { color: "#722ed1", icon: <RobotOutlined />, label: "LLM" },
  TOOL: { color: "#13c2c2", icon: <ToolOutlined />, label: "工具" },
  RETRIEVER: { color: "#52c41a", icon: <DatabaseOutlined />, label: "检索" },
  SKILL: { color: "#faad14", icon: <ThunderboltOutlined />, label: "Skill" },
};

function metricKindLabel(kind: string): string {
  return METRIC_KIND_LABELS[kind] || kind;
}

function metricKindColor(kind: string): string {
  return METRIC_KIND_COLORS[kind] || "#999";
}

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

// ---- Agent Timeline Item ----
interface TimelineItem {
  kind: string;
  name: string;
  summary: string;
  content?: string;
  durationMs?: number;
  status?: string;
  error?: string;
  startTime: string;
  details?: Record<string, unknown>;
}

function buildTimeline(
  thoughts?: ThoughtRecord[],
  finalAnswers?: FinalAnswerRecord[],
  llmCalls?: LLMCallRecord[],
  toolCalls?: ToolCallRecord[],
  retrievalCalls?: RetrievalCallRecord[],
  skillCalls?: SkillCallRecord[],
): TimelineItem[] {
  const items: TimelineItem[] = [];

  for (const t of thoughts ?? []) {
    items.push({
      kind: "THOUGHT",
      name: "思考",
      summary: t.summary,
      content: t.content,
      durationMs: t.duration_ms,
      startTime: t.start_time,
      details: { stage: t.stage, confidence: t.confidence, content_captured: t.content_captured },
    });
  }

  for (const fa of finalAnswers ?? []) {
    items.push({
      kind: "FINAL_ANSWER",
      name: "最终答案",
      summary: fa.answer_summary,
      content: fa.answer_output || fa.structured_output,
      durationMs: fa.duration_ms,
      startTime: fa.start_time,
      details: { structured_output: fa.structured_output },
    });
  }

  for (const l of llmCalls ?? []) {
    items.push({
      kind: "LLM",
      name: `${l.provider}/${l.model}`,
      summary: l.input_summary,
      content: l.output_summary,
      durationMs: l.duration_ms,
      status: l.status,
      error: l.error,
      startTime: l.start_time,
      details: { input_tokens: l.input_tokens, output_tokens: l.output_tokens, total_tokens: l.total_tokens, cost: l.cost },
    });
  }

  for (const t of toolCalls ?? []) {
    items.push({
      kind: "TOOL",
      name: t.tool_name,
      summary: t.arguments_summary,
      content: t.result_summary,
      durationMs: t.duration_ms,
      status: t.success ? "OK" : "ERROR",
      error: t.error,
      startTime: t.start_time,
      details: { tool_type: t.tool_type, retry_count: t.retry_count, side_effect_risk: t.side_effect_risk },
    });
  }

  for (const r of retrievalCalls ?? []) {
    items.push({
      kind: "RETRIEVER",
      name: r.knowledge_base,
      summary: r.query_text,
      durationMs: r.duration_ms,
      startTime: r.start_time,
      details: { top_k: r.top_k, returned_count: r.returned_count, document_ids: r.document_ids },
    });
  }

  for (const s of skillCalls ?? []) {
    items.push({
      kind: "SKILL",
      name: s.skill_name,
      summary: s.input_summary,
      content: s.output_summary,
      durationMs: s.duration_ms,
      status: s.success ? "OK" : "ERROR",
      error: s.error,
      startTime: s.start_time,
      details: { skill_version: s.skill_version },
    });
  }

  // Sort by start_time ascending
  items.sort((a, b) => new Date(a.startTime).getTime() - new Date(b.startTime).getTime());
  return items;
}

function AgentTimeline({ items }: { items: TimelineItem[] }) {
  if (items.length === 0) {
    return <Empty description="暂无 Agent 时间线记录" image={Empty.PRESENTED_IMAGE_SIMPLE} />;
  }

  return (
    <Timeline
      items={items.map((item, idx) => {
        const cfg = AGENT_KIND_CONFIG[item.kind] || { color: "#999", icon: null, label: item.kind };
        const durationStr = item.durationMs != null ? `${item.durationMs}ms` : null;

        return {
          key: idx,
          color: cfg.color,
          dot: cfg.icon,
          children: (
            <div style={{ marginBottom: 8 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 4 }}>
                <Tag color={cfg.color} style={{ marginRight: 0, flexShrink: 0 }}>{cfg.label}</Tag>
                <Text strong style={{ fontSize: 13, wordBreak: "break-all" }}>{item.name}</Text>
                {durationStr && (
                  <Text type="secondary" style={{ fontSize: 12, flexShrink: 0 }}>{durationStr}</Text>
                )}
                {item.status && item.status === "ERROR" && (
                  <Tag color="error" style={{ flexShrink: 0 }}>失败</Tag>
                )}
              </div>
              {item.summary && (
                <Paragraph
                  ellipsis={{ rows: 1, expandable: true, symbol: "展开" }}
                  style={{ fontSize: 12, marginBottom: 2, color: "#666" }}
                >
                  {item.summary}
                </Paragraph>
              )}
              {item.content && (
                <Paragraph
                  ellipsis={{ rows: 1, expandable: true, symbol: "展开" }}
                  style={{ fontSize: 12, marginBottom: 2 }}
                >
                  <Text code style={{ fontSize: 12, wordBreak: "break-all" }}>{item.content}</Text>
                </Paragraph>
              )}
              {item.error && (
                <Text type="danger" style={{ fontSize: 12 }}>
                  {item.error}
                </Text>
              )}
              {item.details && Object.keys(item.details).length > 0 && (
                <Descriptions size="small" column={1} style={{ marginTop: 4 }}>
                  {Object.entries(item.details).map(([k, v]) => {
                    if (v == null || v === "" || v === false) return null;
                    return (
                      <Descriptions.Item key={k} label={k} style={{ paddingBottom: 0 }}>
                        <Text style={{ fontSize: 12 }}>{String(v)}</Text>
                      </Descriptions.Item>
                    );
                  })}
                </Descriptions>
              )}
            </div>
          ),
        };
      })}
    />
  );
}

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
  const projectId = (data as { run?: { project_id?: string } } | undefined)?.run?.project_id ?? "";
  const { data: evalScores, isLoading: evalLoading } = useEvalScoresByRun(projectId, runId ?? "");
  const [activeTab, setActiveTab] = useState("timeline");

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

  const { run, spans, events, llm_calls, tool_calls, retrieval_calls, skill_calls, thoughts, final_answers } = data;

  // Build agent timeline
  const timelineItems = buildTimeline(thoughts, final_answers, llm_calls, tool_calls, retrieval_calls, skill_calls);

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

  const hasDeepTrace = timelineItems.length > 0;

  // ---- Agent Timeline Tab ----
  const timelineTab = {
    key: "timeline",
    label: `Agent 时间线 (${timelineItems.length})`,
    children: <AgentTimeline items={timelineItems} />,
  };

  // ---- Overview Tab ----
  const overviewTab = {
    key: "overview",
    label: "调用概览",
    children: (
      <Row gutter={[16, 16]}>
        <Col xs={12} sm={8} md={6}>
          <Card size="small"><Statistic title="思考" value={thoughts?.length ?? 0} suffix="次" prefix={<BulbOutlined />} /></Card>
        </Col>
        <Col xs={12} sm={8} md={6}>
          <Card size="small"><Statistic title="LLM 调用" value={llm_calls?.length ?? 0} suffix="次" prefix={<RobotOutlined />} /></Card>
        </Col>
        <Col xs={12} sm={8} md={6}>
          <Card size="small"><Statistic title="工具调用" value={tool_calls?.length ?? 0} suffix="次" prefix={<ToolOutlined />} /></Card>
        </Col>
        <Col xs={12} sm={8} md={6}>
          <Card size="small"><Statistic title="知识库检索" value={retrieval_calls?.length ?? 0} suffix="次" prefix={<DatabaseOutlined />} /></Card>
        </Col>
        <Col xs={12} sm={8} md={6}>
          <Card size="small"><Statistic title="Skill 调用" value={skill_calls?.length ?? 0} suffix="次" prefix={<ThunderboltOutlined />} /></Card>
        </Col>
        <Col xs={12} sm={8} md={6}>
          <Card size="small"><Statistic title="输入 Tokens" value={totalInputTokens} /></Card>
        </Col>
        <Col xs={12} sm={8} md={6}>
          <Card size="small"><Statistic title="输出 Tokens" value={totalOutputTokens} /></Card>
        </Col>
        <Col xs={12} sm={8} md={6}>
          <Card size="small"><Statistic title="总 Tokens" value={totalTokens} /></Card>
        </Col>
        <Col xs={12} sm={8} md={6}>
          <Card size="small"><Statistic title="总成本" value={`$${totalCost.toFixed(6)}`} /></Card>
        </Col>
        <Col xs={12} sm={8} md={6}>
          <Card size="small"><Statistic title="总耗时" value={`${totalDurationMs}ms`} /></Card>
        </Col>
      </Row>
    ),
  };

  // ---- Tab items ----
  const tabItems = [
    timelineTab,
    ...(hasDeepTrace ? [overviewTab] : []),
    ...(hasDeepTrace && llm_calls && llm_calls.length > 0 ? [
      {
        key: "llm",
        label: `LLM 调用 (${llm_calls.length})`,
        children: (
          <Table
            dataSource={llm_calls}
            columns={llmColumns}
            rowKey="span_id"
            size="small"
            locale={{ emptyText: "暂无 LLM 调用记录" }}
            pagination={false}
          />
        ),
      },
    ] : []),
    ...(hasDeepTrace && tool_calls && tool_calls.length > 0 ? [
      {
        key: "tool",
        label: `工具调用 (${tool_calls.length})`,
        children: (
          <Table
            dataSource={tool_calls}
            columns={toolColumns}
            rowKey="span_id"
            size="small"
            locale={{ emptyText: "暂无工具调用记录" }}
            pagination={false}
          />
        ),
      },
    ] : []),
    ...(hasDeepTrace && retrieval_calls && retrieval_calls.length > 0 ? [
      {
        key: "retrieval",
        label: `知识库检索 (${retrieval_calls.length})`,
        children: (
          <Table
            dataSource={retrieval_calls}
            columns={retrievalColumns}
            rowKey="span_id"
            size="small"
            locale={{ emptyText: "暂无知识库检索记录" }}
            pagination={false}
          />
        ),
      },
    ] : []),
    ...(hasDeepTrace && skill_calls && skill_calls.length > 0 ? [
      {
        key: "skill",
        label: `Skill 调用 (${skill_calls.length})`,
        children: (
          <Table
            dataSource={skill_calls}
            columns={skillColumns}
            rowKey="span_id"
            size="small"
            locale={{ emptyText: "暂无 Skill 调用记录" }}
            pagination={false}
          />
        ),
      },
    ] : []),
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
    <Space orientation="vertical" size="middle" style={{ width: "100%" }}>
      {/* Header */}
      <Card>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start" }}>
          <div>
            <Title level={4} style={{ margin: 0 }}>
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
      <Card size="small" title="基本信息">
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
            style={{ marginTop: 12 }}
            size="small"
          />
        )}
      </Card>

      {/* Eval Scores Section */}
      <Card size="small" title={<Space><LineChartOutlined /><span>评测分数</span></Space>}>
        {evalLoading ? (
          <div style={{ textAlign: "center", padding: 16 }}>
            <Spin size="small" />
          </div>
        ) : evalScores && evalScores.length > 0 ? (
          <Table
            dataSource={evalScores}
            columns={[
              {
                title: "指标", dataIndex: "metric_kind", key: "metric_kind", width: 140,
                render: (kind: string) => (
                  <Tag color={metricKindColor(kind)}>{metricKindLabel(kind)}</Tag>
                ),
              },
              {
                title: "指标名称", dataIndex: "metric_name", key: "metric_name", width: 160,
              },
              {
                title: "分数", dataIndex: "score", key: "score", width: 100,
                render: (score: number) => (
                  <span style={{ fontWeight: 600, color: score >= 0.7 ? "#52c41a" : "#ff4d4f" }}>
                    {(score * 100).toFixed(0)}
                  </span>
                ),
              },
              {
                title: "结果", dataIndex: "passed", key: "passed", width: 80,
                render: (passed: boolean) => passed
                  ? <Tag icon={<CheckCircleOutlined />} color="success">通过</Tag>
                  : <Tag icon={<CloseCircleOutlined />} color="error">未通过</Tag>,
              },
              { title: "原因", dataIndex: "reason", key: "reason", ellipsis: true },
            ]}
            rowKey="id"
            size="small"
            pagination={false}
            locale={{ emptyText: "暂无评测分数" }}
          />
        ) : (
          <Empty
            description="暂无评测分数。启动多维评测后，每个维度得分会显示在这里。"
            image={Empty.PRESENTED_IMAGE_SIMPLE}
          />
        )}
      </Card>

      {/* Tabs with Agent Timeline as default */}
      <Card>
        <Tabs activeKey={activeTab} onChange={setActiveTab} items={tabItems} />
      </Card>
    </Space>
  );
}
