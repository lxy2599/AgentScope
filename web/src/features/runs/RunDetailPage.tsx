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
  Button,
  App,
  Modal,
  Select,
} from "antd";
import {
  CheckCircleOutlined,
  CloseCircleOutlined,
  LineChartOutlined,
  BulbOutlined,
  RobotOutlined,
  ToolOutlined,
  DatabaseOutlined,
  ThunderboltOutlined,
  PlusOutlined,
} from "@ant-design/icons";
import {
  useAnnotationQueues,
  useCreateAnnotationItemFromRun,
  useEvalScoresByRun,
  useProjectRealtime,
  useRunDetail,
} from "../../api/hooks";
import { SpanTree } from "./SpanTree";
import { EventTable } from "./EventTable";
import { AgentTimeline, buildTimeline } from "./AgentTimeline";
import { TraceDiagnostics as TraceDiagnosticsComponent, computeDiagnostics } from "./TraceDiagnostics";
import type { TraceSpan } from "../../api/types";
import { RealtimeStatusIndicator } from "../../components/ui/RealtimeStatusIndicator";

const { Title, Text } = Typography;

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

// ---- LLM calls table columns ----
function fmtDuration(ms: number | null | undefined): string | null {
  if (ms == null) return null;
  if (ms < 1) return "<1ms";
  return `${ms}ms`;
}

const durationColumn = {
  title: "延迟", dataIndex: "duration_ms", key: "duration_ms",
  render: (v: number | null) => fmtDuration(v) ?? "—",
};

const llmColumns = [
  { title: "Provider", dataIndex: "provider", key: "provider" },
  { title: "模型", dataIndex: "model", key: "model" },
  { title: "输入摘要", dataIndex: "input_summary", key: "input_summary", ellipsis: true },
  { title: "输出摘要", dataIndex: "output_summary", key: "output_summary", ellipsis: true },
  { title: "输入 Tokens", dataIndex: "input_tokens", key: "input_tokens", render: (v: number | null) => v ?? "—" },
  { title: "输出 Tokens", dataIndex: "output_tokens", key: "output_tokens", render: (v: number | null) => v ?? "—" },
  { title: "总 Tokens", dataIndex: "total_tokens", key: "total_tokens", render: (v: number | null) => v ?? "—" },
  { title: "成本", dataIndex: "cost", key: "cost", render: (v: number | null) => v != null ? `$${v.toFixed(6)}` : "—" },
  { title: "延迟", ...durationColumn },
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
  { title: "延迟", ...durationColumn },
];

// ---- Retrieval calls table columns ----
const retrievalColumns = [
  { title: "知识库", dataIndex: "knowledge_base", key: "knowledge_base" },
  { title: "查询", dataIndex: "query_text", key: "query_text", ellipsis: true },
  { title: "请求 TopK", dataIndex: "top_k", key: "top_k", render: (v: number | null) => v ?? "—" },
  { title: "返回数量", dataIndex: "returned_count", key: "returned_count", render: (v: number | null) => v ?? "—" },
  { title: "文档 ID", dataIndex: "document_ids", key: "document_ids", render: (v: string[] | undefined) => v?.join(", ") || "—" },
  { title: "分数", dataIndex: "scores", key: "scores", render: (v: number[] | undefined) => v?.map(s => s.toFixed(3)).join(", ") || "—" },
  { title: "延迟", ...durationColumn },
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
  { title: "延迟", ...durationColumn },
];

export function RunDetailPage() {
  const { runId } = useParams<{ runId: string }>();
  const { message } = App.useApp();
  const { data, isLoading, error } = useRunDetail(runId ?? "");
  const projectId = (data as { run?: { project_id?: string } } | undefined)?.run?.project_id ?? "";
  const realtimeStatus = useProjectRealtime(projectId);
  const { data: evalScores, isLoading: evalLoading } = useEvalScoresByRun(projectId, runId ?? "");
  const [activeTab, setActiveTab] = useState("timeline");

  // Annotation from run
  const { data: queues } = useAnnotationQueues(projectId);
  const createItemFromRun = useCreateAnnotationItemFromRun(projectId);
  const [annotModalOpen, setAnnotModalOpen] = useState(false);
  const [annotQueueId, setAnnotQueueId] = useState<string | undefined>(undefined);

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
  const timelineItems = buildTimeline(thoughts, final_answers, llm_calls, tool_calls, retrieval_calls, skill_calls, spans);

  // Compute diagnostics
  const diagnostics = computeDiagnostics({
    llm_calls: llm_calls ?? [],
    tool_calls: tool_calls ?? [],
    retrieval_calls: retrieval_calls ?? [],
    skill_calls: skill_calls ?? [],
    thoughts: thoughts ?? [],
    final_answers: final_answers ?? [],
    spans: spans as TraceSpan[],
    run: {
      id: run.id,
      trace_id: run.trace_id,
      latency_ms: run.latency_ms,
      status: run.status,
      error: run.error,
    },
  });

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
          <Space>
            <RealtimeStatusIndicator status={realtimeStatus} />
            {statusTag(run.status)}
            <Button
              type="primary"
              size="small"
              icon={<PlusOutlined />}
              onClick={() => setAnnotModalOpen(true)}
            >
              加入标注队列
            </Button>
          </Space>
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

      {/* Trace Diagnostics Section */}
      {diagnostics.length > 0 && (
        <Card size="small" title={<span>诊断摘要</span>}>
          <TraceDiagnosticsComponent diagnostics={diagnostics} />
        </Card>
      )}

      {/* Eval Scores Section */}
      <Card size="small" title={<Space><LineChartOutlined /><span>多维评测分数</span></Space>}>
        {evalLoading ? (
          <div style={{ textAlign: "center", padding: 16 }}>
            <Spin size="small" />
          </div>
        ) : evalScores && evalScores.length > 0 ? (
          <Table
            dataSource={evalScores}
            columns={[
              {
                title: "指标类型", dataIndex: "metric_kind", key: "metric_kind", width: 140,
                render: (kind: string) => (
                  <Tag color={metricKindColor(kind)}>{metricKindLabel(kind)}</Tag>
                ),
              },
              {
                title: "指标名称", dataIndex: "metric_name", key: "metric_name", width: 160,
              },
              {
                title: "分数 (0-100)", dataIndex: "score", key: "score", width: 120,
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
              { title: "评分原因", dataIndex: "reason", key: "reason", ellipsis: true },
            ]}
            rowKey="id"
            size="small"
            pagination={false}
            locale={{ emptyText: "暂无评测分数" }}
          />
        ) : (
          <Empty
            description="暂无多维评测分数。启动多维评测并设置 evaluator 后，每个维度的得分会显示在这里。注意：未配置 evaluator 时不会产生分数，显示 0 分不表示评测通过。"
            image={Empty.PRESENTED_IMAGE_SIMPLE}
          />
        )}
      </Card>

      {/* Tabs with Agent Timeline as default */}
      <Card>
        <Tabs activeKey={activeTab} onChange={setActiveTab} items={tabItems} />
      </Card>

      {/* Add to Annotation Queue Modal */}
      <Modal
        title="加入标注队列"
        open={annotModalOpen}
        onOk={async () => {
          if (!runId) return;
          try {
            await createItemFromRun.mutateAsync({ runId, queueId: annotQueueId });
            message.success("已加入标注队列");
            setAnnotModalOpen(false);
            setAnnotQueueId(undefined);
          } catch {
            message.error("加入标注队列失败");
          }
        }}
        onCancel={() => {
          setAnnotModalOpen(false);
          setAnnotQueueId(undefined);
        }}
        confirmLoading={createItemFromRun.isPending}
        okText="加入"
        cancelText="取消"
      >
        <Space direction="vertical" style={{ width: "100%" }}>
          <div>
            <Text>选择目标队列（可选，默认使用"线上评测待标注"）：</Text>
            {!queues || queues.length === 0 ? (
              <Text type="secondary">暂无可用的标注队列</Text>
            ) : (
              <Select
                style={{ width: "100%", marginTop: 8 }}
                placeholder="选择队列（可选）"
                value={annotQueueId}
                onChange={setAnnotQueueId}
                allowClear
                options={(queues ?? []).map((q) => ({
                  value: q.id,
                  label: q.name,
                }))}
              />
            )}
          </div>
          {!run.trace_id && (
            <Text type="warning">
              该运行缺少 trace_id，加入后只能基于 run 追溯。
            </Text>
          )}
        </Space>
      </Modal>
    </Space>
  );
}
