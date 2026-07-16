import { useCallback, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Beaker, ExternalLink, Key, Plus } from "lucide-react";
import { Button } from "../../components/ui/Button";
import { Input } from "../../components/ui/Input";
import {
  useCreateProjectToken,
  useDatasets,
  useExperiments,
} from "../../api/hooks";
import { DatasetsPanel } from "../datasets/DatasetsPanel";
import { CasesTable } from "../datasets/CasesTable";
import { CreateExperimentPanel } from "../experiments/CreateExperimentPanel";
import type { Dataset } from "../../api/types";

export function ProjectHomePage() {
  const { projectId } = useParams<{ projectId: string }>();
  const createToken = useCreateProjectToken(projectId!);
  const { data: datasets, isLoading: datasetsLoading } = useDatasets(projectId!);
  const { data: experiments, isLoading: experimentsLoading } = useExperiments(projectId!);

  const [tokenDesc, setTokenDesc] = useState("");
  const [newToken, setNewToken] = useState("");
  const [tokenError, setTokenError] = useState("");
  const [activeDatasetId, setActiveDatasetId] = useState<string | null>(null);
  const [activeDatasetName, setActiveDatasetName] = useState("");
  const [expCreated, setExpCreated] = useState(false);

  const navigate = useNavigate();

  const handleCreateToken = async (e: React.FormEvent) => {
    e.preventDefault();
    setTokenError("");
    setNewToken("");
    if (!tokenDesc.trim()) {
      setTokenError("Description is required");
      return;
    }
    try {
      const res = await createToken.mutateAsync({
        description: tokenDesc,
        scopes: ["otlp:write", "runner:write"],
      });
      setNewToken(res.plain_token);
      setTokenDesc("");
    } catch (err: unknown) {
      setTokenError(err instanceof Error ? err.message : "Failed to create token");
    }
  };

  const handleDatasetCreated = useCallback((ds: Dataset) => {
    setActiveDatasetId(ds.id);
    setActiveDatasetName(ds.name);
  }, []);

  const handleExperimentCreated = useCallback(() => {
    setExpCreated(true);
  }, []);

  const handleViewExperiment = useCallback(
    (expId: string) => {
      navigate(`/experiments/${expId}`);
    },
    [navigate],
  );

  return (
    <div className="space-y-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold text-slate-900">Project Home</h1>
        <span className="text-sm text-slate-400">ID: {projectId}</span>
      </div>

      {/* Runs link */}
      <a
        href={`/projects/${projectId}/runs`}
        className="inline-flex items-center gap-2 text-sm font-medium text-blue-600 hover:underline"
      >
        <Beaker className="h-4 w-4" />
        View Runs
      </a>

      {/* Token creation */}
      <section className="rounded-xl border border-slate-200 bg-white p-6">
        <h2 className="mb-4 flex items-center gap-2 text-sm font-semibold text-slate-700">
          <Key className="h-4 w-4" />
          Project Token
        </h2>

        <form onSubmit={handleCreateToken} className="flex items-end gap-3">
          <div className="max-w-sm">
            <Input
              label="Token description"
              value={tokenDesc}
              onChange={(e) => setTokenDesc(e.target.value)}
              placeholder="local-runner"
            />
          </div>
          <Button type="submit" loading={createToken.isPending}>
            <Plus className="h-4 w-4" />
            Create Token
          </Button>
        </form>

        {tokenError && <p className="mt-2 text-sm text-red-600">{tokenError}</p>}

        {newToken && (
          <div className="mt-4 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm">
            <p className="font-medium text-amber-800">
              Token created — copy it now, it won't be shown again:
            </p>
            <code className="mt-1 block break-all rounded bg-amber-100 px-2 py-1 text-xs text-amber-900">
              {newToken}
            </code>
          </div>
        )}
      </section>

      {/* Dataset creation */}
      <DatasetsPanel projectId={projectId!} onCreated={handleDatasetCreated} />

      {/* Datasets list */}
      <section className="rounded-xl border border-slate-200 bg-white p-6">
        <h2 className="mb-4 text-sm font-semibold text-slate-700">
          Existing Datasets
        </h2>
        {datasetsLoading && (
          <p className="text-sm text-slate-500">Loading datasets...</p>
        )}
        {!datasetsLoading && (!datasets || datasets.length === 0) && (
          <p className="text-sm text-slate-500">
            No datasets yet. Create one above.
          </p>
        )}
        {datasets && datasets.length > 0 && (
          <div className="space-y-2">
            {datasets.map((ds) => (
              <button
                key={ds.id}
                onClick={() => {
                  setActiveDatasetId(ds.id);
                  setActiveDatasetName(ds.name);
                }}
                className={`w-full rounded-lg border px-4 py-3 text-left text-sm transition-colors ${
                  activeDatasetId === ds.id
                    ? "border-blue-300 bg-blue-50"
                    : "border-slate-200 hover:bg-slate-50"
                }`}
              >
                <div className="font-medium text-slate-900">{ds.name}</div>
                <div className="mt-0.5 text-xs text-slate-500">
                  {ds.description || "No description"} &middot;{" "}
                  <code className="rounded bg-slate-100 px-1">{ds.id}</code>
                </div>
              </button>
            ))}
          </div>
        )}
      </section>

      {/* Cases for the active dataset */}
      {activeDatasetId && (
        <section className="rounded-xl border border-slate-200 bg-white p-6">
          <h2 className="mb-4 text-sm font-semibold text-slate-700">
            Cases — {activeDatasetName}
          </h2>
          <p className="mb-4 text-xs text-slate-400">Dataset ID: {activeDatasetId}</p>
          <CasesTable datasetId={activeDatasetId} />
        </section>
      )}

      {/* Experiments list */}
      <section className="rounded-xl border border-slate-200 bg-white p-6">
        <h2 className="mb-4 text-sm font-semibold text-slate-700">
          Experiments
        </h2>

        {experimentsLoading && (
          <p className="text-sm text-slate-500">Loading experiments...</p>
        )}

        {!experimentsLoading && (!experiments || experiments.length === 0) && (
          <p className="text-sm text-slate-500">
            No experiments yet. Create one below.
          </p>
        )}

        {experiments && experiments.length > 0 && (
          <div className="mb-6 space-y-2">
            {experiments.map((exp) => (
              <div
                key={exp.id}
                className="flex items-center justify-between rounded-lg border border-slate-200 px-4 py-3"
              >
                <div className="text-sm">
                  <span className="font-medium text-slate-900">
                    {exp.target_name}
                  </span>
                  <span className="ml-2 text-xs text-slate-500">
                    {exp.status} &middot; {exp.total_cases} cases &middot;{" "}
                    {exp.passed_cases} passed
                  </span>
                </div>
                <button
                  onClick={() => handleViewExperiment(exp.id)}
                  className="inline-flex items-center gap-1 text-xs font-medium text-blue-600 hover:underline"
                >
                  <ExternalLink className="h-3 w-3" />
                  View
                </button>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Experiment creation */}
      <section className="rounded-xl border border-slate-200 bg-white p-6">
        <h2 className="mb-4 text-sm font-semibold text-slate-700">
          Create Experiment
        </h2>
        <CreateExperimentPanel
          projectId={projectId!}
          onCreated={handleExperimentCreated}
        />
        {expCreated && (
          <p className="mt-3 text-sm text-green-600">Experiment created.</p>
        )}
      </section>
    </div>
  );
}
