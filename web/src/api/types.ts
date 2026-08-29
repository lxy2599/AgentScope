// ---- Auth ----
export interface User {
  id: string;
  email: string;
  display_name: string;
}

export interface BrowserSession {
  id: string;
  created_at: string;
  last_used_at?: string;
  expires_at: string;
  user_agent_summary?: string;
  ip_prefix?: string;
  current: boolean;
}

export type OnlineEvalJobStatus =
  | "queued"
  | "sampling"
  | "running"
  | "retry_wait"
  | "succeeded"
  | "skipped"
  | "failed"
  | "dead_letter";
export interface OnlineEvalJob {
  id: string;
  project_id: string;
  run_id: string;
  trace_id: string;
  evaluator_version: string;
  status: OnlineEvalJobStatus;
  sampling_rule_id?: string;
  sampling_reason?: string;
  evaluation_source?: "trace_heuristic" | "baseline_fallback" | string;
  fallback_reason?: string;
  attempt_count: number;
  max_attempts: number;
  available_at: string;
  claimed_by?: string;
  claimed_at?: string;
  started_at?: string;
  finished_at?: string;
  last_error?: string;
  annotation_item_id?: string;
  created_at: string;
  updated_at: string;
}

// ---- Project ----
export interface Project {
  id: string;
  name: string;
  description?: string;
  created_at?: string;
}

export type ProjectTokenStatus = "active" | "revoked" | "expired";
export type ProjectTokenEnvironment =
  "development" | "staging" | "production" | "ci" | "local";
export type ProjectTokenType =
  "trace_sdk" | "runner" | "agent_runtime" | "ci" | "custom";

export interface ProjectToken {
  id: string;
  project_id: string;
  token_prefix: string;
  scopes: string[];
  description?: string;
  created_at?: string;
  expires_at?: string;
  last_used_at?: string;
  created_by?: string;
  revoked_at?: string;
  revoked_by?: string;
  revocation_reason?: string;
  rotates_token_id?: string;
  environment: ProjectTokenEnvironment;
  token_type: ProjectTokenType;
  last_used_endpoint?: string;
  last_used_agent_id?: string;
  last_used_instance_id?: string;
  status: ProjectTokenStatus;
}
export interface CreateProjectTokenInput {
  description: string;
  scopes: string[];
  expires_at?: string;
  environment: ProjectTokenEnvironment;
  token_type: ProjectTokenType;
}
export interface CreateTokenResponse {
  token: ProjectToken;
  plain_token: string;
}
export interface ProjectTokenAuditEvent {
  id: string;
  project_id: string;
  token_id: string;
  actor_user_id?: string;
  event_type: string;
  metadata: Record<string, unknown>;
  created_at: string;
}
export interface ProjectTokenPolicy {
  project_id: string;
  allow_non_expiring: boolean;
  max_production_ttl_days?: number;
  max_ci_ttl_days?: number;
  expiry_warning_days: number[];
  unused_warning_days: number;
  updated_by?: string;
  updated_at?: string;
}
export interface ProjectTokenRiskSummary {
  expiring_soon: number;
  unused: number;
  broad_production: number;
  unclassified_environment: number;
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
  input: unknown;
  expected?: unknown;
  metadata?: unknown;
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
  cache_read_tokens?: number;
  cache_write_tokens?: number;
  ttft_ms?: number;
  cost?: number;
  estimated_cost?: number;
  actual_cost?: number;
  cost_source?: string;
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
  aggregate_score?: number;
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
  benchmark_suite_id?: string;
  benchmark_key?: string;
  benchmark_version?: string;
  /** Raw benchmark config may be returned by legacy endpoints; report endpoints use benchmark_config_snapshot instead. */
  benchmark_config?: Record<string, unknown> | string;
  trials_per_case?: number;
  evaluation_snapshot_id?: string;
  evaluation_snapshot_hash?: string;
}

/** Public, identity-only projection of a persisted benchmark_config snapshot.
 * Profile configs and execution overrides are deliberately never returned. */
export interface BenchmarkExperimentArtifactSnapshot {
  id?: string;
  framework?: string;
  image?: string;
  image_digest?: string;
  source_commit?: string;
}

export interface BenchmarkExperimentProfileSnapshot {
  id?: string;
  framework?: string;
}

export interface BenchmarkExperimentProfileSnapshots {
  model?: BenchmarkExperimentProfileSnapshot;
  agent?: BenchmarkExperimentProfileSnapshot;
  execution?: BenchmarkExperimentProfileSnapshot;
}

export interface BenchmarkExperimentTaskSelectionSnapshot {
  mode?: string;
  include?: string[];
  exclude?: string[];
  count?: number;
  seed?: number;
  source_experiment?: string;
}

export interface BenchmarkExperimentConfigSnapshot {
  schema_version?: string;
  catalog_revision?: string;
  run_template_id?: string;
  trials_per_case?: number;
  agent_artifact?: BenchmarkExperimentArtifactSnapshot;
  profiles?: BenchmarkExperimentProfileSnapshots;
  task_selection?: BenchmarkExperimentTaskSelectionSnapshot;
  selected_case_ids?: string[];
}

export interface ExperimentDetail extends Experiment {
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
  case_input?: string;
  case_expected?: string;
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

// ---- Benchmark evaluation contracts ----
export type BenchmarkExecutionKind =
  "function" | "harbor" | "tau2" | "browsergym" | "inspect" | string;

export interface BenchmarkSuite {
  id: string;
  project_id: string;
  dataset_id: string;
  benchmark_key: string;
  name: string;
  description: string;
  version: string;
  harness_kind: string;
  execution_kind: BenchmarkExecutionKind;
  source_uri?: string;
  source_commit?: string;
  default_execution_config?: Record<string, unknown>;
  required_capabilities?: string[];
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at?: string;
}

export interface CreateBenchmarkSuiteInput {
  dataset_id: string;
  benchmark_key: string;
  name: string;
  description?: string;
  version: string;
  harness_kind: string;
  execution_kind: BenchmarkExecutionKind;
  source_uri?: string;
  source_commit?: string;
  default_execution_config: Record<string, unknown>;
  required_capabilities: string[];
}

export interface BenchmarkTarget {
  name: string;
  version?: string;
  description?: string;
  framework?: string;
  status?: string;
  capabilities?: string[];
  metadata?: Record<string, unknown>;
}

export interface BenchmarkAgentArtifact {
  id: string;
  framework: string;
  image: string;
  image_digest: string;
  source_commit?: string;
  task_runner_protocol: string;
  capabilities?: string[];
  display_name?: string;
}

export interface BenchmarkProfile {
  id: string;
  kind: "model" | "agent" | "execution" | string;
  framework?: string;
  version?: string;
  display_name?: string;
  config: Record<string, unknown>;
}

export interface BenchmarkRunTemplate {
  id: string;
  display_name?: string;
  suite_id: string;
  agent_artifact_id: string;
  model_profile_id: string;
  agent_profile_id: string;
  execution_profile_id: string;
  task_selection: Record<string, unknown>;
  execution_overrides?: Record<string, unknown>;
  trials_per_case: number;
}

export interface BenchmarkCatalog {
  revision: string;
  artifacts: BenchmarkAgentArtifact[];
  profiles: BenchmarkProfile[];
  run_templates: BenchmarkRunTemplate[];
}

export interface BenchmarkPreflightInput {
  /** Preferred immutable catalog launch input. */
  run_template_id?: string;
  /** Legacy suite/target launch fields remain supported for manually registered suites. */
  suite_id?: string;
  target_name?: string;
  target_version?: string;
  trials_per_case?: number;
  case_ids?: string[];
  benchmark_config?: Record<string, unknown>;
}

export interface BenchmarkWorkerAvailability {
  project_id?: string;
  execution_kind: BenchmarkExecutionKind;
  available: boolean;
  state: string;
  reason?: string;
  blockers?: string[];
  registered_workers: number;
  matching_workers: number;
  advertised_capacity: number;
}

export interface BenchmarkPreflight {
  compatible: boolean;
  execution_kind: BenchmarkExecutionKind;
  estimated_jobs: number;
  estimated_tasks?: number;
  runner_available?: boolean;
  /** Retained JSON name for backwards compatibility; it reports external worker availability. */
  runner?: BenchmarkWorkerAvailability;
  catalog_revision?: string;
  run_template_id?: string;
  agent_artifact_id?: string;
  matched_capabilities?: string[];
  missing_capabilities?: string[];
  warnings?: string[];
  blockers?: string[];
}

export interface CreateBenchmarkExperimentInput extends BenchmarkPreflightInput {
  case_ids?: string[];
}

export interface BenchmarkArtifact {
  name: string;
  kind?: string;
  url?: string;
  uri?: string;
  size_bytes?: number;
}

export interface BenchmarkTrialResult {
  id: string;
  eval_job_id?: string;
  dataset_case_id: string;
  task_id?: string;
  trial_index: number;
  seed?: number;
  status: string;
  passed?: boolean;
  valid?: boolean;
  official_score?: number;
  official_metrics?: Record<string, number | string | boolean | null>;
  run_id?: string;
  latency_ms?: number;
  cost?: number;
  error_type?: string;
  error_message?: string;
  question?: string;
  answer?: string;
  artifacts?: BenchmarkArtifact[];
}

export interface BenchmarkExperimentExecution {
  queued: number;
  running: number;
  succeeded: number;
  failed: number;
  total_jobs?: number;
}

export interface BenchmarkExperimentQuality {
  official_score?: number;
  pass_at_1?: number;
  pass_at_k?: number;
  valid_trials?: number;
  invalid_trials?: number;
  completed_trials?: number;
  total_trials?: number;
  failed_trials?: number;
}

export interface BenchmarkExperimentReportEfficiency {
  latency_samples?: number;
  avg_latency_ms?: number;
  p95_latency_ms?: number;
  cost_samples?: number;
  total_cost?: number;
  avg_cost?: number;
}

export type BenchmarkLaunchStatus =
  | "not_started"
  | "ready"
  | "starting"
  | "launching"
  | "running"
  | "completed"
  | "failed"
  | "unavailable"
  | "queued"
  | string;

/**
 * Queue-only benchmark launch result. AgentScope does not start a local
 * subprocess; availability is reported from registered external workers.
 */
export interface BenchmarkExperimentLaunch {
  available: boolean;
  status: BenchmarkLaunchStatus;
  message?: string;
  unavailable_reason?: string;
  execution_kind?: BenchmarkExecutionKind;
  registered_workers?: number;
  matching_workers?: number;
  advertised_capacity?: number;
  /** Deprecated former local-launch fields; queue-only responses leave them empty. */
  runner_name?: string;
  pid?: number;
  requested_at?: string;
  started_at?: string;
}

export interface BenchmarkLaunchResponse {
  experiment_id: string;
  launch: BenchmarkExperimentLaunch;
}

export interface BenchmarkCancellationResult {
  experiment_id: string;
  status: string;
  cancelled_jobs: number;
  audit_events: number;
}

export interface BenchmarkCancellationAuditEvent {
  id: string;
  project_id: string;
  experiment_id: string;
  eval_job_id: string;
  actor_user_id: string;
  reason: string;
  previous_status: string;
  created_at: string;
}

export interface BenchmarkExperimentReport {
  experiment: ExperimentDetail;
  suite: BenchmarkSuite;
  execution: BenchmarkExperimentExecution;
  quality: BenchmarkExperimentQuality;
  launch?: BenchmarkExperimentLaunch;
  official_metrics?: Record<string, number | string | boolean | null>;
  efficiency?: BenchmarkExperimentReportEfficiency;
  failure_attribution?: Record<string, number>;
  trials: BenchmarkTrialResult[];
  /** Safe projection of the immutable Run Template execution snapshot. */
  benchmark_config_snapshot?: BenchmarkExperimentConfigSnapshot;
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
  "tool" | "skill" | "knowledge_base" | "memory" | "guardrail" | "model";

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
  project_id: string;
  instance_id: string;
  instance_name: string;
  status: string;
  connection_status: "online" | "offline" | "unknown";
  hostname: string;
  pid: number;
  active_run_id?: string;
  metadata?: Record<string, unknown>;
  started_at: string;
  last_heartbeat_at: string;
}

export interface AgentRuntimeSummary {
  total_instance_count: number;
  online_instance_count: number;
  offline_instance_count: number;
  last_heartbeat_at?: string;
  connection_status: "online" | "offline" | "unknown";
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
  metadata?: AgentRuntimeMetadata;
  created_at: string;
  updated_at: string;
  runtime_summary: AgentRuntimeSummary;
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
  evaluation_status?: string;
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
  evaluation_status?: string;
  evidence_status?: string;
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
  | "queued"
  | "running"
  | "thinking"
  | "waiting_llm"
  | "waiting_tool"
  | "waiting_retrieval"
  | "waiting_memory"
  | "waiting_user"
  | "retrying"
  | "completed"
  | "failed"
  | "timeout"
  | "cancelled";

export interface ActiveRun {
  id: string;
  project_id: string;
  status: RunRuntimeStatus;
  input_summary?: string;
  output_summary?: string;
  error?: string;
  trace_id?: string;
  latency_ms?: number;
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
  input?: string;
  output?: string;
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
  status:
    "healthy" | "no_traffic" | "degraded" | "storage_error" | "unavailable";
  accepted: number;
  rejected: number;
  storage_failed: number;
  missing_run_id: number;
  missing_trace_id: number;
  last_success_at?: string;
  last_failure_at?: string;
  last_error_code?: string;
  sdk_queue_depth?: number;
  sdk_queue_capacity?: number;
  sdk_spans_dropped?: number;
  sdk_export_retries?: number;
  sdk_export_latency_ms?: number;
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

export type QuotaDimension =
  | "ingest_requests"
  | "ingest_spans"
  | "ingest_bytes"
  | "sse_connections"
  | "tokens_daily"
  | "cost_micros_daily";

export interface ProjectQuota {
  project_id: string;
  ingest_requests_per_minute: number;
  ingest_spans_per_minute: number;
  ingest_bytes_per_minute: number;
  sse_connections: number;
  tokens_per_day: number;
  cost_micros_per_day: number;
  version: number;
  updated_at?: string;
}

export interface QuotaEvent {
  id: string;
  project_id: string;
  dimension: QuotaDimension;
  requested: number;
  limit: number;
  remaining: number;
  outcome: "allowed" | "rejected" | "degraded";
  fail_mode: "fail_open" | "fail_closed";
  reason: string;
  occurred_at: string;
}

export interface ProjectQuotaStatus {
  quota: ProjectQuota;
  usage: Partial<Record<QuotaDimension, number>>;
  events: QuotaEvent[];
  can_edit: boolean;
}

export type OperationalAnomalyStatus =
  "normal" | "anomaly" | "insufficient_baseline";

export type OperationalMetric =
  | "run_latency_ms"
  | "llm_total_tokens"
  | "llm_cost"
  | "llm_ttft_ms"
  | "tool_error_rate"
  | "ingest_reject_rate";

export interface OperationalAnomaly {
  id: string;
  group: {
    project_id: string;
    agent_name: string;
    agent_version: string;
    environment: string;
    metric: OperationalMetric;
  };
  window_start: string;
  window_end: string;
  observed_value: number;
  baseline_median: number;
  baseline_mad: number;
  deviation_score: number;
  sample_count: number;
  baseline_sample_count: number;
  status: OperationalAnomalyStatus;
  first_detected_at: string;
  last_detected_at: string;
}

export interface SamplingRule {
  id: string;
  project_id: string;
  agent_id?: string;
  name: string;
  enabled: boolean;
  sample_rate: number;
  filters?: string;
  strategy:
    | "random"
    | "failure"
    | "high_cost"
    | "low_confidence"
    | "high_disagreement"
    | "combined";
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
  comparison_type: "model_swap" | "ablation" | "general";
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
    conclusion:
      | "significant_regression"
      | "no_significant_change"
      | "insufficient_sample";
    metrics?: Record<
      string,
      {
        mean_delta: number;
        ci_lower: number;
        ci_upper: number;
        p_value: number;
      }
    >;
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
  status: "improved" | "regressed" | "unchanged" | "incomparable";
  baseline_latency_ms?: number;
  candidate_latency_ms?: number;
  baseline_cost?: number;
  candidate_cost?: number;
  case_input?: string;
  case_expected?: string;
  baseline_output?: string;
  baseline_reason?: string;
  candidate_output?: string;
  candidate_reason?: string;
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
