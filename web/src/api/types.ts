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

// ---- Auth response ----
export interface AuthResponse {
  user: User;
  session_token: string;
}
