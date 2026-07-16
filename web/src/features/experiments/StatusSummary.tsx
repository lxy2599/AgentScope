import type { ExperimentDetail } from "../../api/types";

interface Props {
  detail: ExperimentDetail;
}

export function StatusSummary({ detail }: Props) {
  const { total_cases, status_counts } = detail;
  const queued = status_counts?.queued ?? 0;
  const running = status_counts?.running ?? 0;
  const passed = status_counts?.passed ?? 0;
  const failed = status_counts?.failed ?? 0;
  const passedPct = total_cases > 0 ? ((passed / total_cases) * 100).toFixed(0) : "0";
  const failedPct = total_cases > 0 ? ((failed / total_cases) * 100).toFixed(0) : "0";
  const queuedPct = total_cases > 0 ? ((queued / total_cases) * 100).toFixed(0) : "0";

  return (
    <div className="space-y-4">
      <div className="text-sm text-slate-500">
        Total: {total_cases} cases
      </div>

      <div className="grid grid-cols-4 gap-3">
        <div className="rounded-lg border border-green-200 bg-green-50 p-3 text-center">
          <div className="text-2xl font-bold text-green-700">{passed}</div>
          <div className="text-xs text-green-600">Passed</div>
          <div className="text-xs text-green-500">{passedPct}%</div>
        </div>

        <div className="rounded-lg border border-red-200 bg-red-50 p-3 text-center">
          <div className="text-2xl font-bold text-red-700">{failed}</div>
          <div className="text-xs text-red-600">Failed</div>
          <div className="text-xs text-red-500">{failedPct}%</div>
        </div>

        <div className="rounded-lg border border-blue-200 bg-blue-50 p-3 text-center">
          <div className="text-2xl font-bold text-blue-700">{running}</div>
          <div className="text-xs text-blue-600">Running</div>
        </div>

        <div className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-center">
          <div className="text-2xl font-bold text-slate-500">{queued}</div>
          <div className="text-xs text-slate-500">Queued</div>
          <div className="text-xs text-slate-400">{queuedPct}%</div>
        </div>
      </div>
    </div>
  );
}
