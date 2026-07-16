import { useParams } from "react-router-dom";
import { AlertCircle, Info } from "lucide-react";
import { useRunDetail } from "../../api/hooks";
import { Badge, statusBadge } from "../../components/ui/Badge";
import { SpanTree } from "./SpanTree";
import { EventTable } from "./EventTable";

export function RunDetailPage() {
  const { runId } = useParams<{ runId: string }>();
  const { data, isLoading, error } = useRunDetail(runId ?? "");

  if (isLoading) {
    return <div className="text-sm text-slate-500">Loading run...</div>;
  }

  if (error) {
    return (
      <div className="flex items-center gap-2 text-sm text-red-600">
        <AlertCircle className="h-4 w-4" />
        {error instanceof Error ? error.message : "Failed to load run"}
      </div>
    );
  }

  if (!data) {
    return (
      <div className="flex items-center gap-2 text-sm text-slate-500">
        <Info className="h-4 w-4" />
        Run not found.
      </div>
    );
  }

  const { run, spans, events } = data;

  return (
    <div className="space-y-6">
      {/* Run summary */}
      <div className="rounded-xl border border-slate-200 bg-white p-6">
        <div className="flex items-center justify-between">
          <h1 className="text-2xl font-bold text-slate-900">{run.agent_name}</h1>
          <Badge variant={statusBadge(run.status)}>{run.status}</Badge>
        </div>
        <div className="mt-4 grid grid-cols-2 gap-4 text-sm">
          <div>
            <span className="text-slate-500">Source:</span> {run.source}
          </div>
          <div>
            <span className="text-slate-500">Version:</span>{" "}
            {run.agent_version || "—"}
          </div>
          <div>
            <span className="text-slate-500">Trace ID:</span>{" "}
            <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">
              {run.trace_id}
            </code>
          </div>
          <div>
            <span className="text-slate-500">Latency:</span>{" "}
            {run.latency_ms != null ? `${run.latency_ms}ms` : "—"}
          </div>
          {run.input_summary && (
            <div className="col-span-2">
              <span className="text-slate-500">Input:</span>{" "}
              <p className="mt-1 rounded bg-slate-50 p-2 font-mono text-xs">
                {run.input_summary}
              </p>
            </div>
          )}
          {run.output_summary && (
            <div className="col-span-2">
              <span className="text-slate-500">Output:</span>{" "}
              <p className="mt-1 rounded bg-slate-50 p-2 font-mono text-xs">
                {run.output_summary}
              </p>
            </div>
          )}
          {run.error && (
            <div className="col-span-2">
              <span className="text-slate-500">Error:</span>
              <p className="mt-1 rounded bg-red-50 p-2 font-mono text-xs text-red-700">
                {run.error}
              </p>
            </div>
          )}
        </div>
      </div>

      {/* Spans */}
      <section className="rounded-xl border border-slate-200 bg-white p-6">
        <h2 className="mb-4 text-sm font-semibold text-slate-700">
          Spans ({spans.length})
        </h2>
        {spans.length > 0 ? (
          <SpanTree spans={spans} />
        ) : (
          <p className="text-sm text-slate-500">No spans recorded.</p>
        )}
      </section>

      {/* Events */}
      {events.length > 0 && (
        <section className="rounded-xl border border-slate-200 bg-white p-6">
          <h2 className="mb-4 text-sm font-semibold text-slate-700">
            Events ({events.length})
          </h2>
          <EventTable events={events} />
        </section>
      )}
    </div>
  );
}
