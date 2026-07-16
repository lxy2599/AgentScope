import { Tag, Typography, Space } from "antd";
import { CaretRightOutlined, CaretDownOutlined, MinusOutlined } from "@ant-design/icons";
import { useState } from "react";
import type { TraceSpan } from "../../api/types";

const { Text } = Typography;

interface Props {
  spans: TraceSpan[];
}

export interface TreeNode {
  span: TraceSpan;
  children: TreeNode[];
}

export function buildSpanTree(spans: TraceSpan[]): TreeNode[] {
  const map = new Map<string, TreeNode>();
  const roots: TreeNode[] = [];

  for (const span of spans) {
    map.set(span.span_id, { span, children: [] });
  }

  for (const span of spans) {
    const node = map.get(span.span_id)!;
    if (span.parent_span_id && map.has(span.parent_span_id)) {
      map.get(span.parent_span_id)!.children.push(node);
    } else {
      roots.push(node);
    }
  }

  // Sort by duration descending (longest first)
  const sort = (nodes: TreeNode[]) => {
    nodes.sort(
      (a, b) => (b.span.duration_ms ?? 0) - (a.span.duration_ms ?? 0),
    );
    for (const n of nodes) sort(n.children);
  };
  sort(roots);

  return roots;
}

const statusColors: Record<string, string> = {
  OK: "success",
  ERROR: "error",
  UNSET: "default",
};

function SpanNode({ node, depth }: { node: TreeNode; depth: number }) {
  const [expanded, setExpanded] = useState(true);
  const hasChildren = node.children.length > 0;
  const { span } = node;

  return (
    <div>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          padding: "4px 8px",
          cursor: "pointer",
          borderRadius: 4,
          marginLeft: depth * 16,
        }}
        className="hover:bg-gray-50"
        onClick={() => setExpanded(!expanded)}
      >
        {hasChildren ? (
          expanded ? (
            <CaretDownOutlined style={{ fontSize: 12, color: "#999" }} />
          ) : (
            <CaretRightOutlined style={{ fontSize: 12, color: "#999" }} />
          )
        ) : (
          <MinusOutlined style={{ fontSize: 12, color: "#ddd" }} />
        )}
        <Text strong style={{ fontSize: 13 }}>{span.name}</Text>
        {span.kind && (
          <Text type="secondary" style={{ fontSize: 12 }}>
            {span.kind}
          </Text>
        )}
        <div style={{ marginLeft: "auto", display: "flex", alignItems: "center", gap: 8 }}>
          {span.duration_ms != null && (
            <Text type="secondary" style={{ fontSize: 12 }}>
              {span.duration_ms}ms
            </Text>
          )}
          {span.status_code && (
            <Tag color={statusColors[span.status_code] || "default"}>
              {span.status_code}
            </Tag>
          )}
          {span.truncated && (
            <Tag color="warning" style={{ fontSize: 11 }}>
              已截断
            </Tag>
          )}
        </div>
      </div>

      {expanded && hasChildren && (
        <div>
          {node.children.map((child) => (
            <SpanNode key={child.span.span_id} node={child} depth={depth + 1} />
          ))}
        </div>
      )}
    </div>
  );
}

export function SpanTree({ spans }: Props) {
  const tree = buildSpanTree(spans);
  return (
    <div>
      {tree.map((node) => (
        <SpanNode key={node.span.span_id} node={node} depth={0} />
      ))}
    </div>
  );
}
