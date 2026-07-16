import { useParams } from "react-router-dom";
import { AlertCircle, Info } from "lucide-react";
import { useExperiment } from "../../api/hooks";
import { Badge, statusBadge } from "../../components/ui/Badge";
import { StatusSummary } from "./StatusSummary";

export function ExperimentDetailPage() {
  const { experimentId } = useParams<{ experimentId: string }>();
  const { data, isLoading, error } = useExperiment(experimentId ?? "");

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

  if (!data) {
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
          <Badge variant={statusBadge(data.status)}>{data.status}</Badge>
        </div>

        <div className="mt-6 space-y-6">
          <StatusSummary detail={data} />
        </div>

        <div className="mt-6 grid grid-cols-2 gap-4 text-sm">
          <div>
            <span className="text-slate-500">Target:</span>{" "}
            {data.target_name}
          </div>
          <div>
            <span className="text-slate-500">Version:</span>{" "}
            {data.target_version || "—"}
          </div>
          <div>
            <span className="text-slate-500">Dataset:</span>{" "}
            <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">
              {data.dataset_id}
            </code>
          </div>
          <div>
            <span className="text-slate-500">Evaluator:</span>{" "}
            <code className="rounded bg-slate-100 px-1 py-0.5 text-xs">
              {data.evaluator_id || "—"}
            </code>
          </div>
        </div>
      </div>

      {/* Result rows placeholder */}
      <section className="rounded-xl border border-slate-200 bg-white p-6">
        <h2 className="text-sm font-semibold text-slate-700">Results</h2>
        <p className="mt-2 text-sm text-slate-500">
          Result case-level detail is not yet available from the API. Status
          counts reflect the current aggregate state.
        </p>
      </section>
    </div>
  );
}
