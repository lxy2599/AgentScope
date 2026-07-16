import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiRequest } from "./client";
import type {
  AuthResponse,
  CreateTokenResponse,
  Dataset,
  DatasetCase,
  Experiment,
  ExperimentDetail,
  Project,
  RunDetail,
  RunSummary,
  User,
} from "./types";

// ---- Auth ----
export function useLogin() {
  return useMutation({
    mutationFn: (body: { email: string; password: string }) =>
      apiRequest<AuthResponse>("/api/auth/login", {
        method: "POST",
        body: JSON.stringify(body),
      }),
  });
}

export function useRegister() {
  return useMutation({
    mutationFn: (body: {
      email: string;
      password: string;
      display_name: string;
    }) =>
      apiRequest<{ user: User }>("/api/auth/register", {
        method: "POST",
        body: JSON.stringify(body),
      }),
  });
}

// ---- Projects ----
export function useProjects() {
  return useQuery({
    queryKey: ["projects"],
    queryFn: () =>
      apiRequest<{ projects: Project[] }>("/api/projects").then(
        (r) => r.projects,
      ),
  });
}

export function useCreateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; description?: string }) =>
      apiRequest<{ project: Project }>("/api/projects", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["projects"] }),
  });
}

export function useCreateProjectToken(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { description?: string; scopes: string[] }) =>
      apiRequest<CreateTokenResponse>(
        `/api/projects/${projectId}/tokens`,
        { method: "POST", body: JSON.stringify(body) },
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: ["tokens", projectId] }),
  });
}

// ---- Datasets ----
export function useCreateDataset(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; description?: string }) =>
      apiRequest<{ dataset: Dataset }>(
        `/api/projects/${projectId}/datasets`,
        { method: "POST", body: JSON.stringify(body) },
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: ["datasets", projectId] }),
  });
}

export function useAddDatasetCase(datasetId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { input: string; expected?: string }) =>
      apiRequest<{ dataset_case: DatasetCase }>(
        `/api/datasets/${datasetId}/cases`,
        { method: "POST", body: JSON.stringify(body) },
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: ["cases", datasetId] }),
  });
}

export function useDatasetCases(datasetId: string) {
  return useQuery({
    queryKey: ["cases", datasetId],
    queryFn: () =>
      apiRequest<{ cases: DatasetCase[] }>(
        `/api/datasets/${datasetId}/cases`,
      ).then((r) => r.cases),
    enabled: !!datasetId,
  });
}

// ---- Runs ----
export function useProjectRuns(projectId: string) {
  return useQuery({
    queryKey: ["runs", projectId],
    queryFn: () =>
      apiRequest<{ runs: RunSummary[] }>(
        `/api/projects/${projectId}/runs`,
      ).then((r) => r.runs),
    enabled: !!projectId,
  });
}

export function useRunDetail(runId: string) {
  return useQuery({
    queryKey: ["run", runId],
    queryFn: () =>
      apiRequest<RunDetail>(`/api/runs/${runId}`),
    enabled: !!runId,
  });
}

// ---- Experiments ----
export function useCreateExperiment(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: {
      dataset_id: string;
      evaluator_id?: string;
      target_name: string;
      target_version?: string;
      case_ids?: string[];
    }) =>
      apiRequest<{ experiment: Experiment }>(
        `/api/projects/${projectId}/experiments`,
        { method: "POST", body: JSON.stringify(body) },
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: ["experiments", projectId] }),
  });
}

export function useExperiment(experimentId: string) {
  return useQuery({
    queryKey: ["experiment", experimentId],
    queryFn: () =>
      apiRequest<ExperimentDetail>(`/api/experiments/${experimentId}`),
    enabled: !!experimentId,
  });
}
