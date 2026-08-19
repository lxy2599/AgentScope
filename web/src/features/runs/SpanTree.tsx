import {
  CaretDownOutlined,
  CaretRightOutlined,
  CodeOutlined,
  MinusOutlined,
} from "@ant-design/icons";
import { Button, Descriptions, Empty, Tag, Tooltip, Typography } from "antd";
import { useState } from "react";
import type { TraceSpan } from "../../api/types";
import { TechnicalId } from "../../components/ui/TechnicalId";

const { Text } = Typography;

interface Props {
  spans: TraceSpan[];
}

export interface TreeNode {
  span: TraceSpan;
  children: TreeNode[];
}

export interface SpanPreview {
  input?: { label: string; value: string };
  output?: { label: string; value: string };
  fields: Array<{ label: string; value: string }>;
}

const KIND_THEME: Record<string, { accent: string; soft: string; label: string }> = {
  LLM: { accent: "#7758d8", soft: "#f4f0ff", label: "LLM" },
  TOOL: { accent: "#0b9f9a", soft: "#e8fbfa", label: "TOOL" },
  FINAL_ANSWER: { accent: "#1677ff", soft: "#edf6ff", label: "ANSWER" },
  THOUGHT: { accent: "#d46b08", soft: "#fff7e6", label: "THOUGHT" },
  RETRIEVER: { accent: "#3f8600", soft: "#f6ffed", label: "RETRIEVER" },
  SKILL: { accent: "#ad6800", soft: "#fffbe6", label: "SKILL" },
  MEMORY: { accent: "#c41d7f", soft: "#fff0f6", label: "MEMORY" },
  GUARDRAIL: { accent: "#cf1322", soft: "#fff1f0", label: "GUARDRAIL" },
  INTERNAL: { accent: "#52718f", soft: "#f3f7fb", label: "INTERNAL" },
};

const statusColors: Record<string, string> = {
  OK: "success",
  ERROR: "error",
  UNSET: "default",
};

/** Parse the original OTLP attributes. Unknown keys remain intentionally visible. */
export function parseSpanAttributes(attributes?: string): unknown {
  if (!attributes) return {};
  try {
    return JSON.parse(attributes);
  } catch {
    return attributes;
  }
}

function attributeRecord(attributes?: string): Record<string, unknown> {
  const parsed = parseSpanAttributes(attributes);
  if (typeof parsed === "object" && parsed !== null && !Array.isArray(parsed)) {
    return parsed as Record<string, unknown>;
  }
  return {};
}

function attributeText(value: unknown): string {
  if (value == null) return "";
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function firstAttribute(attrs: Record<string, unknown>, keys: string[]): string {
  for (const key of keys) {
    const value = attributeText(attrs[key]);
    if (value) return value;
  }
  return "";
}

function compact(value: string, limit = 160): string {
  return value.length > limit ? `${value.slice(0, limit)}…` : value;
}

function looksLikeJSON(value: string): boolean {
  const trimmed = value.trim();
  return (trimmed.startsWith("{") && trimmed.endsWith("}")) ||
    (trimmed.startsWith("[") && trimmed.endsWith("]")) ||
    (trimmed.startsWith('"') && trimmed.endsWith('"'));
}

/**
 * Tool results can arrive as JSON embedded inside a JSON string. Parse twice
 * where appropriate so the tree shows readable objects rather than escaped
 * backslashes and quote sequences.
 */
export function formatSpanPayload(value: string): string {
  let parsed: unknown;
  try {
    parsed = JSON.parse(value);
  } catch {
    return value;
  }

  if (typeof parsed === "string" && looksLikeJSON(parsed)) {
    try {
      parsed = JSON.parse(parsed);
    } catch {
      // The first parse was still useful: remove the outer JSON quotes.
    }
  }

  if (typeof parsed === "string") return parsed;
  return JSON.stringify(parsed, null, 2);
}

export function formatSpanAttributes(attributes?: string): string {
  const parsed = parseSpanAttributes(attributes);
  return typeof parsed === "string" ? formatSpanPayload(parsed) : JSON.stringify(parsed, null, 2);
}

/** Build the detailed content rendered directly inside every tree node. */
export function getSpanPreview(span: TraceSpan): SpanPreview {
  const attrs = attributeRecord(span.attributes);
  const semanticKind = String(attrs["agentscope.span.kind"] ?? span.kind ?? "").toUpperCase();
  const input = firstAttribute(attrs, [
    "llm.input_summary",
    "tool.arguments_summary",
    "answer.input",
    "thought.summary",
    "input_summary",
  ]);
  const output = firstAttribute(attrs, [
    "llm.output_summary",
    "tool.result_summary",
    "answer.output",
    "thought.content",
    "output_summary",
  ]);
  const fields: Array<{ label: string; value: string }> = [];
  const add = (label: string, value: unknown) => {
    const text = attributeText(value);
    if (text) fields.push({ label, value: compact(text) });
  };

  if (semanticKind === "LLM") {
    add("Provider", attrs["llm.provider"]);
    add("Model", attrs["llm.model"]);
    add("输入", attrs["llm.input_tokens"] != null ? `${attrs["llm.input_tokens"]} tokens` : "");
    add("输出", attrs["llm.output_tokens"] != null ? `${attrs["llm.output_tokens"]} tokens` : "");
    add("缓存读取", attrs["llm.cache_read_tokens"] != null ? `${attrs["llm.cache_read_tokens"]} tokens` : "");
    add("TTFT", attrs["llm.ttft_ms"] != null ? `${attrs["llm.ttft_ms"]}ms` : "");
  } else if (semanticKind === "TOOL") {
    add("工具", attrs["tool.name"]);
    add("类型", attrs["tool.type"]);
    add("成功", attrs["tool.success"]);
    add("重试", attrs["tool.retry_count"]);
    add("模式", attrs["hermes.tool.execution_mode"]);
    add("失败分类", attrs["hermes.tool.failure_category"]);
  } else {
    for (const [key, value] of Object.entries(attrs)) {
      if (
        key === "agentscope.span.kind" ||
        key.endsWith("input_summary") ||
        key.endsWith("output_summary") ||
        key.endsWith("arguments_summary") ||
        key.endsWith("result_summary") ||
        key === "answer.output" ||
        key === "thought.content"
      ) continue;
      add(key, value);
      if (fields.length >= 6) break;
    }
  }

  return {
    input: input ? { label: semanticKind === "TOOL" ? "调用参数" : "输入内容", value: input } : undefined,
    output: output ? { label: semanticKind === "TOOL" ? "工具结果" : "输出内容", value: output } : undefined,
    fields,
  };
}

export function buildSpanTree(spans: TraceSpan[]): TreeNode[] {
  const map = new Map<string, TreeNode>();
  const roots: TreeNode[] = [];
  for (const span of spans) map.set(span.span_id, { span, children: [] });
  for (const span of spans) {
    const node = map.get(span.span_id)!;
    if (span.parent_span_id && map.has(span.parent_span_id)) map.get(span.parent_span_id)!.children.push(node);
    else roots.push(node);
  }
  const sort = (nodes: TreeNode[]) => {
    nodes.sort((a, b) => (b.span.duration_ms ?? 0) - (a.span.duration_ms ?? 0));
    for (const node of nodes) sort(node.children);
  };
  sort(roots);
  return roots;
}

function themeFor(span: TraceSpan) {
  const attrs = attributeRecord(span.attributes);
  const kind = String(attrs["agentscope.span.kind"] ?? span.kind ?? "INTERNAL").toUpperCase();
  return KIND_THEME[kind] ?? KIND_THEME.INTERNAL;
}

function formatTimestamp(value?: string): string {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString("zh-CN");
}

function PayloadPanel({ label, value, accent, soft }: { label: string; value: string; accent: string; soft: string }) {
  const [full, setFull] = useState(false);
  const formatted = formatSpanPayload(value);
  const parsedAsJSON = formatted !== value || looksLikeJSON(value);
  const needsToggle = formatted.length > 1300;

  return (
    <details open style={{ marginTop: 10, border: `1px solid ${accent}2e`, borderRadius: 8, overflow: "hidden", background: "#fff" }}>
      <summary style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, padding: "7px 10px", background: soft, cursor: "pointer", listStyle: "none" }}>
        <SpaceLabel accent={accent} label={label} />
        <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
          {parsedAsJSON && <Tag color="blue" style={tagStyle}><CodeOutlined /> JSON</Tag>}
          <Text type="secondary" style={{ fontSize: 11 }}>{formatted.length.toLocaleString("zh-CN")} chars · 点击展开/收起</Text>
        </div>
      </summary>
      <div>
        <pre style={{ ...codeStyle, maxHeight: full ? 640 : 190 }}>{formatted}</pre>
        {needsToggle && (
          <div style={{ display: "flex", justifyContent: "flex-end", padding: "6px 10px", borderTop: "1px solid #edf2f7" }}>
            <Button type="link" size="small" style={{ padding: 0, height: "auto", fontSize: 12 }} onClick={() => setFull(!full)}>
              {full ? "收起完整内容" : "展开完整内容"}
            </Button>
          </div>
        )}
      </div>
    </details>
  );
}

function SpaceLabel({ accent, label }: { accent: string; label: string }) {
  return (
    <Text strong style={{ fontSize: 12, color: accent }}>
      {label}
    </Text>
  );
}

function SpanMetadata({ span }: { span: TraceSpan }) {
  const attrs = attributeRecord(span.attributes);
  const entries = Object.entries(attrs);
  return (
    <details style={{ marginTop: 12 }}>
      <summary style={summaryStyle}>查看全部 Attributes 与技术字段 · {entries.length} 项</summary>
      <div style={{ marginTop: 9 }}>
        <Descriptions
          bordered
          size="small"
          column={1}
          items={[
            { key: "status-message", label: "状态消息", children: span.status_message ? <pre style={codeStyle}>{span.status_message}</pre> : "—" },
            { key: "start", label: "开始时间", children: formatTimestamp(span.start_time) },
            { key: "end", label: "结束时间", children: formatTimestamp(span.end_time) },
            { key: "truncated", label: "内容截断", children: span.truncated ? <Tag color="warning">是</Tag> : "否" },
            { key: "span-id", label: "Span ID", children: <TechnicalId value={span.span_id} full /> },
            { key: "parent-span-id", label: "父 Span ID", children: span.parent_span_id ? <TechnicalId value={span.parent_span_id} full /> : "—" },
            { key: "trace-id", label: "Trace ID", children: <TechnicalId value={span.trace_id} full /> },
            { key: "run-id", label: "Run ID", children: <TechnicalId value={span.run_id} full /> },
            { key: "project-id", label: "Project ID", children: <TechnicalId value={span.project_id} full /> },
            { key: "attributes", label: "全部 Attributes", children: <pre data-testid="span-attributes" style={codeStyle}>{formatSpanAttributes(span.attributes)}</pre> },
          ]}
        />
      </div>
    </details>
  );
}

function SpanNode({ node, depth }: { node: TreeNode; depth: number }) {
  const [childrenOpen, setChildrenOpen] = useState(true);
  const { span } = node;
  const preview = getSpanPreview(span);
  const theme = themeFor(span);
  const hasChildren = node.children.length > 0;

  return (
    <div style={{ marginLeft: depth * 24, position: "relative" }}>
      {depth > 0 && <div style={{ position: "absolute", left: -13, top: 0, bottom: 0, width: 1, background: "#d6e4f0" }} />}
      <article style={{ margin: "10px 0", border: "1px solid #dce8f2", borderLeft: `4px solid ${theme.accent}`, borderRadius: 10, background: "#fff", boxShadow: "0 3px 10px rgba(33, 74, 112, 0.045)", overflow: "hidden" }}>
        <header style={{ display: "flex", alignItems: "center", gap: 9, padding: "9px 12px", background: `linear-gradient(90deg, ${theme.soft}, #fff 68%)`, borderBottom: "1px solid #edf2f7" }}>
          <span
            aria-label={hasChildren ? (childrenOpen ? "折叠子 Span" : "展开子 Span") : undefined}
            onClick={() => hasChildren && setChildrenOpen(!childrenOpen)}
            style={{ width: 16, cursor: hasChildren ? "pointer" : "default", color: theme.accent }}
          >
            {hasChildren ? (childrenOpen ? <CaretDownOutlined /> : <CaretRightOutlined />) : <MinusOutlined style={{ color: "#aab9c6" }} />}
          </span>
          <Tag style={{ ...tagStyle, color: theme.accent, borderColor: `${theme.accent}55`, background: theme.soft }}>{theme.label}</Tag>
          <Text strong style={{ fontSize: 13, color: "#18324b", wordBreak: "break-all" }}>{span.name}</Text>
          <div style={{ marginLeft: "auto", display: "flex", alignItems: "center", gap: 7, flexShrink: 0 }}>
            {span.duration_ms != null && <Text type="secondary" style={{ fontSize: 12 }}>{span.duration_ms}ms</Text>}
            {span.status_code && <Tag color={statusColors[span.status_code] || "default"} style={tagStyle}>{span.status_code}</Tag>}
            {span.truncated && <Tag color="warning" style={tagStyle}>已截断</Tag>}
          </div>
        </header>
        <div style={{ padding: "10px 12px 12px" }}>
          {preview.fields.length > 0 && (
            <div style={{ display: "flex", flexWrap: "wrap", gap: 7 }}>
              {preview.fields.map((field) => (
                <Tooltip title={field.value} key={field.label}>
                  <span style={{ display: "inline-flex", gap: 5, maxWidth: 310, padding: "4px 7px", borderRadius: 6, background: "#f6faff", border: "1px solid #e1edf7", fontSize: 12 }}>
                    <Text type="secondary" style={{ fontSize: 12, whiteSpace: "nowrap" }}>{field.label}</Text>
                    <Text style={{ fontSize: 12, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{field.value}</Text>
                  </span>
                </Tooltip>
              ))}
            </div>
          )}
          {preview.input && <PayloadPanel {...preview.input} accent={theme.accent} soft={theme.soft} />}
          {preview.output && <PayloadPanel {...preview.output} accent={theme.accent} soft={theme.soft} />}
          <SpanMetadata span={span} />
        </div>
      </article>
      {childrenOpen && hasChildren && node.children.map((child) => <SpanNode key={child.span.span_id} node={child} depth={depth + 1} />)}
    </div>
  );
}

const tagStyle: React.CSSProperties = { margin: 0, fontSize: 11, lineHeight: "18px", borderRadius: 5 };
const summaryStyle: React.CSSProperties = { cursor: "pointer", color: "#1677ff", fontSize: 12, fontWeight: 600 };
const codeStyle: React.CSSProperties = {
  margin: 0,
  padding: "10px 12px",
  overflow: "auto",
  whiteSpace: "pre-wrap",
  wordBreak: "break-word",
  fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace',
  fontSize: 12,
  lineHeight: 1.62,
  color: "#243b53",
  background: "#f8fbfe",
};

/** Detailed, Span-first trace explorer. */
export function SpanTree({ spans }: Props) {
  const tree = buildSpanTree(spans);
  if (spans.length === 0) return <Empty description="该 Run 暂无原始 Span" image={Empty.PRESENTED_IMAGE_SIMPLE} />;

  return (
    <div style={{ padding: "2px 0 8px" }}>
      <div style={{ marginBottom: 12, padding: "10px 12px", borderRadius: 8, background: "#f3f8fc", border: "1px solid #dbeaf5" }}>
        <Text strong style={{ color: "#244765" }}>Span 诊断树</Text>
        <Text type="secondary" style={{ display: "block", marginTop: 2, fontSize: 12 }}>
          每个节点直接显示结构化输入、输出和关键指标；JSON 已解析格式化。长内容可展开/收起，完整 Attributes 也保留在节点内。
        </Text>
      </div>
      {tree.map((node) => <SpanNode key={node.span.span_id} node={node} depth={0} />)}
    </div>
  );
}
