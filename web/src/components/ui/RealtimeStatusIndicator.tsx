import { Badge, Tooltip } from "antd";
import type { RealtimeStatus } from "../../api/realtime";

const STATUS_VIEW: Record<
  RealtimeStatus,
  { label: string; badge: "success" | "processing" | "warning" | "default"; tip: string }
> = {
  connecting: {
    label: "连接中",
    badge: "processing",
    tip: "正在建立项目实时事件连接",
  },
  live: {
    label: "实时",
    badge: "success",
    tip: "状态变化将通过 SSE 实时推送",
  },
  reconnecting: {
    label: "重连中",
    badge: "warning",
    tip: "实时连接暂时中断，正在自动重连",
  },
  fallback: {
    label: "轮询降级",
    badge: "warning",
    tip: "实时连接不可用，页面已切换为定时轮询",
  },
  closed: {
    label: "已关闭",
    badge: "default",
    tip: "当前页面未订阅实时事件",
  },
};

export function RealtimeStatusIndicator({
  status,
}: {
  status: RealtimeStatus;
}) {
  const view = STATUS_VIEW[status];
  return (
    <Tooltip title={view.tip}>
      <span
        aria-label={`实时状态：${view.label}`}
        style={{
          display: "inline-flex",
          alignItems: "center",
          minWidth: 72,
          height: 24,
          padding: "0 8px",
          border: "1px solid #d9d9d9",
          borderRadius: 4,
          background: "#fff",
        }}
      >
        <Badge status={view.badge} text={view.label} />
      </span>
    </Tooltip>
  );
}
