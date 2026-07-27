import { Badge } from "antd";

const STATUS_META: Record<string, { label: string; color: string }> = {
  pending: { label: "待开始", color: "default" },
  queued: { label: "排队中", color: "warning" },
  running: { label: "运行中", color: "processing" },
  thinking: { label: "思考中", color: "processing" },
  waiting_llm: { label: "等待 LLM", color: "warning" },
  waiting_tool: { label: "等待工具", color: "warning" },
  waiting_retrieval: { label: "等待检索", color: "processing" },
  retrying: { label: "重试中", color: "warning" },
  completed: { label: "已完成", color: "success" },
  succeeded: { label: "成功", color: "success" },
  failed: { label: "失败", color: "error" },
  error: { label: "错误", color: "error" },
  timeout: { label: "超时", color: "error" },
  cancelled: { label: "已取消", color: "default" },
  active: { label: "在线", color: "success" },
  inactive: { label: "离线", color: "default" },
};

interface StatusBadgeProps {
  status?: string | null;
  label?: string;
  className?: string;
}

export function statusLabel(status?: string | null) {
  if (!status) return "未采集";
  return STATUS_META[status.toLowerCase()]?.label ?? status;
}

/** Status always carries a text label; color is only secondary signal. */
export function StatusBadge({ status, label, className }: StatusBadgeProps) {
  const normalized = status?.toLowerCase() ?? "";
  const meta = STATUS_META[normalized];
  return (
    <span className={`status-badge ${className ?? ""}`}>
      <Badge status={meta?.color as "success" | "processing" | "error" | "warning" | "default" | undefined} />
      <span>{label ?? meta?.label ?? status ?? "未采集"}</span>
    </span>
  );
}
