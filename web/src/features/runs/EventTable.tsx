import type { TraceSpanEvent } from "../../api/types";

interface Props {
  events: TraceSpanEvent[];
}

export function EventTable({ events }: Props) {
  return (
    <div className="overflow-hidden rounded-lg border border-slate-200">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-slate-100 bg-slate-50 text-left text-xs font-medium uppercase text-slate-500">
            <th className="px-3 py-2">Name</th>
            <th className="px-3 py-2">Span ID</th>
            <th className="px-3 py-2">Time</th>
            <th className="px-3 py-2">Truncated</th>
          </tr>
        </thead>
        <tbody>
          {events.map((ev, i) => (
            <tr
              key={`${ev.span_id}-${i}`}
              className="border-b border-slate-50"
            >
              <td className="px-3 py-2 font-medium text-slate-800">
                {ev.event_name}
              </td>
              <td className="max-w-[100px] truncate px-3 py-2 font-mono text-xs text-slate-400">
                {ev.span_id}
              </td>
              <td className="px-3 py-2 text-slate-400">
                {new Date(ev.event_time).toLocaleString()}
              </td>
              <td className="px-3 py-2 text-slate-400">
                {ev.truncated ? "Yes" : "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
