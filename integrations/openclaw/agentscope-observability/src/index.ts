import { context, trace, type Span } from "@opentelemetry/api";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-proto";
import { resourceFromAttributes } from "@opentelemetry/resources";
import { BasicTracerProvider, SimpleSpanProcessor } from "@opentelemetry/sdk-trace-base";
import { randomUUID } from "node:crypto";
import { definePluginEntry, type OpenClawPluginApi } from "openclaw/plugin-sdk/plugin-entry";

type Config = {
  serverUrl: string;
  projectToken: string;
  agentName: string;
  captureContent: boolean;
};

type BenchmarkContext = {
  experimentId?: string;
  evalJobId?: string;
  evalTrialId?: string;
  benchmarkKey?: string;
  benchmarkTaskId?: string;
  trialIndex?: number;
};

type RuntimeRun = { id: string };
type ActiveLlm = {
  span: Span;
  provider: string;
  model: string;
  inputSeen: boolean;
  outputSeen: boolean;
};
type ActiveTool = {
  span: Span;
  toolName: string;
};

class PendingRegistry<T> {
  private readonly queues = new Map<string, T[]>();
  private readonly pending: T[] = [];

  add(key: string, value: T): void {
    const queue = this.queues.get(key);
    if (queue) {
      queue.push(value);
    } else {
      this.queues.set(key, [value]);
    }
    this.pending.push(value);
  }

  find(predicate: (value: T) => boolean): T | undefined {
    return this.pending.find(predicate);
  }

  take(key: string, predicate: (value: T) => boolean): T | undefined {
    const queue = this.queues.get(key);
    if (!queue) return undefined;
    const queueIndex = queue.findIndex(predicate);
    if (queueIndex < 0) return undefined;

    const [value] = queue.splice(queueIndex, 1);
    if (queue.length === 0) this.queues.delete(key);
    const pendingIndex = this.pending.indexOf(value);
    if (pendingIndex >= 0) this.pending.splice(pendingIndex, 1);
    return value;
  }

  drain(): T[] {
    const values = this.pending.splice(0);
    this.queues.clear();
    return values;
  }
}

type ActiveRun = {
  sourceRunId: string;
  platformRunId: string;
  traceId: string;
  root: Span;
  startedAt: number;
  finalAnswer: string;
  tools: PendingRegistry<ActiveTool>;
  llms: PendingRegistry<ActiveLlm>;
};

const BENCHMARK_CONTEXT_MAX_VALUE_LENGTH = 512;

function optionalEnvironmentValue(name: string): string | undefined {
  const value = process.env[name]?.trim();
  if (!value || value.length > BENCHMARK_CONTEXT_MAX_VALUE_LENGTH) return undefined;
  return value;
}

function benchmarkContextFromEnvironment(): BenchmarkContext {
  const trialIndex = optionalEnvironmentValue("AGENTSCOPE_TRIAL_INDEX");
  const parsedTrialIndex = trialIndex !== undefined && /^\d+$/u.test(trialIndex)
    ? Number(trialIndex)
    : undefined;

  return {
    experimentId: optionalEnvironmentValue("AGENTSCOPE_EXPERIMENT_ID"),
    evalJobId: optionalEnvironmentValue("AGENTSCOPE_EVAL_JOB_ID"),
    evalTrialId: optionalEnvironmentValue("AGENTSCOPE_EVAL_TRIAL_ID"),
    benchmarkKey: optionalEnvironmentValue("AGENTSCOPE_BENCHMARK_KEY"),
    benchmarkTaskId: optionalEnvironmentValue("AGENTSCOPE_BENCHMARK_TASK_ID"),
    trialIndex: Number.isSafeInteger(parsedTrialIndex) ? parsedTrialIndex : undefined,
  };
}

function benchmarkMetadata(context: BenchmarkContext): Record<string, string | number> {
  return {
    ...(context.experimentId === undefined ? {} : { experiment_id: context.experimentId }),
    ...(context.evalJobId === undefined ? {} : { eval_job_id: context.evalJobId }),
    ...(context.evalTrialId === undefined ? {} : { eval_trial_id: context.evalTrialId }),
    ...(context.benchmarkKey === undefined ? {} : { benchmark_key: context.benchmarkKey }),
    ...(context.benchmarkTaskId === undefined ? {} : { benchmark_task_id: context.benchmarkTaskId }),
    ...(context.trialIndex === undefined ? {} : { trial_index: context.trialIndex }),
  };
}

function benchmarkSpanAttributes(context: BenchmarkContext): Record<string, string | number> {
  return {
    ...(context.experimentId === undefined ? {} : { "agentscope.experiment.id": context.experimentId }),
    ...(context.evalJobId === undefined ? {} : { "agentscope.eval.job.id": context.evalJobId }),
    ...(context.evalTrialId === undefined ? {} : { "agentscope.eval.trial.id": context.evalTrialId }),
    ...(context.benchmarkKey === undefined ? {} : { "agentscope.benchmark.key": context.benchmarkKey }),
    ...(context.benchmarkTaskId === undefined ? {} : { "agentscope.benchmark.task.id": context.benchmarkTaskId }),
    ...(context.trialIndex === undefined ? {} : { "agentscope.trial.index": context.trialIndex }),
  };
}

function parseConfig(value: unknown): Config {
  const config = value as Partial<Config> | undefined;
  if (!config?.serverUrl || !config.projectToken || !config.agentName) {
    throw new Error("agentscope-observability requires serverUrl, projectToken and agentName");
  }
  return {
    serverUrl: config.serverUrl.replace(/\/$/u, ""),
    projectToken: config.projectToken,
    agentName: config.agentName,
    captureContent: config.captureContent === true,
  };
}

function summary(value: unknown): string {
  try {
    return JSON.stringify(value).slice(0, 4096);
  } catch {
    return String(value).slice(0, 4096);
  }
}

const REDACTED_CONTENT = "[REDACTED]";

class ContentPolicy {
  constructor(private readonly captureContent: boolean) {}

  apiText(value: string | undefined): string {
    return this.captureContent ? value ?? "" : REDACTED_CONTENT;
  }

  joinedText(values: string[]): string {
    return this.captureContent ? values.join("\n") : REDACTED_CONTENT;
  }

  spanText(value: string): string | undefined {
    return this.captureContent ? value : undefined;
  }

  spanValue(value: unknown): string | undefined {
    return this.captureContent ? summary(value) : undefined;
  }

  exception(value: unknown, fallback = "Unknown error"): Error {
    if (!this.captureContent) return new Error(REDACTED_CONTENT);
    if (value instanceof Error) return value;
    if (value === undefined || value === null || value === "") {
      return new Error(fallback);
    }
    return new Error(String(value));
  }

  logError(value: unknown): string {
    return this.captureContent ? String(value) : REDACTED_CONTENT;
  }

  responseDetail(value: string): string {
    return this.captureContent ? value.slice(0, 512) : "";
  }
}

function collectText(value: unknown, output: string[]): void {
  if (typeof value === "string") {
    if (value) output.push(value);
    return;
  }
  if (Array.isArray(value)) {
    for (const item of value) collectText(item, output);
    return;
  }
  if (!value || typeof value !== "object") return;
  const record = value as Record<string, unknown>;
  if (typeof record.text === "string") output.push(record.text);
  if (record.content !== undefined) collectText(record.content, output);
}

function extractAssistantText(messages: unknown[]): string {
  const output: string[] = [];
  for (const message of messages) {
    if (!message || typeof message !== "object") continue;
    const record = message as Record<string, unknown>;
    if (record.role === "assistant") collectText(record.content, output);
  }
  return output.join("\n").trim();
}

class AgentScopeClient {
  private agentId = "";
  private readonly instanceId = `openclaw-${randomUUID()}`;

  constructor(
    private readonly config: Config,
    private readonly content: ContentPolicy,
  ) {}

  private async request(path: string, body: unknown, scope: "agent:register" | "agent:runtime"): Promise<unknown> {
    const response = await fetch(`${this.config.serverUrl}${path}`, {
      method: "POST",
      headers: { Authorization: `Bearer ${this.config.projectToken}`, "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(5_000),
    });
    if (!response.ok) {
      const detail = this.content.responseDetail(await response.text());
      throw new Error(`AgentScope ${scope} request failed: ${response.status}${detail ? `: ${detail}` : ""}`);
    }
    return response.json();
  }

  async ensureAgent(): Promise<string> {
    if (this.agentId) return this.agentId;
    const registered = (await this.request("/api/v1/agents/register", {
      name: this.config.agentName,
      description: "OpenClaw gateway observed by AgentScope",
      metadata: { framework: "openclaw", source: "native-plugin" },
    }, "agent:register")) as { id: string };
    this.agentId = registered.id;
    await this.heartbeat("online");
    return this.agentId;
  }

  async heartbeat(status: string): Promise<void> {
    if (!this.agentId) return;
    await this.request(`/api/v1/agents/${this.agentId}/heartbeat`, {
      instance_id: this.instanceId,
      status,
      payload: { framework: "openclaw" },
    }, "agent:register");
  }

  async startRun(
    input: string,
    traceId: string,
    sourceRunId: string,
    benchmarkContext: BenchmarkContext,
  ): Promise<RuntimeRun> {
    const agentId = await this.ensureAgent();
    return (await this.request("/api/sdk/runs/start", {
      agent_id: agentId,
      instance_id: this.instanceId,
      input: this.content.apiText(input),
      trace_id: traceId,
      metadata: {
        framework: "openclaw",
        adapter_name: "openclaw-native-plugin",
        source_run_id: sourceRunId,
        ...benchmarkMetadata(benchmarkContext),
      },
    }, "agent:runtime")) as RuntimeRun;
  }

  async finishRun(run: ActiveRun, success: boolean, error = ""): Promise<void> {
    const latencyMs = Date.now() - run.startedAt;
    const body = {
      agent_id: this.agentId,
      instance_id: this.instanceId,
      trace_id: run.traceId,
      latency_ms: latencyMs,
    };
    if (success) {
      await this.request(`/api/sdk/runs/${run.platformRunId}/finish`, {
        ...body,
        output: this.content.apiText(run.finalAnswer),
      }, "agent:runtime");
      return;
    }
    await this.request(`/api/sdk/runs/${run.platformRunId}/fail`, {
      ...body,
      error: this.content.apiText(error),
    }, "agent:runtime");
  }
}

class OpenClawTracer {
  private readonly client: AgentScopeClient;
  private readonly content: ContentPolicy;
  private readonly provider: BasicTracerProvider;
  private readonly tracer;
  private readonly runs = new Map<string, ActiveRun>();
  private readonly benchmarkContext = benchmarkContextFromEnvironment();

  constructor(private readonly config: Config, private readonly log: (message: string) => void) {
    this.content = new ContentPolicy(config.captureContent);
    this.client = new AgentScopeClient(config, this.content);
    const exporter = new OTLPTraceExporter({ url: `${config.serverUrl}/v1/traces`, headers: { Authorization: `Bearer ${config.projectToken}` } });
    this.provider = new BasicTracerProvider({
      resource: resourceFromAttributes({ "service.name": config.agentName, "agentscope.framework": "openclaw" }),
      spanProcessors: [new SimpleSpanProcessor(exporter)],
    });
    this.tracer = this.provider.getTracer("agentscope.openclaw");
  }

  async start(sourceRunId: string, input: string): Promise<ActiveRun | undefined> {
    if (this.runs.has(sourceRunId)) return this.runs.get(sourceRunId);
    const root = this.tracer.startSpan("agent.run", {
      attributes: {
        "agentscope.span.kind": "AGENT",
        "openinference.span.kind": "AGENT",
        "agentscope.source_run_id": sourceRunId,
        ...benchmarkSpanAttributes(this.benchmarkContext),
      },
    });
    const traceId = root.spanContext().traceId;
    try {
      const remote = await this.client.startRun(
        input,
        traceId,
        sourceRunId,
        this.benchmarkContext,
      );
      root.setAttribute("agentscope.run.id", remote.id);
      root.setAttribute("agentscope.trace.id", traceId);
      const run: ActiveRun = {
        sourceRunId,
        platformRunId: remote.id,
        traceId,
        root,
        startedAt: Date.now(),
        finalAnswer: this.content.apiText(""),
        tools: new PendingRegistry(),
        llms: new PendingRegistry(),
      };
      this.runs.set(sourceRunId, run);
      return run;
    } catch (error) {
      root.recordException(this.content.exception(error));
      root.end();
      this.log(`AgentScope run start failed: ${this.content.logError(error)}`);
      return undefined;
    }
  }

  async activate(): Promise<void> {
    try {
      await this.client.ensureAgent();
    } catch (error) {
      this.log(`AgentScope agent registration failed: ${this.content.logError(error)}`);
    }
  }

  private child(run: ActiveRun, name: string, attributes: Record<string, string | number | boolean>): Span {
    const parent = trace.setSpan(context.active(), run.root);
    return this.tracer.startSpan(name, { attributes: { ...attributes, "agentscope.run.id": run.platformRunId, "agentscope.trace.id": run.traceId } }, parent);
  }

  private setContentText(span: Span, key: string, value: string): void {
    const captured = this.content.spanText(value);
    if (captured !== undefined) span.setAttribute(key, captured);
  }

  private setContentValue(span: Span, key: string, value: unknown): void {
    const captured = this.content.spanValue(value);
    if (captured !== undefined) span.setAttribute(key, captured);
  }

  async modelStarted(runId: string, callId: string, provider: string, model: string): Promise<void> {
    const run = this.runs.get(runId);
    if (!run) return;
    run.llms.add(callId, {
      span: this.child(run, "llm.call", { "agentscope.span.kind": "LLM", "openinference.span.kind": "LLM", "llm.provider": provider, "llm.model": model }),
      provider,
      model,
      inputSeen: false,
      outputSeen: false,
    });
  }

  modelInput(runId: string, event: { provider: string; model: string; prompt: string; systemPrompt?: string; historyMessages: unknown[] }): void {
    const run = this.runs.get(runId);
    if (!run) return;
    const active = run.llms.find(
      (candidate) =>
        !candidate.inputSeen &&
        candidate.provider === event.provider &&
        candidate.model === event.model,
    );
    if (active) {
      active.inputSeen = true;
      this.setContentText(active.span, "llm.input_summary", event.prompt);
      active.span.setAttribute("llm.prompt_length", event.prompt.length);
      active.span.setAttribute("llm.history_messages", event.historyMessages.length);
    }
    if (event.systemPrompt) active?.span.setAttribute("llm.system_prompt_captured", this.config.captureContent === true);
  }

  modelOutput(runId: string, event: { provider: string; model: string; assistantTexts: string[]; usage?: { input?: number; output?: number; total?: number } }): void {
    const run = this.runs.get(runId);
    if (!run) return;
    run.finalAnswer = this.content.joinedText(event.assistantTexts);
    const active = run.llms.find(
      (candidate) =>
        !candidate.outputSeen &&
        candidate.provider === event.provider &&
        candidate.model === event.model,
    );
    if (!active) return;
    active.outputSeen = true;
    this.setContentText(active.span, "llm.output_summary", run.finalAnswer);
    active.span.setAttribute(
      "llm.output_length",
      event.assistantTexts.reduce(
        (length, text, index) => length + text.length + (index > 0 ? 1 : 0),
        0,
      ),
    );
    if (event.usage?.input !== undefined) active.span.setAttribute("llm.input_tokens", event.usage.input);
    if (event.usage?.output !== undefined) active.span.setAttribute("llm.output_tokens", event.usage.output);
    if (event.usage?.total !== undefined) active.span.setAttribute("llm.total_tokens", event.usage.total);
  }

  modelEnded(runId: string, callId: string, provider: string, model: string, durationMs: number, outcome: string, error?: string): void {
    const run = this.runs.get(runId);
    const active = run?.llms.take(
      callId,
      (candidate) =>
        candidate.provider === provider &&
        candidate.model === model,
    );
    if (!active) return;
    active.span.setAttribute("llm.duration_ms", durationMs);
    if (outcome !== "completed") {
      active.span.recordException(error ? this.content.exception(error) : new Error(outcome));
    }
    active.span.end();
  }

  toolStarted(runId: string, toolCallId: string, toolName: string, params: unknown): void {
    const run = this.runs.get(runId);
    if (!run) return;
    const span = this.child(run, `tool.${toolName}`, {
      "agentscope.span.kind": "TOOL",
      "openinference.span.kind": "TOOL",
      "tool.name": toolName,
    });
    this.setContentValue(span, "tool.arguments_summary", params);
    run.tools.add(toolCallId, { span, toolName });
  }

  toolEnded(runId: string, toolCallId: string, toolName: string, result: unknown, durationMs?: number, error?: string): void {
    const run = this.runs.get(runId);
    const active = run?.tools.take(
      toolCallId,
      (candidate) => candidate.toolName === toolName,
    );
    if (!run || !active) return;
    this.setContentValue(active.span, "tool.result_summary", result);
    if (durationMs !== undefined) active.span.setAttribute("tool.duration_ms", durationMs);
    if (error) active.span.recordException(this.content.exception(error));
    active.span.end();
    const kind = toolName.toLowerCase().includes("memory") ? "MEMORY" : toolName.toLowerCase().includes("skill") ? "SKILL" : "";
    if (kind) {
      const semantic = this.child(run, kind === "MEMORY" ? "memory.access" : "skill.call", {
        "agentscope.span.kind": kind,
        "openinference.span.kind": kind,
        "event.name": toolName,
      });
      this.setContentValue(semantic, "event.output_summary", result);
      semantic.end();
    }
  }

  async end(runId: string, success: boolean, error?: string, finalAnswer?: string): Promise<void> {
    const run = this.runs.get(runId);
    if (!run) return;
    if (finalAnswer) run.finalAnswer = this.content.apiText(finalAnswer);
    for (const active of run.llms.drain()) active.span.end();
    for (const active of run.tools.drain()) active.span.end();
    const answer = this.child(run, "agent.answer", {
      "agentscope.span.kind": "FINAL_ANSWER",
      "openinference.span.kind": "FINAL_ANSWER",
    });
    this.setContentText(answer, "agent.output_summary", run.finalAnswer);
    answer.end();
    if (!success) {
      run.root.recordException(
        error
          ? this.content.exception(error)
          : new Error("OpenClaw agent failed"),
      );
    }
    try {
      await this.client.finishRun(run, success, error);
    } catch (finishError) {
      run.root.recordException(this.content.exception(finishError));
      this.log(`AgentScope run finish failed: ${this.content.logError(finishError)}`);
    } finally {
      run.root.end();
      await this.provider.forceFlush();
      this.runs.delete(runId);
    }
  }

  async shutdown(): Promise<void> {
    await this.client.heartbeat("offline").catch((error) =>
      this.log(`AgentScope heartbeat failed: ${this.content.logError(error)}`),
    );
    await this.provider.shutdown();
  }
}

export default definePluginEntry({
  id: "agentscope-observability",
  name: "AgentScope Observability",
  description: "Exports OpenClaw agent lifecycle, LLM, tool and session traces to AgentScope.",
  configSchema: { parse: parseConfig },
  register(api: OpenClawPluginApi) {
    const config = parseConfig(api.pluginConfig);
    const observer = new OpenClawTracer(config, (message) => api.logger.warn(message));
    const sourceRun = (runId: string | undefined, fallback: string) => runId || fallback;
    api.on("gateway_start", () => { void observer.activate(); });
    api.on("before_agent_run", async (event, ctx) => { await observer.start(sourceRun(ctx.runId, ctx.sessionId || randomUUID()), event.prompt); });
    api.on("agent_end", async (event, ctx) => {
      const runId = sourceRun(event.runId, ctx.runId || ctx.sessionId || "");
      const finalAnswer = config.captureContent
        ? extractAssistantText(event.messages)
        : undefined;
      if (runId) await observer.end(runId, event.success, event.error, finalAnswer);
    });
    api.on("gateway_stop", async () => { await observer.shutdown(); });
    api.on("model_call_started", async (event) => { await observer.modelStarted(event.runId, event.callId, event.provider, event.model); });
    api.on("llm_input", (event) => observer.modelInput(event.runId, event));
    api.on("llm_output", (event) => observer.modelOutput(event.runId, event));
    api.on("model_call_ended", (event) => observer.modelEnded(event.runId, event.callId, event.provider, event.model, event.durationMs, event.outcome, event.errorCategory));
    api.on("before_tool_call", (event, ctx) => observer.toolStarted(sourceRun(event.runId, ctx.runId || ""), event.toolCallId || event.toolName, event.toolName, event.params));
    api.on("after_tool_call", (event, ctx) => observer.toolEnded(sourceRun(event.runId, ctx.runId || ""), event.toolCallId || event.toolName, event.toolName, event.result, event.durationMs, event.error));
  },
});
