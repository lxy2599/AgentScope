import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiRequest } from "./client";
import type {
  CreateTokenResponse,
  Dataset,
  DatasetCase,
  EvalCaseResult,
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
      apiRequest<User>("/api/auth/login", {
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
      apiRequest<User>("/api/auth/register", {
        method: "POST",
        body: JSON.stringify(body),
      }),
  });
}

// ---- Projects ----
export function useProjects() {
  return useQuery({
    queryKey: ["projects"],
    queryFn: () => apiRequest<Project[]>("/api/projects"),
  });
}

export function useCreateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; description?: string }) =>
      apiRequest<Project>("/api/projects", {
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
      apiRequest<Dataset>(
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
      apiRequest<DatasetCase>(
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
      apiRequest<DatasetCase[]>(`/api/datasets/${datasetId}/cases`),
    enabled: !!datasetId,
  });
}

// ---- Runs ----
export function useProjectRuns(projectId: string) {
  return useQuery({
    queryKey: ["runs", projectId],
    queryFn: () =>
      apiRequest<RunSummary[]>(`/api/projects/${projectId}/runs`),
    enabled: !!projectId,
  });
}

export function useRunDetail(runId: string) {
  return useQuery({
    queryKey: ["run", runId],
    queryFn: () => apiRequest<RunDetail>(`/api/runs/${runId}`),
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
      case_ids: string[];
    }) =>
      apiRequest<Experiment>(
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

export function useDatasets(projectId: string) {
  return useQuery({
    queryKey: ["datasets", projectId],
    queryFn: () =>
      apiRequest<Dataset[]>(`/api/projects/${projectId}/datasets`),
    enabled: !!projectId,
  });
}

export function useExperiments(projectId: string) {
  return useQuery({
    queryKey: ["experiments", projectId],
    queryFn: () =>
      apiRequest<Experiment[]>(`/api/projects/${projectId}/experiments`),
    enabled: !!projectId,
  });
}

export function useExperimentResults(projectId: string, experimentId: string) {
  return useQuery({
    queryKey: ["experimentResults", projectId, experimentId],
    queryFn: () =>
      apiRequest<EvalCaseResult[]>(
        `/api/projects/${projectId}/experiments/${experimentId}/results`,
      ),
    enabled: !!projectId && !!experimentId,
  });
}
