// ---- Auth ----
export interface User {
  id: string;
  email: string;
  display_name: string;
}

// ---- Project ----
export interface Project {
  id: string;
  name: string;
  description?: string;
  created_at?: string;
}

export interface ProjectToken {
  id: string;
  project_id: string;
  token_prefix: string;
  scopes: string[];
  description?: string;
}

export interface CreateTokenResponse {
  token: ProjectToken;
  plain_token: string;
}

// ---- Dataset ----
export interface Dataset {
  id: string;
  project_id: string;
  name: string;
  description?: string;
  created_at: string;
}

export interface DatasetCase {
  id: string;
  dataset_id: string;
  input: string;
  expected?: string;
  metadata?: string;
  sort_order: number;
  created_at: string;
}

// ---- Run ----
export interface RunSummary {
  id: string;
  project_id: string;
  source: string;
  status: string;
  trace_id: string;
  agent_name: string;
  created_at: string;
}

export interface Run {
  id: string;
  project_id: string;
  source: string;
  status: string;
  trace_id: string;
  agent_name: string;
  agent_version?: string;
  input_summary?: string;
  output_summary?: string;
  error?: string;
  started_at?: string;
  ended_at?: string;
  latency_ms?: number;
  created_at: string;
}

export interface TraceSpan {
  run_id: string;
  span_id: string;
  trace_id: string;
  project_id: string;
  parent_span_id?: string;
  name: string;
  kind?: string;
  status_code?: string;
  status_message?: string;
  start_time: string;
  end_time?: string;
  duration_ms?: number;
  attributes?: string;
  truncated: boolean;
}

export interface TraceSpanEvent {
  project_id: string;
  run_id: string;
  trace_id: string;
  span_id: string;
  event_name: string;
  event_time: string;
  attributes?: string;
  truncated: boolean;
}

export interface RunDetail {
  run: Run;
  spans: TraceSpan[];
  events: TraceSpanEvent[];
  llm_calls?: LLMCallRecord[];
  tool_calls?: ToolCallRecord[];
  retrieval_calls?: RetrievalCallRecord[];
  skill_calls?: SkillCallRecord[];
  thoughts?: ThoughtRecord[];
  final_answers?: FinalAnswerRecord[];
}

// ---- Deep Trace (Phase 2.3) ----
export interface LLMCallRecord {
  project_id: string;
  run_id: string;
  trace_id: string;
  span_id: string;
  agent_id?: string;
  agent_instance_id?: string;
  provider: string;
  model: string;
  input_tokens?: number;
  output_tokens?: number;
  total_tokens?: number;
  cost?: number;
  input_summary?: string;
  output_summary?: string;
  content_captured: boolean;
  status: string;
  error?: string;
  start_time: string;
  duration_ms?: number;
}

export interface ToolCallRecord {
  project_id: string;
  run_id: string;
  trace_id: string;
  span_id: string;
  agent_id?: string;
  agent_instance_id?: string;
  tool_name: string;
  tool_type?: string;
  arguments_summary?: string;
  result_summary?: string;
  success: boolean;
  error?: string;
  retry_count: number;
  side_effect_risk?: string;
  start_time: string;
  duration_ms?: number;
}

export interface RetrievalCallRecord {
  project_id: string;
  run_id: string;
  trace_id: string;
  span_id: string;
  agent_id?: string;
  agent_instance_id?: string;
  knowledge_base: string;
  query_text: string;
  top_k?: number;
  returned_count?: number;
  document_ids?: string[];
  scores?: number[];
  start_time: string;
  duration_ms?: number;
}

export interface SkillCallRecord {
  project_id: string;
  run_id: string;
  trace_id: string;
  span_id: string;
  agent_id?: string;
  agent_instance_id?: string;
  skill_name: string;
  skill_version?: string;
  input_summary?: string;
  output_summary?: string;
  success: boolean;
  error?: string;
  start_time: string;
  duration_ms?: number;
}

// ---- Agent Semantic Traces (Phase 2.8) ----
export interface ThoughtRecord {
  project_id: string;
  run_id: string;
  trace_id: string;
  span_id: string;
  agent_id?: string;
  agent_instance_id?: string;
  stage?: string;
  summary: string;
  content?: string;
  confidence?: number;
  content_captured: boolean;
  start_time: string;
  duration_ms?: number;
}

export interface FinalAnswerRecord {
  project_id: string;
  run_id: string;
  trace_id: string;
  span_id: string;
  answer_summary: string;
  answer_output?: string;
  structured_output?: string;
  start_time: string;
  duration_ms?: number;
}

// ---- Experiment ----
export interface Experiment {
  id: string;
  project_id: string;
  dataset_id: string;
  evaluator_id?: string;
  target_name: string;
  target_version?: string;
  status: string;
  total_cases: number;
  passed_cases: number;
  failed_cases: number;
  created_at: string;
  model_provider?: string;
  model_name?: string;
  model_version?: string;
  prompt_version?: string;
  harness_version?: string;
  dataset_version?: string;
  toolset_version?: string;
  skillset_version?: string;
  rag_config_version?: string;
  feature_flags?: Record<string, unknown>;
}

export interface ExperimentDetail {
  id: string;
  project_id: string;
  dataset_id: string;
  evaluator_id?: string;
  target_name: string;
  target_version?: string;
  status: string;
  total_cases: number;
  passed_cases: number;
  failed_cases: number;
  created_at: string;
  status_counts: Record<string, number>;
}

// ---- Experiment Report (from /report endpoint) ----
export interface ExperimentReportQuality {
  avg_score: number;
  pass_threshold: number;
  pass_count: number;
  fail_count: number;
  total_evaluated: number;
}

export interface ExperimentReportExecution {
  succeeded: number;
  failed: number;
  running: number;
  queued: number;
}

export interface ExperimentReportResult {
  id: string;
  dataset_case_id: string;
  run_id?: string;
  score: number;
  passed: boolean;
  reason: string;
  actual_output?: string;
  latency_ms?: number;
  eval_scores?: EvalScore[];
}

export interface ExperimentReport {
  experiment: ExperimentDetail;
  status: string;
  total_cases: number;
  execution: ExperimentReportExecution;
  quality: ExperimentReportQuality;
  dimension_aggregation: MetricKindAggregation[];
  results: ExperimentReportResult[];
}

// ---- Agent ----
export interface AgentInfo {
  id: string;
  project_id: string;
  name: string;
  description: string;
  status: string;
  version: number;
  metadata?: AgentRuntimeMetadata;
  created_at: string;
  updated_at: string;
}

export type AgentCapabilityKind =
  | "tool"
  | "skill"
  | "knowledge_base"
  | "memory"
  | "guardrail"
  | "model";

export interface AgentRuntimeMetadata {
  contract_version?: string;
  agent_version?: string;
  framework?: string;
  runtime?: string;
  source?: string;
  environment?: string;
  model_provider?: string;
  model_name?: string;
  harness_version?: string;
  prompt_version?: string;
  tags?: string[];
}

export interface AgentInstanceInfo {
  id: string;
  agent_id: string;
  instance_id: string;
  status: string;
  hostname: string;
  pid: number;
  metadata?: Record<string, unknown>;
  started_at: string;
  last_heartbeat_at: string;
}

export interface CapabilityMetadata {
  contract_version?: string;
  kind?: AgentCapabilityKind;
  version?: string;
  schema?: Record<string, unknown>;
  side_effect_risk?: string;
}

export interface AgentCapabilityInfo {
  id: string;
  agent_id: string;
  name: string;
  description: string;
  metadata?: CapabilityMetadata;
  created_at: string;
}

export interface AgentDetail {
  id: string;
  project_id: string;
  name: string;
  description: string;
  status: string;
  version: number;
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  instances: AgentInstanceInfo[];
  capabilities: AgentCapabilityInfo[];
}

// Agent control center summary
export interface AgentSummary {
  id: string;
  project_id: string;
  name: string;
  description: string;
  status: string;
  version: number;
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  // Runtime contract summary
  framework?: string;
  runtime?: string;
  source?: string;
  environment?: string;
  agent_version?: string;
  model_provider?: string;
  model_name?: string;
  harness_version?: string;
  prompt_version?: string;
  // Aggregated metrics
  instance_count: number;
  online_instance_count: number;
  active_run_count: number;
  recent_run_count: number;
  recent_error_count: number;
  last_heartbeat_at?: string;
  last_run_at?: string;
  p95_latency_ms?: number;
  error_rate: number;
  total_cost?: number;
  // Derived status
  current_status_summary: string;
}

// RecentRunType is a lightweight run record for recent errors display.
export interface RecentRunType {
  id: string;
  project_id: string;
  status: string;
  input_summary?: string;
  error?: string;
  trace_id?: string;
  latency_ms?: number;
  agent_name?: string;
  created_at: string;
  updated_at: string;
}

// ---- Eval Case Result ----
export interface EvalCaseResult {
  id: string;
  eval_job_id: string;
  run_id?: string;
  dataset_case_id: string;
  score: number;
  passed: boolean;
  reason: string;
  details?: string;
  actual_output?: string;
  latency_ms?: number;
  cost?: number;
  created_at: string;
  /** Per-case eval_scores from the report endpoint (optional, backward compatible) */
  eval_scores?: EvalScore[];
}

// ---- Multi-dimensional evaluation scores (Phase 2.5) ----
export interface EvalScore {
  id: string;
  project_id: string;
  run_id: string;
  trace_id: string;
  span_id?: string;
  metric_name: string;
  metric_kind: string;
  score: number;
  passed: boolean;
  reason: string;
  details?: string;
  evaluator_run_id?: string;
  created_at: string;
}

export interface MetricKindAggregation {
  metric_kind: string;
  total_count: number;
  passed_count: number;
  avg_score: number;
}

export interface MetricDefinition {
  id: string;
  project_id: string;
  name: string;
  kind: string;
  config?: string;
  created_at: string;
}

export interface RubricTemplate {
  id: string;
  project_id: string;
  name: string;
  description: string;
  created_at: string;
  updated_at: string;
}

export interface RubricTemplateVersion {
  id: string;
  template_id: string;
  version: number;
  status: "draft" | "published";
  rubric: string;
  created_by?: string;
  created_at: string;
  published_at?: string;
}

export interface JudgeVote {
  id: string;
  provider: string;
  model: string;
  prompt_version: string;
  score?: number;
  passed?: boolean;
  reason: string;
  error?: string;
  latency_ms?: number;
  input_tokens?: number;
  output_tokens?: number;
  cost?: number;
  veto_triggered: boolean;
  veto_reason?: string;
  created_at: string;
}

export interface JudgeAggregationResult {
  method: "mean" | "median" | "trimmed_mean" | "majority";
  score: number;
  passed: boolean;
  valid_vote_count: number;
  error_vote_count: number;
  required_votes: number;
  stddev: number;
  range: number;
  disagreement: number;
  confidence: number;
  low_confidence: boolean;
  essential_failed: boolean;
  veto_triggered: boolean;
  veto_reason?: string;
}

export interface JudgeAggregation {
  id: string;
  metric_name: string;
  metric_kind: string;
  rubric_version_id?: string;
  result: JudgeAggregationResult;
  created_at: string;
}

export interface JudgeEvaluation {
  votes: JudgeVote[];
  aggregations: JudgeAggregation[];
}

export interface RubricDimension {
  name: string;
  weight: number;
  description?: string;
}

export interface ScoringLevel {
  level: number;
  label: string;
  description?: string;
}

export interface RubricConfig {
  dimensions: RubricDimension[];
  scoring_levels?: ScoringLevel[];
  essential_items?: string[];
  optional_items?: string[];
  pitfall_items?: string[];
  veto_items?: string[];
  examples?: Array<Record<string, unknown>>;
  edge_cases?: string[];
}

export interface MetricDefinitionConfig {
  type?: string;
  weight?: number;
  pass_threshold?: number;
  enabled?: boolean;
  veto?: boolean;
  rubric?: RubricConfig;
  judge_provider?: string;
  judge_model?: string;
  calibration_required?: boolean;
}
export type RunRuntimeStatus =
  | 'queued'
  | 'running'
  | 'thinking'
  | 'waiting_llm'
  | 'waiting_tool'
  | 'waiting_retrieval'
  | 'waiting_memory'
  | 'waiting_user'
  | 'retrying'
  | 'completed'
  | 'failed'
  | 'timeout'
  | 'cancelled';

export interface ActiveRun {
  id: string;
  project_id: string;
  status: RunRuntimeStatus;
  input_summary?: string;
  output_summary?: string;
  error?: string;
  trace_id?: string;
  latency_ms?: string;
  agent_id?: string;
  agent_name?: string;
  agent_instance_id?: string;
  agent_instance_name?: string;
  created_at: string;
  updated_at: string;
  last_state_event_at?: string;
  last_state?: string;
}

export interface RunStateEvent {
  id: string;
  run_id: string;
  from_status?: string;
  to_status: RunRuntimeStatus;
  message?: string;
  created_at: string;
}

export interface RunRuntimeDetail extends ActiveRun {
  agent_instance_id?: string;
  agent_instance_name?: string;
  events: RunStateEvent[];
  // Semantic events and spans from ClickHouse (best-effort)
  thoughts?: ThoughtRecord[];
  llm_calls?: LLMCallRecord[];
  tool_calls?: ToolCallRecord[];
  retrieval_calls?: RetrievalCallRecord[];
  skill_calls?: SkillCallRecord[];
  final_answers?: FinalAnswerRecord[];
  spans?: TraceSpan[];
}

// ---- Phase 2.7: Ingest Health Stats ----

export interface IngestStatsResponse {
  project_id: string;
  minutes: number;
  collected_at: string;
  spans: number;
  events: number;
  errors: number;
  llms: number;
  tools: number;
  retrievals: number;
  skills: number;
  tokens: number;
  cost: number;
  status: 'healthy' | 'no_traffic' | 'degraded' | 'storage_error' | 'unavailable';
  accepted: number;
  rejected: number;
  storage_failed: number;
  missing_run_id: number;
  missing_trace_id: number;
  last_success_at?: string;
  last_failure_at?: string;
  last_error_code?: string;
}

export interface IngestHealthEventItem {
  agent_name: string;
  outcome: string;
  http_status: number;
  span_count: number;
  error_code?: string;
  event_time: string;
}

export interface IngestAgentStatsItem {
  agent_name: string;
  error_count: number;
  run_count: number;
  p95_latency: number;
}


export interface SamplingRule {
  id: string;
  project_id: string;
  agent_id?: string;
  name: string;
  enabled: boolean;
  sample_rate: number;
  filters?: string;
  strategy: "random" | "failure" | "high_cost" | "low_confidence" | "high_disagreement" | "combined";
  daily_budget: number;
  stratification: string;
}

export interface AnnotationQueue {
  id: string;
  project_id: string;
  name: string;
  description?: string;
}

export interface AnnotationItem {
  id: string;
  queue_id: string;
  project_id: string;
  run_id: string;
  trace_id?: string;
  status: string;
  priority: number;
  scores?: string;
  corrected_output?: string;
  expected_tool_path?: string;
  comment?: string;
  agent_name?: string;
  input_summary?: string;
  output_summary?: string;
  error?: string;
  failure_category?: string;
  privacy_reviewed: boolean;
  reviewer_note?: string;
  gold_scores?: string;
  is_gold: boolean;
  annotator_id?: string;
  completed_at?: string;
}

export interface CalibrationDimension {
  sample_count: number;
  mae: number;
  mean_bias: number;
  agreement_rate: number;
  cohens_kappa?: number;
}

export interface CalibrationReport {
  sample_count: number;
  observation_count: number;
  mae: number;
  agreement_rate: number;
  cohens_kappa?: number;
  dimensions: Record<string, CalibrationDimension>;
  warnings?: string[];
}

export interface CalibrationRun {
  id: string;
  project_id: string;
  rubric_version_id: string;
  status: "pending" | "running" | "completed" | "failed";
  sample_run_ids: string[];
  report: CalibrationReport;
  created_by?: string;
  created_at: string;
  completed_at?: string;
}

// ---- Experiment Comparison (Phase 3.6) ----
export interface ExperimentComparison {
  id: string;
  project_id: string;
  baseline_experiment_id: string;
  candidate_experiment_id: string;
  comparison_type: 'model_swap' | 'ablation' | 'general';
  created_by: string;
  status: string;
  summary: string;
  regression_gates: string;
  created_at: string;
  updated_at: string;
}

export interface RegressionGatesConfig {
  pass_rate_threshold: number;
  score_delta: number;
  cost_increase_ratio: number;
  p95_latency_increase_pct: number;
  safety_veto: boolean;
}

export interface MultiDimMetricSummary {
  metric_name: string;
  metric_kind: string;
  baseline_avg: number;
  candidate_avg: number;
  delta: number;
  baseline_passed_count: number;
  candidate_passed_count: number;
  baseline_total_count: number;
  candidate_total_count: number;
}

export interface ComparisonSummary {
  total_cases: number;
  paired_cases: number;
  baseline_pass_rate: number;
  candidate_pass_rate: number;
  baseline_avg_score: number;
  candidate_avg_score: number;
  avg_score_delta: number;
  improved_cases: number;
  regressed_cases: number;
  unchanged_cases: number;
  incomparable_cases: number;
  baseline_p95_latency_ms: number;
  candidate_p95_latency_ms: number;
  latency_delta_pct: number;
  baseline_avg_cost: number;
  candidate_avg_cost: number;
  cost_delta_pct: number;
  baseline_error_rate: number;
  candidate_error_rate: number;
  safety_regressed: boolean;
  regression_triggered: boolean;
  regression_reasons?: string[];
  multi_dim_metrics?: Record<string, MultiDimMetricSummary>;
  statistical_significance?: {
    sample_size: number;
    iterations: number;
    seed: number;
    conclusion: "significant_regression" | "no_significant_change" | "insufficient_sample";
    metrics?: Record<string, {
      mean_delta: number;
      ci_lower: number;
      ci_upper: number;
      p_value: number;
    }>;
  };
}

export interface ComparisonCaseResult {
  id: string;
  comparison_id: string;
  dataset_case_id: string;
  baseline_run_id?: string;
  candidate_run_id?: string;
  baseline_score: number;
  candidate_score: number;
  baseline_passed: boolean;
  candidate_passed: boolean;
  score_diff: number;
  status: 'improved' | 'regressed' | 'unchanged' | 'incomparable';
  baseline_latency_ms?: number;
  candidate_latency_ms?: number;
  baseline_cost?: number;
  candidate_cost?: number;
  created_at: string;
}

export interface ComparisonDetail {
  id: string;
  project_id: string;
  baseline_experiment_id: string;
  candidate_experiment_id: string;
  comparison_type: string;
  created_by: string;
  status: string;
  summary: string;
  regression_gates: string;
  created_at: string;
  updated_at: string;
  baseline_experiment: Experiment;
  candidate_experiment: Experiment;
  comparison_summary: ComparisonSummary;
  case_results: ComparisonCaseResult[];
}

export interface RunSnapshot {
  id: string;
  run_id: string;
  project_id: string;
  schema_version: string;
  canonical_payload: Record<string, unknown>;
  payload_hash: string;
  created_at: string;
}
