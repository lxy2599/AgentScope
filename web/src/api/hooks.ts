import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiRequest } from "./client";
import type {
  ActiveRun,
  RunRuntimeDetail,
  AgentDetail,
  AgentInfo,
  AgentSummary,
  RecentRunType,
  CreateTokenResponse,
  Dataset,
  DatasetCase,
  EvalCaseResult,
  EvalScore,
  Experiment,
  ExperimentDetail,
  ExperimentReport,
  MetricDefinition,
  MetricKindAggregation,
  Project,
  RunDetail,
  RunSummary,
  SamplingRule,
  AnnotationQueue,
  AnnotationItem,
  IngestAgentStatsItem,
  IngestStatsResponse,
  ComparisonDetail,
  ExperimentComparison,
  RunRuntimeStatus,
  User,
} from "./types";

// Running states that should trigger polling
const LIVE_STATUSES: readonly RunRuntimeStatus[] = [
  'running',
  'thinking',
  'waiting_llm',
  'waiting_tool',
  'waiting_retrieval',
  'waiting_memory',
  'waiting_user',
  'retrying',
] as const;

/** Returns the refetch interval for Run Detail based on run status.
 *  Live (in-progress) statuses poll every 2500ms; terminal or unknown states stop polling. */
export function getRunDetailRefetchInterval(status?: string): 2500 | false {
  if (!status) return false;
  return (LIVE_STATUSES as readonly string[]).includes(status) ? 2500 : false;
}

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
    refetchInterval: (query) => {
      return getRunDetailRefetchInterval(query.state.data?.run?.status);
    },
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

export function useExperimentReport(projectId: string, experimentId: string) {
  return useQuery({
    queryKey: ["experimentReport", projectId, experimentId],
    queryFn: () =>
      apiRequest<ExperimentReport>(
        `/api/projects/${projectId}/experiments/${experimentId}/report`,
      ),
    enabled: !!projectId && !!experimentId,
  });
}

// ---- Agents ----
export function useAgents(projectId: string) {
  return useQuery({
    queryKey: ["agents", projectId],
    queryFn: () =>
      apiRequest<AgentInfo[]>(`/api/v1/agents?project_id=${projectId}`),
    enabled: !!projectId,
  });
}

export function useAgentDetail(agentId: string) {
  return useQuery({
    queryKey: ["agent", agentId],
    queryFn: () => apiRequest<AgentDetail>(`/api/v1/agents/${agentId}`),
    enabled: !!agentId,
  });
}

// Agent control center summaries
export function useAgentSummaries(projectId: string) {
  return useQuery({
    queryKey: ["agentSummaries", projectId],
    queryFn: () =>
      apiRequest<AgentSummary[]>(
        `/api/v1/agent-summary?project_id=${projectId}`,
      ),
    enabled: !!projectId,
    refetchInterval: 10_000,
  });
}

// Recent errors for an agent (last 24h failed/timeout runs)
export function useAgentRecentErrors(agentId: string) {
  return useQuery({
    queryKey: ["agentRecentErrors", agentId],
    queryFn: () =>
      apiRequest<RecentRunType[]>(
        `/api/v1/agents/${agentId}/recent-errors`,
      ),
    enabled: !!agentId,
    refetchInterval: 15_000,
  });
}

// ---- Active Runs ----
export function useActiveRuns(projectId: string, agentId?: string) {
  const params = new URLSearchParams({ limit: "50" });
  if (agentId) params.set("agent_id", agentId);
  return useQuery({
    queryKey: ["activeRuns", projectId, agentId],
    queryFn: () =>
      apiRequest<ActiveRun[]>(
        `/api/projects/${projectId}/active-runs?${params.toString()}`,
      ),
    enabled: !!projectId,
    refetchInterval: 3000,
  });
}

export function useRunRuntimeDetail(projectId: string, runId: string) {
  return useQuery({
    queryKey: ["runRuntimeDetail", projectId, runId],
    queryFn: () =>
      apiRequest<RunRuntimeDetail>(
        `/api/projects/${projectId}/runs/${runId}/runtime`,
      ),
    enabled: !!projectId && !!runId,
    refetchInterval: (query) => {
      return getRunDetailRefetchInterval(query.state.data?.status);
    },
  });
}

// ---- Multi-dimensional evaluation scores (Phase 2.5) ----

export function useEvalScoresByRun(projectId: string, runId: string) {
  return useQuery({
    queryKey: ["evalScores", projectId, runId],
    queryFn: () =>
      apiRequest<EvalScore[]>(
        `/api/projects/${projectId}/eval-scores/by-run/${runId}`,
      ),
    enabled: !!projectId && !!runId,
  });
}

export function useExperimentEvalScores(
  projectId: string,
  experimentId: string,
) {
  return useQuery({
    queryKey: ["experimentEvalScores", projectId, experimentId],
    queryFn: () =>
      apiRequest<MetricKindAggregation[]>(
        `/api/projects/${projectId}/experiments/${experimentId}/eval-scores`,
      ),
    enabled: !!projectId && !!experimentId,
  });
}

export function useMetricDefinitions(projectId: string) {
  return useQuery({
    queryKey: ["metricDefinitions", projectId],
    queryFn: () =>
      apiRequest<MetricDefinition[]>(
        `/api/projects/${projectId}/metric-definitions`,
      ),
    enabled: !!projectId,
  });
}

export function useCreateMetricDefinition(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; kind: string; config?: string; weight?: number; pass_threshold?: number; enabled?: boolean }) =>
      apiRequest<MetricDefinition>(
        `/api/projects/${projectId}/metric-definitions`,
        { method: "POST", body: JSON.stringify(body) },
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["metricDefinitions", projectId] }),
  });
}

export function useUpdateMetricDefinition(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: { id: string; name: string; kind: string; config?: string; weight?: number; pass_threshold?: number; enabled?: boolean }) =>
      apiRequest<MetricDefinition>(
        `/api/projects/${projectId}/metric-definitions/${id}`,
        { method: "PUT", body: JSON.stringify(body) },
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["metricDefinitions", projectId] }),
  });
}

export function useDeleteMetricDefinition(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      apiRequest<void>(
        `/api/projects/${projectId}/metric-definitions/${id}`,
        { method: "DELETE" },
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["metricDefinitions", projectId] }),
  });
}

// ---- Evaluators ----

export function useEvaluators(projectId: string) {
  return useQuery({
    queryKey: ["evaluators", projectId],
    queryFn: () =>
      apiRequest<any[]>(`/api/projects/${projectId}/evaluators`),
    enabled: !!projectId,
  });
}

export function useCreateEvaluator(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; type: string; config: any }) =>
      apiRequest<any>(
        `/api/projects/${projectId}/evaluators`,
        { method: "POST", body: JSON.stringify(body) },
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["evaluators", projectId] }),
  });
}

export function useCreateEvalScore(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: {
      run_id: string;
      metric_name: string;
      metric_kind: string;
      score: number;
      passed: boolean;
      reason?: string;
    }) =>
      apiRequest<EvalScore>(
        `/api/projects/${projectId}/eval-scores`,
        { method: "POST", body: JSON.stringify(body) },
      ),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: ["evalScores"] }),
  });
}

// ---- Phase 2.6: Online Eval & Annotation ----

export function useSamplingRules(projectId: string) {
  return useQuery({
    queryKey: ['samplingRules', projectId],
    queryFn: () =>
      apiRequest<SamplingRule[]>(),
    enabled: !!projectId,
  });
}



export function useCreateSamplingRule(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; sample_rate: number; filters?: string; agent_id?: string }) =>
      apiRequest<SamplingRule>(
        `/api/projects/${projectId}/sampling-rules`,
        { method: 'POST', body: JSON.stringify(body) },
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['samplingRules', projectId] }),
  });
}

export function useUpdateSamplingRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ ruleId, enabled }: { ruleId: string; enabled: boolean }) =>
      apiRequest<SamplingRule>(
        `/api/sampling-rules/${ruleId}`,
        { method: 'PATCH', body: JSON.stringify({ enabled }) },
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['samplingRules'] }),
  });
}

export function useDeleteSamplingRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ruleId: string) =>
      apiRequest<void>(`/api/sampling-rules/${ruleId}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['samplingRules'] }),
  });
}

export function useAnnotationQueues(projectId: string) {
  return useQuery({
    queryKey: ['annotationQueues', projectId],
    queryFn: () =>
      apiRequest<AnnotationQueue[]>(`/api/projects/${projectId}/annotation-queues`),
    enabled: !!projectId,
  });
}

export function useCreateAnnotationQueue(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; description?: string }) =>
      apiRequest<AnnotationQueue>(
        `/api/projects/${projectId}/annotation-queues`,
        { method: 'POST', body: JSON.stringify(body) },
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['annotationQueues', projectId] }),
  });
}

export function useAnnotationQueueItems(queueId: string) {
  return useQuery({
    queryKey: ['annotationItems', queueId],
    queryFn: () =>
      apiRequest<AnnotationItem[]>(`/api/annotation-queues/${queueId}/items`),
    enabled: !!queueId,
  });
}

export function useUpdateAnnotationItem() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ itemId, ...body }: { itemId: string; status?: string; corrected_output?: string; expected_tool_path?: string; comment?: string; failure_category?: string; privacy_reviewed?: boolean; reviewer_note?: string }) =>
      apiRequest<AnnotationItem>(
        `/api/annotation-items/${itemId}`,
        { method: 'PATCH', body: JSON.stringify(body) },
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['annotationItems'] }),
  });
}

export function useCreateAnnotationItemFromRun(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ runId, queueId }: { runId: string; queueId?: string }) =>
      apiRequest<AnnotationItem>(
        `/api/projects/${projectId}/runs/${runId}/annotation-items`,
        { method: 'POST', body: JSON.stringify({ queue_id: queueId }) },
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['annotationItems'] }),
  });
}

export function usePromoteAnnotationItem() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ itemId, datasetId }: { itemId: string; datasetId: string }) =>
      apiRequest<{ case_id: string }>(
        `/api/annotation-items/${itemId}/promote-to-dataset`,
        { method: 'POST', body: JSON.stringify({ dataset_id: datasetId }) },
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['annotationItems'] }),
  });
}

// ---- Phase 2.7: Ingest Health ----

export function useProjectIngestStats(projectId: string, minutes: number = 15) {
  return useQuery({
    queryKey: ['ingestStats', projectId, minutes],
    queryFn: () =>
      apiRequest<IngestStatsResponse>(
        `/api/projects/${projectId}/ingest-stats?minutes=${minutes}`,
      ),
    enabled: !!projectId,
    refetchInterval: 30_000, // refresh every 30s
  });
}

export function useProjectIngestAgentStats(projectId: string, minutes: number = 15) {
  return useQuery({
    queryKey: ['ingestAgentStats', projectId, minutes],
    queryFn: () =>
      apiRequest<IngestAgentStatsItem[]>(
        `/api/projects/${projectId}/ingest-stats/agents?minutes=${minutes}`,
      ),
    enabled: !!projectId,
    refetchInterval: 30_000,
  });
}


// ---- Phase 3.6: Experiment Comparison ----

export function useCreateComparison(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: {
      baseline_experiment_id: string;
      candidate_experiment_id: string;
      comparison_type: string;
      regression_gates?: string;
    }) =>
      apiRequest<ComparisonDetail>(
        `/api/projects/${projectId}/comparisons`,
        { method: "POST", body: JSON.stringify(body) },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["comparisons", projectId] });
    },
  });
}

export function useComparison(comparisonId: string) {
  return useQuery({
    queryKey: ["comparison", comparisonId],
    queryFn: () =>
      apiRequest<ComparisonDetail>(`/api/comparisons/${comparisonId}`),
    enabled: !!comparisonId,
  });
}

export function useComparisons(projectId: string) {
  return useQuery({
    queryKey: ["comparisons", projectId],
    queryFn: () =>
      apiRequest<ExperimentComparison[]>(
        `/api/projects/${projectId}/comparisons`,
      ),
    enabled: !!projectId,
  });
}
