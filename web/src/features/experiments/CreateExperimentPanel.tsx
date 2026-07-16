import { useState } from "react";
import { useCreateExperiment } from "../../api/hooks";
import { Button } from "../../components/ui/Button";
import { Input } from "../../components/ui/Input";

interface Props {
  projectId: string;
  onCreated?: () => void;
}

export function CreateExperimentPanel({ projectId, onCreated }: Props) {
  const create = useCreateExperiment(projectId);
  const [datasetId, setDatasetId] = useState("");
  const [targetName, setTargetName] = useState("");
  const [targetVersion, setTargetVersion] = useState("");
  const [caseIds, setCaseIds] = useState("");
  const [error, setError] = useState("");

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");

    if (!datasetId.trim() || !targetName.trim()) {
      setError("Dataset ID and Target name are required");
      return;
    }

    const ids = caseIds
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean);

    try {
      await create.mutateAsync({
        dataset_id: datasetId.trim(),
        target_name: targetName.trim(),
        target_version: targetVersion.trim() || undefined,
        case_ids: ids.length > 0 ? ids : undefined,
      });
      setDatasetId("");
      setTargetName("");
      setTargetVersion("");
      setCaseIds("");
      onCreated?.();
    } catch (err: unknown) {
      setError(
        err instanceof Error ? err.message : "Failed to create experiment",
      );
    }
  };

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <Input
        label="Dataset ID"
        value={datasetId}
        onChange={(e) => setDatasetId(e.target.value)}
        required
        placeholder="uuid or name"
      />
      <Input
        label="Target Name"
        value={targetName}
        onChange={(e) => setTargetName(e.target.value)}
        required
        placeholder="support-agent"
      />
      <Input
        label="Target Version (optional)"
        value={targetVersion}
        onChange={(e) => setTargetVersion(e.target.value)}
        placeholder="1.0.0"
      />
      <div className="space-y-1">
        <label className="block text-sm font-medium text-slate-700">
          Case IDs (optional, one per line)
        </label>
        <textarea
          className="block w-full rounded-lg border border-slate-300 px-3 py-2 text-sm shadow-sm placeholder:text-slate-400 focus:outline-none focus:ring-2 focus:ring-blue-500"
          rows={3}
          value={caseIds}
          onChange={(e) => setCaseIds(e.target.value)}
          placeholder="case-uuid-1&#10;case-uuid-2"
        />
      </div>

      {error && <p className="text-sm text-red-600">{error}</p>}

      <Button type="submit" loading={create.isPending}>
        Create Experiment
      </Button>
    </form>
  );
}
