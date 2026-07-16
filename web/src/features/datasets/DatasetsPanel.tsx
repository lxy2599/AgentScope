import { useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { Plus } from "lucide-react";
import { Button } from "../../components/ui/Button";
import { Input } from "../../components/ui/Input";
import { useCreateDataset } from "../../api/hooks";

export function DatasetsPanel() {
  const { projectId } = useParams<{ projectId: string }>();
  const navigate = useNavigate();
  const createDataset = useCreateDataset(projectId!);
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    await createDataset.mutateAsync({ name, description: desc });
    setName("");
    setDesc("");
    navigate(`/projects/${projectId}/datasets`);
  };

  return (
    <section className="rounded-xl border border-slate-200 bg-white p-6">
      <h2 className="mb-4 text-sm font-semibold text-slate-700">Datasets</h2>

      <form onSubmit={handleCreate} className="flex items-end gap-3">
        <div className="max-w-sm">
          <Input
            label="Dataset name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            placeholder="support-cases"
          />
        </div>
        <div className="max-w-sm">
          <Input
            label="Description (optional)"
            value={desc}
            onChange={(e) => setDesc(e.target.value)}
            placeholder="Support agent test cases"
          />
        </div>
        <Button type="submit" loading={createDataset.isPending}>
          <Plus className="h-4 w-4" />
          Create
        </Button>
      </form>
    </section>
  );
}
