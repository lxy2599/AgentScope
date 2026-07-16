import { ChevronRight, ChevronDown } from "lucide-react";
import { useState } from "react";
import type { TraceSpan } from "../../api/types";
import { Badge, statusBadge } from "../../components/ui/Badge";

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

function SpanNode({ node, depth }: { node: TreeNode; depth: number }) {
  const [expanded, setExpanded] = useState(true);
  const hasChildren = node.children.length > 0;
  const { span } = node;

  return (
    <div>
      <div
        className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-slate-50"
        style={{ paddingLeft: `${depth * 16 + 8}px` }}
        onClick={() => setExpanded(!expanded)}
      >
        {hasChildren ? (
          expanded ? (
            <ChevronDown className="h-3.5 w-3.5 shrink-0 text-slate-400" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5 shrink-0 text-slate-400" />
          )
        ) : (
          <span className="w-3.5" />
        )}
        <span className="font-medium text-slate-800">{span.name}</span>
        <span className="text-xs text-slate-400">{span.kind}</span>
        {span.duration_ms != null && (
          <span className="ml-auto text-xs text-slate-400">
            {span.duration_ms}ms
          </span>
        )}
        {span.status_code && (
          <Badge variant={statusBadge(span.status_code)}>
            {span.status_code}
          </Badge>
        )}
        {span.truncated && (
          <span className="rounded bg-amber-100 px-1 text-xs text-amber-700">
            truncated
          </span>
        )}
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
    <div className="divide-y divide-slate-100">
      {tree.map((node) => (
        <SpanNode key={node.span.span_id} node={node} depth={0} />
      ))}
    </div>
  );
}
