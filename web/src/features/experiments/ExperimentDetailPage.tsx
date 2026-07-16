import { useCallback } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { AlertCircle, ExternalLink, Info } from "lucide-react";
import { useExperiment, useExperimentResults } from "../../api/hooks";
import { Badge, statusBadge } from "../../components/ui/Badge";
import { StatusSummary } from "./StatusSummary";

export function ExperimentDetailPage() {
  const { experimentId } = useParams<{ experimentId: string }>();
  const { data: experiment, isLoading, error } = useExperiment(experimentId ?? "");

  const projectId = experiment?.project_id ?? "";
  const { data: results, isLoading: resultsLoading } = useExperimentResults(projectId, experimentId ?? "");

  const navigate = useNavigate();

  const handleRunClick = useCallback(
    (runId: string) => {
      navigate(`/runs/${runId}`);
    },
    [navigate],
  );

  if (isLoading) {
    return <div className="text-sm text-slate-500">Loading experiment...</div>;
  }

  if (error) {
    return (
      <div className="flex items-center gap-2 text-sm text-red-600">
        <AlertCircle className="h-4 w-4" />
        {error instanceof Error ? error.message : "Failed to load experiment"}
      </div>
    );
  }

  if (!experiment) {
    return (
      <div className="flex items-center gap-2 text-sm text-slate-500">
        <Info className="h-4 w-4" />
        Experiment not found.
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="rounded-xl border border-slate-200 bg-white p-6">
        <div className="flex items-center justify-between">
          <h1 className="text-2xl font-bold text-slate-900">
            Experiment {experimentId?.slice(0, 8)}
          </h1>
          <Badge variant={statusBadge(experiment.status)}>{experiment.status}</Badge>
        </div>

        <div className="mt-6 space-y-6">
          <StatusSummary detail={experiment} />
        </div>

        <div className="mt-6 grid grid-cols-2 gap-4 text-sm">
          <div>
            <span className="text-slate-500">Target:</span>{" "}
            {experiment.target_name}
          </div>
          <div>
            <span className="text-slate-500">Version:</span>{" "}
            {experiment.target_version || "—"}
          </div>
          <div>
            <span className="text-slate-500">Dataset:</span>{" "}
            <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">
              {experiment.dataset_id}
            </code>
          </div>
          <div>
            <span className="text-slate-500">Evaluator:</span>{" "}
            <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">
              {experiment.evaluator_id || "—"}
            </code>
          </div>
        </div>
      </div>

      {/* Results table */}
      <section className="rounded-xl border border-slate-200 bg-white p-6">
        <h2 className="mb-4 text-sm font-semibold text-slate-700">Results</h2>

        {resultsLoading && (
          <p className="text-sm text-slate-500">Loading results...</p>
        )}

        {!resultsLoading && (!results || results.length === 0) && (
          <p className="text-sm text-slate-500">
            No results yet. Results appear after the runner submits evaluation data.
          </p>
        )}

        {results && results.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b border-slate-200 text-xs uppercase text-slate-500">
                  <th className="pb-2 pr-4 font-medium">Passed</th>
                  <th className="pb-2 pr-4 font-medium">Score</th>
                  <th className="pb-2 pr-4 font-medium">Case</th>
                  <th className="pb-2 pr-4 font-medium">Reason</th>
                  <th className="pb-2 pr-4 font-medium">Latency</th>
                  <th className="pb-2 pr-4 font-medium">Cost</th>
                  <th className="pb-2 pr-4 font-medium">Run</th>
                  <th className="pb-2 font-medium">Created</th>
                </tr>
              </thead>
              <tbody>
                {results.map((r) => (
                  <tr key={r.id} className="border-b border-slate-100 text-slate-700">
                    <td className="py-2 pr-4">
                      <span
                        className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${
                          r.passed
                            ? "bg-green-100 text-green-700"
                            : "bg-red-100 text-red-700"
                        }`}
                      >
                        {r.passed ? "PASS" : "FAIL"}
                      </span>
                    </td>
                    <td className="py-2 pr-4 font-mono text-xs">
                      {r.score.toFixed(2)}
                    </td>
                    <td className="py-2 pr-4 font-mono text-xs" title={r.dataset_case_id}>
                      {r.dataset_case_id ? r.dataset_case_id.slice(0, 8) : "—"}
                    </td>
                    <td className="max-w-xs truncate py-2 pr-4 text-xs" title={r.reason}>
                      {r.reason || "—"}
                    </td>
                    <td className="py-2 pr-4 font-mono text-xs">
                      {r.latency_ms != null ? `${r.latency_ms}ms` : "—"}
                    </td>
                    <td className="py-2 pr-4 font-mono text-xs">
                      {r.cost != null ? r.cost.toFixed(6) : "—"}
                    </td>
                    <td className="py-2 pr-4">
                      {r.run_id ? (
                        <button
                          onClick={() => handleRunClick(r.run_id!)}
                          className="inline-flex items-center gap-1 text-xs text-blue-600 hover:underline"
                        >
                          <ExternalLink className="h-3 w-3" />
                          Trace
                        </button>
                      ) : (
                        <span className="text-xs text-slate-400">—</span>
                      )}
                    </td>
                    <td className="py-2 font-mono text-xs text-slate-400">{r.created_at}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
