import { Empty, Tag, Typography, Descriptions, Timeline } from "antd";
import {
  BulbOutlined,
  CheckSquareOutlined,
  RobotOutlined,
  ToolOutlined,
  DatabaseOutlined,
  ThunderboltOutlined,
  SafetyOutlined,
  WarningOutlined,
  BugOutlined,
  FieldTimeOutlined,
  CloseCircleOutlined,
} from "@ant-design/icons";
import type {
  ThoughtRecord,
  FinalAnswerRecord,
  LLMCallRecord,
  ToolCallRecord,
  RetrievalCallRecord,
  SkillCallRecord,
  TraceSpan,
} from "../../api/types";

const { Text, Paragraph } = Typography;

// ---- Public types ----

export interface TimelineItem {
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

// ---- Kind display config ----

export const AGENT_KIND_CONFIG: Record<string, { color: string; icon: React.ReactNode; label: string }> = {
  THOUGHT: { color: "#fa8c16", icon: <BulbOutlined />, label: "思考" },
  FINAL_ANSWER: { color: "#1890ff", icon: <CheckSquareOutlined />, label: "最终答案" },
  LLM: { color: "#722ed1", icon: <RobotOutlined />, label: "LLM" },
  TOOL: { color: "#13c2c2", icon: <ToolOutlined />, label: "工具" },
  RETRIEVER: { color: "#52c41a", icon: <DatabaseOutlined />, label: "检索" },
  SKILL: { color: "#faad14", icon: <ThunderboltOutlined />, label: "Skill" },
  MEMORY: { color: "#eb2f96", icon: <SafetyOutlined />, label: "Memory" },
  GUARDRAIL: { color: "#ff4d4f", icon: <WarningOutlined />, label: "安全" },
  RETRY: { color: "#fa541c", icon: <BugOutlined />, label: "重试" },
  ERROR: { color: "#ff4d4f", icon: <CloseCircleOutlined />, label: "错误" },
};

// ---- Duration formatter ----

function fmtDuration(ms: number | null | undefined): string {
  if (ms == null) return "未知";
  if (ms < 1) return "<1ms";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}min`;
}

// Parse span attributes JSON string into a record
function parseAttributes(attrs?: string): Record<string, string> | null {
  if (!attrs) return null;
  try {
    const parsed = JSON.parse(attrs);
    if (typeof parsed === "object" && parsed !== null) {
      // Extract known values as strings
      const result: Record<string, string> = {};
      for (const [k, v] of Object.entries(parsed)) {
        result[k] = String(v);
      }
      return result;
    }
  } catch {
    // Not valid JSON, ignore
  }
  return null;
}

// ---- Build unified timeline from semantic records and raw spans ----

export function buildTimeline(
  thoughts?: ThoughtRecord[],
  finalAnswers?: FinalAnswerRecord[],
  llmCalls?: LLMCallRecord[],
  toolCalls?: ToolCallRecord[],
  retrievalCalls?: RetrievalCallRecord[],
  skillCalls?: SkillCallRecord[],
  spans?: TraceSpan[],
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
      details: {
        stage: t.stage,
        confidence: t.confidence,
        content_captured: t.content_captured,
        span_id: t.span_id,
        trace_id: t.trace_id,
        run_id: t.run_id,
      },
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
      details: {
        structured_output: fa.structured_output,
        span_id: fa.span_id,
        trace_id: fa.trace_id,
        run_id: fa.run_id,
      },
    });
  }

  for (const l of llmCalls ?? []) {
    items.push({
      kind: "LLM",
      name: `${l.provider}/${l.model}`,
      summary: l.input_summary ?? "",
      content: l.output_summary,
      durationMs: l.duration_ms,
      status: l.status,
      error: l.error,
      startTime: l.start_time,
      details: {
        input_tokens: l.input_tokens,
        output_tokens: l.output_tokens,
        total_tokens: l.total_tokens,
        cache_read_tokens: l.cache_read_tokens,
        cache_write_tokens: l.cache_write_tokens,
        ttft_ms: l.ttft_ms,
        cost: l.cost,
        estimated_cost: l.estimated_cost,
        actual_cost: l.actual_cost,
        cost_source: l.cost_source,
        content_captured: l.content_captured,
        span_id: l.span_id,
        trace_id: l.trace_id,
        run_id: l.run_id,
      },
    });
  }

  for (const t of toolCalls ?? []) {
    items.push({
      kind: "TOOL",
      name: t.tool_name,
      summary: t.arguments_summary ?? "",
      content: t.result_summary,
      durationMs: t.duration_ms,
      status: t.success ? "OK" : "ERROR",
      error: t.error,
      startTime: t.start_time,
      details: {
        tool_type: t.tool_type,
        retry_count: t.retry_count,
        side_effect_risk: t.side_effect_risk,
        span_id: t.span_id,
        trace_id: t.trace_id,
        run_id: t.run_id,
      },
    });
  }

  for (const r of retrievalCalls ?? []) {
    items.push({
      kind: "RETRIEVER",
      name: r.knowledge_base,
      summary: r.query_text,
      durationMs: r.duration_ms,
      startTime: r.start_time,
      details: {
        top_k: r.top_k,
        returned_count: r.returned_count,
        document_ids: r.document_ids,
        scores: r.scores,
        span_id: r.span_id,
        trace_id: r.trace_id,
        run_id: r.run_id,
      },
    });
  }

  for (const s of skillCalls ?? []) {
    items.push({
      kind: "SKILL",
      name: s.skill_name,
      summary: s.input_summary ?? "",
      content: s.output_summary,
      durationMs: s.duration_ms,
      status: s.success ? "OK" : "ERROR",
      error: s.error,
      startTime: s.start_time,
      details: {
        skill_version: s.skill_version,
        span_id: s.span_id,
        trace_id: s.trace_id,
        run_id: s.run_id,
      },
    });
  }

  // Normalize memory/guardrail events from raw spans
  for (const sp of spans ?? []) {
    const name = sp.name?.toLowerCase() || "";
    const attrs = parseAttributes(sp.attributes);
    if (name.includes("memory") || name.includes("mem_") || attrs?.type === "memory") {
      items.push({
        kind: "MEMORY",
        name: sp.name,
        summary: attrs?.summary || attrs?.operation || name,
        durationMs: sp.duration_ms,
        startTime: sp.start_time,
        details: {
          span_id: sp.span_id,
          trace_id: sp.trace_id,
          run_id: sp.run_id,
          status_code: sp.status_code,
        },
      });
    }
    if (name.includes("guardrail") || name.includes("safety") || attrs?.type === "guardrail" || attrs?.type === "safety") {
      const isBlocked = sp.status_code === "ERROR" || attrs?.blocked === "true" || attrs?.action === "block";
      items.push({
        kind: "GUARDRAIL",
        name: sp.name,
        summary: attrs?.summary || attrs?.rule || name,
        durationMs: sp.duration_ms,
        status: isBlocked ? "ERROR" : "OK",
        error: isBlocked ? (attrs?.reason || "blocked") : undefined,
        startTime: sp.start_time,
        details: {
          span_id: sp.span_id,
          trace_id: sp.trace_id,
          run_id: sp.run_id,
          status_code: sp.status_code,
        },
      });
    }
  }

  // Sort by start_time ascending
  items.sort((a, b) => new Date(a.startTime).getTime() - new Date(b.startTime).getTime());
  return items;
}

// ---- AgentTimeline component ----

export function AgentTimeline({ items }: { items: TimelineItem[] }) {
  if (items.length === 0) {
    return <Empty description="暂无 Agent 时间线记录" image={Empty.PRESENTED_IMAGE_SIMPLE} />;
  }

  return (
    <Timeline
      items={items.map((item, idx) => {
        const cfg = AGENT_KIND_CONFIG[item.kind] || { color: "#999", icon: null, label: item.kind };
        const durationStr = fmtDuration(item.durationMs);

        return {
          key: idx,
          color: cfg.color,
          dot: cfg.icon,
          children: (
            <div style={{ marginBottom: 8 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 4, flexWrap: "wrap" }}>
                <Tag color={cfg.color} style={{ marginRight: 0, flexShrink: 0 }}>{cfg.label}</Tag>
                <Text strong style={{ fontSize: 13, wordBreak: "break-all" }}>{item.name}</Text>
                <Text type="secondary" style={{ fontSize: 12, flexShrink: 0 }}>
                  <FieldTimeOutlined style={{ marginRight: 2 }} />
                  {durationStr}
                </Text>
                {item.status && item.status === "ERROR" && (
                  <Tag color="error" style={{ flexShrink: 0 }}>失败</Tag>
                )}
                {item.details?.content_captured === false && (
                  <Tag style={{ flexShrink: 0, fontSize: 11 }}>内容未采集</Tag>
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
                    // Skip display-only keys shown elsewhere
                    if (k === "content_captured") return null;
                    if (Array.isArray(v) && v.length === 0) return null;
                    return (
                      <Descriptions.Item key={k} label={k} style={{ paddingBottom: 0 }}>
                        <Text style={{ fontSize: 12 }}>
                          {Array.isArray(v) ? v.join(", ") : String(v)}
                        </Text>
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
