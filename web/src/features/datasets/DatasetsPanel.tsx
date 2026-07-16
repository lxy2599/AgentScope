import { useState } from "react";
import { Plus } from "lucide-react";
import { Button } from "../../components/ui/Button";
import { Input } from "../../components/ui/Input";
import { useCreateDataset } from "../../api/hooks";
import type { Dataset } from "../../api/types";

interface Props {
  projectId: string;
  onCreated: (ds: Dataset) => void;
}

export function DatasetsPanel({ projectId, onCreated }: Props) {
  const createDataset = useCreateDataset(projectId);
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    const ds = await createDataset.mutateAsync({ name, description: desc });
    setName("");
    setDesc("");
    onCreated(ds);
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
