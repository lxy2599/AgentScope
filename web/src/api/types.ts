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
  message?: string;
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
}
