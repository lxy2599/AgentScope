import { useState } from "react";
import { useParams } from "react-router-dom";
import { Key, Plus } from "lucide-react";
import { Button } from "../../components/ui/Button";
import { Input } from "../../components/ui/Input";
import { useCreateProjectToken } from "../../api/hooks";

export function ProjectHomePage() {
  const { projectId } = useParams<{ projectId: string }>();
  const createToken = useCreateProjectToken(projectId!);
  const [tokenDesc, setTokenDesc] = useState("");
  const [newToken, setNewToken] = useState("");
  const [tokenError, setTokenError] = useState("");

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

  return (
    <div className="space-y-8">
      <h1 className="text-2xl font-bold text-slate-900">Project Home</h1>
      <p className="text-sm text-slate-500">ID: {projectId}</p>

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

      {/* Dataset / Experiment links — placeholder content for now */}
      <section className="rounded-xl border border-slate-200 bg-white p-6">
        <h2 className="mb-4 text-sm font-semibold text-slate-700">
          Quick Links
        </h2>
        <div className="space-y-2 text-sm">
          <p>
            <a
              href={`/projects/${projectId}/runs`}
              className="text-blue-600 hover:underline"
            >
              View Runs
            </a>
          </p>
        </div>
      </section>
    </div>
  );
}
