import { Alert, Tag, Space, Typography } from "antd";
import {
  WarningOutlined,
  CloseCircleOutlined,
  InfoCircleOutlined,
  BugOutlined,
  FieldTimeOutlined,
} from "@ant-design/icons";
import type {
  LLMCallRecord,
  ToolCallRecord,
  RetrievalCallRecord,
  SkillCallRecord,
  ThoughtRecord,
  FinalAnswerRecord,
  TraceSpan,
} from "../../api/types";

const { Text, Paragraph } = Typography;

// ---- Public types ----

export interface TraceDiagnostic {
  type: "error" | "warning" | "info";
  message: string;
  detail?: string;
  span_id?: string;
  tool_name?: string;
}

// ---- Slow threshold ----
const SLOW_LLM_THRESHOLD_MS = 10000;
const SLOW_TOOL_THRESHOLD_MS = 5000;

// ---- Compute diagnostics from run detail data ----

export interface DiagnosticInput {
  llm_calls: LLMCallRecord[];
  tool_calls: ToolCallRecord[];
  retrieval_calls: RetrievalCallRecord[];
  skill_calls: SkillCallRecord[];
  thoughts: ThoughtRecord[];
  final_answers: FinalAnswerRecord[];
  spans: TraceSpan[];
  run: {
    id: string;
    trace_id: string;
    latency_ms?: number;
    status?: string;
    error?: string;
  };
}

export function computeDiagnostics(input: DiagnosticInput): TraceDiagnostic[] {
  const diags: TraceDiagnostic[] = [];
  const {
    llm_calls,
    tool_calls,
    retrieval_calls,
    skill_calls,
    thoughts,
    final_answers,
    spans,
    run,
  } = input;

  // 1. Missing final answer
  if (thoughts.length > 0 || llm_calls.length > 0 || tool_calls.length > 0) {
    if (final_answers.length === 0) {
      diags.push({
        type: "error",
        message: "缺少最终答案（final_answer）",
        detail: "Run 包含思考、LLM 调用或工具调用，但未记录最终答案。可能 Agent 未完成回答或因错误中断。",
      });
    }
  }

  // 2. Missing trace_id
  if (!run.trace_id) {
    diags.push({
      type: "error",
      message: "缺少 trace_id",
      detail: "该 Run 没有关联的 Trace。如果 run 已完成，需要检查 OTLP 上报链路。",
    });
  }

  // 3. Zero / invalid run_id
  if (!run.id || run.id === "00000000-0000-0000-0000-000000000000") {
    diags.push({
      type: "error",
      message: "run_id 为空或为全零 UUID",
      detail: "Run ID 异常，无法正确关联到 Trace。",
    });
  }

  // 4. Failed/error tool calls
  for (const t of tool_calls) {
    if (!t.success) {
      diags.push({
        type: "error",
        message: `工具调用失败：${t.tool_name}`,
        detail: t.error || "未知错误",
        span_id: t.span_id,
        tool_name: t.tool_name,
      });
    }
  }

  // 5. Slow LLM calls
  for (const l of llm_calls) {
    const dur = l.duration_ms ?? 0;
    if (dur > SLOW_LLM_THRESHOLD_MS && dur > 0) {
      diags.push({
        type: "warning",
        message: `慢 LLM 调用：${l.provider}/${l.model}（${dur}ms）`,
        detail: `输入 ${l.input_tokens ?? "?"} tokens, 输出 ${l.output_tokens ?? "?"} tokens`,
        span_id: l.span_id,
      });
    }
  }

  // 6. Slow tool calls
  for (const t of tool_calls) {
    const dur = t.duration_ms ?? 0;
    if (dur > SLOW_TOOL_THRESHOLD_MS && dur > 0) {
      diags.push({
        type: "warning",
        message: `慢工具调用：${t.tool_name}（${dur}ms）`,
        span_id: t.span_id,
        tool_name: t.tool_name,
      });
    }
  }

  // 7. Retry events
  for (const t of tool_calls) {
    if (t.retry_count > 0) {
      diags.push({
        type: "warning",
        message: `工具重试：${t.tool_name}（重试 ${t.retry_count} 次）`,
        span_id: t.span_id,
        tool_name: t.tool_name,
      });
    }
  }

  // 8. High cost
  let totalCost = 0;
  for (const l of llm_calls) {
    totalCost += l.cost ?? 0;
  }
  if (totalCost > 0.05) {
    diags.push({
      type: "warning",
      message: `高成本：$${totalCost.toFixed(4)}`,
      detail: "总成本超过阈值（$0.05）。请检查是否有多余调用或上下文过长。",
    });
  }

  // 9. High token usage
  let totalTokens = 0;
  for (const l of llm_calls) {
    totalTokens += l.total_tokens ?? 0;
  }
  if (totalTokens > 10000) {
    diags.push({
      type: "info",
      message: `高 Token 消耗：${totalTokens} tokens`,
      detail: "总 token 数超过 10000，可能上下文累积过重。",
    });
  }

  // 10. Duration 0 for LLM/tool
  const hasTimingInfo = llm_calls.some(l => l.duration_ms != null) ||
    tool_calls.some(t => t.duration_ms != null) ||
    retrieval_calls.some(r => r.duration_ms != null) ||
    skill_calls.some(s => s.duration_ms != null) ||
    thoughts.some(t => t.duration_ms != null) ||
    final_answers.some(f => f.duration_ms != null);
  if (hasTimingInfo) {
    const zeroDurLLM = llm_calls.filter(l => l.duration_ms === 0);
    const zeroDurTool = tool_calls.filter(t => t.duration_ms === 0);
    if (zeroDurLLM.length > 0) {
      diags.push({
        type: "warning",
        message: `${zeroDurLLM.length} 个 LLM 调用 duration 为 0`,
        detail: "可能 SDK 未正确记录 span 结束时间。",
        span_id: zeroDurLLM[0].span_id,
      });
    }
    if (zeroDurTool.length > 0) {
      diags.push({
        type: "warning",
        message: `${zeroDurTool.length} 个工具调用 duration 为 0`,
        detail: "可能 SDK 未正确记录 span 结束时间。",
        span_id: zeroDurTool[0].span_id,
      });
    }
  }

  // 11. Span vs semantic count mismatch
  const semanticCount = llm_calls.length + tool_calls.length + retrieval_calls.length + thoughts.length + final_answers.length;
  if (spans.length > 0 && semanticCount > 0) {
    if (spans.length > semanticCount * 2) {
      diags.push({
        type: "info",
        message: "Raw Span 数量远多于语义记录",
        detail: `Span ${spans.length} 个 vs 语义记录 ${semanticCount} 条，可能部分 Span 没有对应语义解析。`,
      });
    }
  }

  // 12. Guardrail blocked/error detection from spans
  for (const sp of spans) {
    const name = sp.name?.toLowerCase() || "";
    const attrs = parseSpanAttributes(sp.attributes);
    const isGuardrail = name.includes("guardrail") || name.includes("safety") || attrs?.type === "guardrail" || attrs?.type === "safety";
    if (isGuardrail) {
      const isBlocked = sp.status_code === "ERROR" || attrs?.blocked === "true" || attrs?.action === "block";
      if (isBlocked) {
        diags.push({
          type: "error",
          message: `安全拦截：${sp.name}`,
          detail: attrs?.reason || "Guardrail 触发了阻断",
          span_id: sp.span_id,
        });
      }
    }
  }

  // 13. Memory failure detection from spans
  for (const sp of spans) {
    const name = sp.name?.toLowerCase() || "";
    const attrs = parseSpanAttributes(sp.attributes);
    const isMemory = name.includes("memory") || name.includes("mem_") || attrs?.type === "memory";
    if (isMemory && sp.status_code === "ERROR") {
      diags.push({
        type: "error",
        message: `Memory 操作失败：${sp.name}`,
        detail: attrs?.error || sp.status_message || "未知错误",
        span_id: sp.span_id,
      });
    }
  }

  return diags;
}

function parseSpanAttributes(attrs?: string): Record<string, string> | null {
  if (!attrs) return null;
  try {
    const parsed = JSON.parse(attrs);
    if (typeof parsed === "object" && parsed !== null) {
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

// ---- TraceDiagnostics display component ----

const DIAGNOSTIC_ICONS: Record<string, React.ReactNode> = {
  error: <CloseCircleOutlined />,
  warning: <WarningOutlined />,
  info: <InfoCircleOutlined />,
};

const DIAGNOSTIC_TYPES: Record<string, { color: "error" | "warning" | "info" }> = {
  error: { color: "error" },
  warning: { color: "warning" },
  info: { color: "info" },
};

export function TraceDiagnostics({ diagnostics }: { diagnostics: TraceDiagnostic[] }) {
  if (diagnostics.length === 0) {
    return (
      <div style={{ padding: "8px 0" }}>
        <Alert
          type="success"
          message="无异常诊断项"
          description="所有检查项通过，未发现异常。"
          showIcon
          style={{ marginBottom: 8 }}
        />
      </div>
    );
  }

  return (
    <Space direction="vertical" size="small" style={{ width: "100%" }}>
      {diagnostics.map((d, idx) => (
        <Alert
          key={idx}
          type={DIAGNOSTIC_TYPES[d.type]?.color || "info"}
          message={
            <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
              <span>{d.message}</span>
              {d.span_id && (
                <Tag style={{ fontSize: 11, marginRight: 0 }}>
                  span: {d.span_id.substring(0, 12)}...
                </Tag>
              )}
              {d.tool_name && (
                <Tag color="blue" style={{ fontSize: 11, marginRight: 0 }}>
                  {d.tool_name}
                </Tag>
              )}
            </div>
          }
          description={d.detail}
          showIcon
          icon={DIAGNOSTIC_ICONS[d.type]}
          style={{ marginBottom: 0 }}
        />
      ))}
    </Space>
  );
}
