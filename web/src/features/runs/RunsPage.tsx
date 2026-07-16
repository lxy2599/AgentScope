import { useParams, Link } from "react-router-dom";
import { useProjectRuns } from "../../api/hooks";
import { Badge, statusBadge } from "../../components/ui/Badge";

export function RunsPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const { data: runs, isLoading } = useProjectRuns(projectId ?? "");

  if (isLoading) {
    return <div className="text-sm text-slate-500">Loading runs...</div>;
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-slate-900">Runs</h1>

      {runs && runs.length > 0 ? (
        <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-slate-100 bg-slate-50 text-left text-xs font-medium uppercase text-slate-500">
                <th className="px-4 py-3">Status</th>
                <th className="px-4 py-3">Source</th>
                <th className="px-4 py-3">Agent</th>
                <th className="px-4 py-3">Trace ID</th>
                <th className="px-4 py-3">Created</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((r) => (
                <tr
                  key={r.id}
                  className="border-b border-slate-50 hover:bg-slate-50"
                >
                  <td className="px-4 py-3">
                    <Badge variant={statusBadge(r.status)}>{r.status}</Badge>
                  </td>
                  <td className="px-4 py-3 text-slate-600">{r.source}</td>
                  <td className="px-4 py-3 font-medium text-slate-800">
                    <Link to={`/runs/${r.id}`} className="text-blue-600 hover:underline">
                      {r.agent_name}
                    </Link>
                  </td>
                  <td className="max-w-[120px] truncate px-4 py-3 font-mono text-xs text-slate-400">
                    {r.trace_id}
                  </td>
                  <td className="px-4 py-3 text-slate-400">
                    {new Date(r.created_at).toLocaleString()}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="text-sm text-slate-500">No runs for this project.</p>
      )}
    </div>
  );
}
