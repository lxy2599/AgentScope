import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { apiRequest, benchmarkApiPaths } from "./client";
import {
  subscribeProjectEvents,
  type RealtimeEvent,
  type RealtimeStatus,
} from "./realtime";
import type {
  ActiveRun,
  RunRuntimeDetail,
  AgentDetail,
  AgentInfo,
  AgentSummary,
  RecentRunType,
  CreateTokenResponse,
  CreateProjectTokenInput,
  ProjectToken,
  ProjectTokenAuditEvent,
  ProjectTokenPolicy,
  ProjectTokenRiskSummary,
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
  IngestHealthEventItem,
  ComparisonDetail,
  ExperimentComparison,
  RunRuntimeStatus,
  RunSnapshot,
  User,
  RubricTemplate,
  RubricTemplateVersion,
  JudgeEvaluation,
  CalibrationRun,
  ProjectQuota,
  ProjectQuotaStatus,
  OperationalAnomaly,
  OnlineEvalJob,
  BenchmarkExperimentReport,
  BenchmarkExperimentLaunch,
  BenchmarkLaunchResponse,
  BenchmarkPreflight,
  BenchmarkPreflightInput,
  BenchmarkSuite,
  BenchmarkTarget,
  CreateBenchmarkExperimentInput,
  CreateBenchmarkSuiteInput,
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

export function useProjectTokens(projectId: string) {
  return useQuery({ queryKey: ["tokens", projectId], queryFn: () => apiRequest<ProjectToken[]>(`/api/projects/${projectId}/tokens`), enabled: !!projectId });
}
export function useCreateProjectToken(projectId: string) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: (body: CreateProjectTokenInput) => apiRequest<CreateTokenResponse>(`/api/projects/${projectId}/tokens`, { method: "POST", body: JSON.stringify(body) }), onSuccess: () => qc.invalidateQueries({ queryKey: ["tokens", projectId] }) });
}
export function useRevokeProjectToken(projectId: string) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: ({ tokenId, reason }: { tokenId: string; reason?: string }) => apiRequest<ProjectToken>(`/api/projects/${projectId}/tokens/${tokenId}/revoke`, { method: "POST", body: JSON.stringify({ reason: reason ?? "" }) }), onSuccess: () => qc.invalidateQueries({ queryKey: ["tokens", projectId] }) });
}
export function useRotateProjectToken(projectId: string) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: ({ tokenId, ...body }: CreateProjectTokenInput & { tokenId: string }) => apiRequest<CreateTokenResponse>(`/api/projects/${projectId}/tokens/${tokenId}/rotate`, { method: "POST", body: JSON.stringify(body) }), onSuccess: () => qc.invalidateQueries({ queryKey: ["tokens", projectId] }) });
}
export function useProjectTokenAudit(projectId: string, tokenId: string) { return useQuery({ queryKey: ["tokenAudit", projectId, tokenId], queryFn: () => apiRequest<ProjectTokenAuditEvent[]>(`/api/projects/${projectId}/tokens/${tokenId}/audit-events`), enabled: !!projectId && !!tokenId }); }
export function useProjectTokenPolicy(projectId: string) { return useQuery({ queryKey: ["tokenPolicy", projectId], queryFn: () => apiRequest<ProjectTokenPolicy>(`/api/projects/${projectId}/token-policy`), enabled: !!projectId }); }
export function useProjectTokenRiskSummary(projectId: string) { return useQuery({ queryKey: ["tokenRiskSummary", projectId], queryFn: () => apiRequest<ProjectTokenRiskSummary>(`/api/projects/${projectId}/tokens/risk-summary`), enabled: !!projectId }); }
export function useUpdateProjectTokenPolicy(projectId: string) { const qc=useQueryClient(); return useMutation({ mutationFn:(body: ProjectTokenPolicy)=>apiRequest<ProjectTokenPolicy>(`/api/projects/${projectId}/token-policy`,{method:"PUT",body:JSON.stringify(body)}),onSuccess:()=>qc.invalidateQueries({queryKey:["tokenPolicy",projectId]})}); }

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

export function useRunSnapshot(runId: string) {
  return useQuery({
    queryKey: ["runSnapshot", runId],
    queryFn: () => apiRequest<RunSnapshot>(`/api/runs/${runId}/snapshot`),
    enabled: !!runId,
    retry: false,
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
      evaluator_config?: Record<string, unknown>;
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

export function useBenchmarkSuites(projectId: string) {
  return useQuery({
    queryKey: ["benchmarkSuites", projectId],
    queryFn: () => apiRequest<BenchmarkSuite[]>(benchmarkApiPaths.suites(projectId)),
    enabled: !!projectId,
  });
}

export function useCreateBenchmarkSuite(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateBenchmarkSuiteInput) =>
      apiRequest<BenchmarkSuite>(benchmarkApiPaths.suites(projectId), {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: (suite) => {
      qc.setQueryData<BenchmarkSuite[]>(["benchmarkSuites", projectId], (current = []) => [
        ...current.filter((item) => item.id !== suite.id),
        suite,
      ]);
      qc.invalidateQueries({ queryKey: ["benchmarkSuites", projectId] });
    },
  });
}

export function useBenchmarkTargets(projectId: string) {
  return useQuery({
    queryKey: ["benchmarkTargets", projectId],
    queryFn: () => apiRequest<BenchmarkTarget[]>(benchmarkApiPaths.targets(projectId)),
    enabled: !!projectId,
  });
}

export function useBenchmarkPreflight(projectId: string) {
  return useMutation({
    mutationFn: (body: BenchmarkPreflightInput) =>
      apiRequest<BenchmarkPreflight>(benchmarkApiPaths.preflight(projectId), {
        method: "POST",
        body: JSON.stringify(body),
      }),
  });
}

export function useCreateBenchmarkExperiment(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateBenchmarkExperimentInput) =>
      apiRequest<Experiment>(benchmarkApiPaths.experiments(projectId), {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["experiments", projectId] });
      qc.invalidateQueries({ queryKey: ["benchmarkSuites", projectId] });
    },
  });
}

const LIVE_BENCHMARK_EXPERIMENT_STATUSES = new Set([
  "queued",
  "running",
  "pending",
  "retrying",
]);

const LIVE_BENCHMARK_LAUNCH_STATUSES = new Set([
  "starting",
  "launching",
  "running",
]);

export function getBenchmarkReportRefetchInterval(
  experimentStatus?: string,
  launchStatus?: string,
): 2500 | false {
  if (experimentStatus && LIVE_BENCHMARK_EXPERIMENT_STATUSES.has(experimentStatus.toLowerCase())) {
    return 2500;
  }
  if (launchStatus && LIVE_BENCHMARK_LAUNCH_STATUSES.has(launchStatus.toLowerCase())) {
    return 2500;
  }
  return false;
}

export function useLaunchBenchmarkExperiment(projectId: string, experimentId: string) {
  const qc = useQueryClient();
  const queryKey = ["benchmarkExperimentReport", projectId, experimentId];
  return useMutation({
    mutationFn: async () => {
      const response = await apiRequest<BenchmarkLaunchResponse>(benchmarkApiPaths.launch(projectId, experimentId), {
        method: "POST",
      });
      return response.launch;
    },
    onSuccess: (launch) => {
      qc.setQueryData<BenchmarkExperimentReport>(queryKey, (current) => (
        current ? { ...current, launch } : current
      ));
      qc.invalidateQueries({ queryKey });
    },
  });
}

export function useBenchmarkExperimentReport(projectId: string, experimentId: string) {
  return useQuery({
    queryKey: ["benchmarkExperimentReport", projectId, experimentId],
    queryFn: () =>
      apiRequest<BenchmarkExperimentReport>(benchmarkApiPaths.report(projectId, experimentId)),
    enabled: !!projectId && !!experimentId,
    refetchInterval: (query) => getBenchmarkReportRefetchInterval(
      query.state.data?.experiment.status,
      query.state.data?.launch?.status,
    ),
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
    refetchInterval: 10_000,
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
export function useActiveRuns(
  projectId: string,
  agentId?: string,
  realtimeStatus?: RealtimeStatus,
) {
  const params = new URLSearchParams({ limit: "50" });
  if (agentId) params.set("agent_id", agentId);
  return useQuery({
    queryKey: ["activeRuns", projectId, agentId],
    queryFn: () =>
      apiRequest<ActiveRun[]>(
        `/api/projects/${projectId}/active-runs?${params.toString()}`,
      ),
    enabled: !!projectId,
    refetchInterval: realtimeStatus === "live" ? false : 3000,
  });
}

export function useRunRuntimeDetail(
  projectId: string,
  runId: string,
  realtimeStatus?: RealtimeStatus,
) {
  return useQuery({
    queryKey: ["runRuntimeDetail", projectId, runId],
    queryFn: () =>
      apiRequest<RunRuntimeDetail>(
        `/api/projects/${projectId}/runs/${runId}/runtime`,
      ),
    enabled: !!projectId && !!runId,
    refetchInterval: (query) => {
      if (realtimeStatus === "live") return false;
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

export function useRubricTemplates(projectId: string) {
  return useQuery({
    queryKey: ["rubricTemplates", projectId],
    queryFn: () =>
      apiRequest<RubricTemplate[]>(`/api/projects/${projectId}/rubrics`),
    enabled: !!projectId,
  });
}

export function useCreateRubricTemplate(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; description?: string }) =>
      apiRequest<RubricTemplate>(`/api/projects/${projectId}/rubrics`, {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: ["rubricTemplates", projectId] }),
  });
}

export function useRubricVersions(projectId: string, templateId: string) {
  return useQuery({
    queryKey: ["rubricVersions", projectId, templateId],
    queryFn: () =>
      apiRequest<RubricTemplateVersion[]>(
        `/api/projects/${projectId}/rubrics/${templateId}/versions`,
      ),
    enabled: !!projectId && !!templateId,
  });
}

export function useCreateRubricVersion(projectId: string, templateId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rubric: Record<string, unknown>) =>
      apiRequest<RubricTemplateVersion>(
        `/api/projects/${projectId}/rubrics/${templateId}/versions`,
        { method: "POST", body: JSON.stringify({ rubric }) },
      ),
    onSuccess: () =>
      qc.invalidateQueries({
        queryKey: ["rubricVersions", projectId, templateId],
      }),
  });
}

export function usePublishRubricVersion(projectId: string, templateId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (versionId: string) =>
      apiRequest<RubricTemplateVersion>(
        `/api/projects/${projectId}/rubric-versions/${versionId}/publish`,
        { method: "POST" },
      ),
    onSuccess: () =>
      qc.invalidateQueries({
        queryKey: ["rubricVersions", projectId, templateId],
      }),
  });
}

export function useJudgeEvaluation(projectId: string, runId: string) {
  return useQuery({
    queryKey: ["judgeEvaluation", projectId, runId],
    queryFn: () =>
      apiRequest<JudgeEvaluation>(
        `/api/projects/${projectId}/runs/${runId}/judge-evaluation`,
      ),
    enabled: !!projectId && !!runId,
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
      apiRequest<SamplingRule[]>(`/api/projects/${projectId}/sampling-rules`),
    enabled: !!projectId,
  });
}



export function useCreateSamplingRule(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: {
      name: string;
      sample_rate: number;
      filters?: string;
      agent_id?: string;
      strategy?: SamplingRule["strategy"];
      daily_budget?: number;
      stratification?: string;
    }) =>
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

export function useProjectAnnotationItems(projectId: string) {
  return useQuery({
    queryKey: ["annotationItems", "project", projectId],
    queryFn: () =>
      apiRequest<AnnotationItem[]>(`/api/projects/${projectId}/annotation-items`),
    enabled: !!projectId,
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

export function useSubmitAnnotationGold() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ itemId, goldScores }: { itemId: string; goldScores: Record<string, number> }) =>
      apiRequest<AnnotationItem>(`/api/annotation-items/${itemId}/gold`, {
        method: "POST",
        body: JSON.stringify({ gold_scores: goldScores }),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["annotationItems"] }),
  });
}

export function useEvalCalibrations(projectId: string) {
  return useQuery({
    queryKey: ["evalCalibrations", projectId],
    queryFn: () =>
      apiRequest<CalibrationRun[]>(`/api/projects/${projectId}/eval-calibrations`),
    enabled: !!projectId,
  });
}

export function useRunEvalCalibration(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: {
      rubric_version_id: string;
      sample_run_ids: string[];
      pass_threshold: number;
    }) =>
      apiRequest<CalibrationRun>(`/api/projects/${projectId}/eval-calibrations`, {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["evalCalibrations", projectId] }),
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

export function useProjectIngestHealthEvents(projectId: string, minutes: number = 15) {
  return useQuery({
    queryKey: ['ingestHealthEvents', projectId, minutes],
    queryFn: () => apiRequest<IngestHealthEventItem[]>(`/api/projects/${projectId}/ingest-stats/events?minutes=${minutes}&limit=20`),
    enabled: !!projectId,
    refetchInterval: 30_000,
  });
}

export function useProjectQuota(projectId: string) {
  return useQuery({
    queryKey: ["projectQuota", projectId],
    queryFn: () =>
      apiRequest<ProjectQuotaStatus>(`/api/projects/${projectId}/quotas`),
    enabled: !!projectId,
    refetchInterval: 15_000,
  });
}

export function useUpdateProjectQuota(projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (quota: Omit<ProjectQuota, "project_id" | "version" | "updated_at">) =>
      apiRequest<ProjectQuota>(`/api/projects/${projectId}/quotas`, {
        method: "PUT",
        body: JSON.stringify(quota),
      }),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["projectQuota", projectId] }),
  });
}

export function useProjectAnomalies(projectId: string) {
  return useQuery({
    queryKey: ["projectAnomalies", projectId],
    queryFn: () =>
      apiRequest<OperationalAnomaly[]>(`/api/projects/${projectId}/anomalies?limit=100`),
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

// ---- Phase 4.2: Project realtime stream ----

export function useProjectRealtime(projectId: string): RealtimeStatus {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<RealtimeStatus>(
    projectId ? "connecting" : "closed",
  );

  useEffect(() => {
    if (!projectId) {
      setStatus("closed");
      return;
    }

    return subscribeProjectEvents(projectId, {
      onStatus: setStatus,
      onReset: () => {
        void queryClient.invalidateQueries();
      },
      onEvent: (event) => {
        invalidateRealtimeQueries(queryClient, event);
      },
    });
  }, [projectId, queryClient]);

  return status;
}

function invalidateRealtimeQueries(
  queryClient: QueryClient,
  event: RealtimeEvent,
): void {
  const invalidations: Array<readonly unknown[]> = [];

  if (event.type.startsWith("agent.")) {
    invalidations.push(
      ["agents", event.project_id],
      ["agentSummaries", event.project_id],
    );
    if (event.agent_id) {
      invalidations.push(["agent", event.agent_id]);
    }
  }

  if (event.type.startsWith("run.")) {
    invalidations.push(
      ["activeRuns", event.project_id],
      ["runs", event.project_id],
    );
    if (event.run_id) {
      invalidations.push(
        ["run", event.run_id],
        ["runRuntimeDetail", event.project_id, event.run_id],
        ["evalScores", event.project_id, event.run_id],
      );
    }
  }

  if (event.type.startsWith("ingest.") || event.type.startsWith("quota.")) {
    invalidations.push(
      ["ingestStats", event.project_id],
      ["ingestAgentStats", event.project_id],
      ["ingestHealthEvents", event.project_id],
    );
  }

  if (event.type === "eval.completed") {
    invalidations.push(
      ["experiments", event.project_id],
      ["experimentResults", event.project_id],
      ["experimentReport", event.project_id],
      ["experimentEvalScores", event.project_id],
    );
  }

  for (const queryKey of invalidations) {
    void queryClient.invalidateQueries({ queryKey });
  }
}

export function useBrowserSessions() {
  return useQuery({
    queryKey: ["browser-sessions"],
    queryFn: () => apiRequest<import("./types").BrowserSession[]>("/api/auth/sessions"),
  });
}

export function useRevokeBrowserSession() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (sessionId: string) => apiRequest<void>(`/api/auth/sessions/${sessionId}`, { method: "DELETE" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["browser-sessions"] }),
  });
}

export function useOnlineEvalJobs(projectId: string, runId?: string) {
  return useQuery({
    queryKey: ["onlineEvalJobs", projectId, runId ?? ""],
    queryFn: () => apiRequest<OnlineEvalJob[]>(`/api/projects/${projectId}/online-eval-jobs${runId ? `?run_id=${encodeURIComponent(runId)}` : ""}`),
    enabled: !!projectId,
    refetchInterval: (query) => {
      const jobs = query.state.data ?? [];
      return jobs.some((job) => ["queued", "sampling", "running", "retry_wait"].includes(job.status)) ? 2500 : false;
    },
  });
}

export function useRetryOnlineEvalJob(projectId: string, runId?: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (jobId: string) => apiRequest<OnlineEvalJob>(`/api/projects/${projectId}/online-eval-jobs/${jobId}/retry`, { method: "POST" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["onlineEvalJobs", projectId, runId ?? ""] }),
  });
}
