import { useState } from "react";
import { Plus } from "lucide-react";
import { Button } from "../../components/ui/Button";
import { Input } from "../../components/ui/Input";
import { useAddDatasetCase, useDatasetCases } from "../../api/hooks";

interface Props {
  datasetId: string;
}

export function CasesTable({ datasetId }: Props) {
  const { data: cases, isLoading } = useDatasetCases(datasetId);
  const addCase = useAddDatasetCase(datasetId);
  const [input, setInput] = useState("");
  const [expected, setExpected] = useState("");

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!input.trim()) return;
    await addCase.mutateAsync({ input, expected: expected.trim() || undefined });
    setInput("");
    setExpected("");
  };

  if (isLoading) {
    return <div className="text-sm text-slate-500">Loading cases...</div>;
  }

  return (
    <div className="space-y-4">
      <form onSubmit={handleAdd} className="flex items-end gap-3">
        <div className="flex-1">
          <Input
            label="Input"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            required
            placeholder="What is a refund?"
          />
        </div>
        <div className="flex-1">
          <Input
            label="Expected (optional)"
            value={expected}
            onChange={(e) => setExpected(e.target.value)}
            placeholder="Expected answer"
          />
        </div>
        <Button type="submit" loading={addCase.isPending}>
          <Plus className="h-4 w-4" />
          Add
        </Button>
      </form>

      {cases && cases.length > 0 ? (
        <div className="overflow-hidden rounded-lg border border-slate-200">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-slate-100 bg-slate-50 text-left text-xs font-medium uppercase text-slate-500">
                <th className="px-3 py-2">#</th>
                <th className="px-3 py-2">Input</th>
                <th className="px-3 py-2">Expected</th>
              </tr>
            </thead>
            <tbody>
              {cases.map((c) => (
                <tr key={c.id} className="border-b border-slate-50">
                  <td className="px-3 py-2 text-slate-400">{c.sort_order}</td>
                  <td className="max-w-xs truncate px-3 py-2 font-mono text-xs">
                    {c.input}
                  </td>
                  <td className="max-w-xs truncate px-3 py-2 font-mono text-xs text-slate-500">
                    {c.expected || "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="text-sm text-slate-500">No cases yet. Add one above.</p>
      )}
    </div>
  );
}
