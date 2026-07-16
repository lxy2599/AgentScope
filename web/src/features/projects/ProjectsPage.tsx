import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Plus } from "lucide-react";
import { useCreateProject, useProjects } from "../../api/hooks";
import { Button } from "../../components/ui/Button";
import { Input } from "../../components/ui/Input";

export function ProjectsPage() {
  const { data: projects, isLoading } = useProjects();
  const createProject = useCreateProject();
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    const res = await createProject.mutateAsync({ name, description: desc });
    setName("");
    setDesc("");
    navigate(`/projects/${res.project.id}`);
  };

  if (isLoading) {
    return <div className="text-slate-500">Loading projects...</div>;
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-slate-900">Projects</h1>

      {/* Create form */}
      <form onSubmit={handleCreate} className="flex items-end gap-3">
        <div className="flex-1">
          <Input
            label="New project name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            placeholder="my-agent-project"
          />
        </div>
        <div className="flex-1">
          <Input
            label="Description (optional)"
            value={desc}
            onChange={(e) => setDesc(e.target.value)}
            placeholder="Agent evaluation project"
          />
        </div>
        <Button type="submit" loading={createProject.isPending}>
          <Plus className="h-4 w-4" />
          Create
        </Button>
      </form>

      {/* Project list */}
      {projects && projects.length > 0 ? (
        <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-slate-100 bg-slate-50 text-left text-xs font-medium uppercase text-slate-500">
                <th className="px-4 py-3">Name</th>
                <th className="px-4 py-3">Description</th>
                <th className="px-4 py-3">Created</th>
              </tr>
            </thead>
            <tbody>
              {projects.map((p) => (
                <tr
                  key={p.id}
                  className="cursor-pointer border-b border-slate-50 hover:bg-slate-50"
                  onClick={() => navigate(`/projects/${p.id}`)}
                >
                  <td className="px-4 py-3 font-medium text-blue-600">
                    {p.name}
                  </td>
                  <td className="px-4 py-3 text-slate-500">
                    {p.description || "—"}
                  </td>
                  <td className="px-4 py-3 text-slate-400">
                    {p.created_at
                      ? new Date(p.created_at).toLocaleDateString()
                      : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="text-sm text-slate-500">
          No projects yet. Create one above.
        </p>
      )}
    </div>
  );
}
