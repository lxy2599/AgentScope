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
  captureContent?: boolean;
};

type RuntimeRun = { id: string };
type ActiveRun = {
  sourceRunId: string;
  platformRunId: string;
  traceId: string;
  root: Span;
  startedAt: number;
  finalAnswer: string;
  tools: Map<string, Span>;
  llms: Map<string, Span>;
};

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

function text(value: unknown): string {
  return typeof value === "string" ? value : summary(value);
}

class AgentScopeClient {
  private agentId = "";
  private readonly instanceId = `openclaw-${randomUUID()}`;

  constructor(private readonly config: Config) {}

  private async request(path: string, body: unknown, scope: "agent:register" | "agent:runtime"): Promise<unknown> {
    const response = await fetch(`${this.config.serverUrl}${path}`, {
      method: "POST",
      headers: { Authorization: `Bearer ${this.config.projectToken}`, "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (!response.ok) {
      throw new Error(`AgentScope ${scope} request failed: ${response.status}`);
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

  async startRun(input: string, traceId: string, sourceRunId: string): Promise<RuntimeRun> {
    const agentId = await this.ensureAgent();
    return (await this.request("/api/sdk/runs/start", {
      agent_id: agentId,
      instance_id: this.instanceId,
      input,
      trace_id: traceId,
      metadata: { framework: "openclaw", adapter_name: "openclaw-native-plugin", source_run_id: sourceRunId },
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
      await this.request(`/api/sdk/runs/${run.platformRunId}/finish`, { ...body, output: run.finalAnswer }, "agent:runtime");
      return;
    }
    await this.request(`/api/sdk/runs/${run.platformRunId}/fail`, { ...body, error }, "agent:runtime");
  }
}

class OpenClawTracer {
  private readonly client: AgentScopeClient;
  private readonly provider: BasicTracerProvider;
  private readonly tracer;
  private readonly runs = new Map<string, ActiveRun>();

  constructor(private readonly config: Config, private readonly log: (message: string) => void) {
    this.client = new AgentScopeClient(config);
    const exporter = new OTLPTraceExporter({ url: `${config.serverUrl}/v1/traces`, headers: { Authorization: `Bearer ${config.projectToken}` } });
    this.provider = new BasicTracerProvider({
      resource: resourceFromAttributes({ "service.name": config.agentName, "agentscope.framework": "openclaw" }),
      spanProcessors: [new SimpleSpanProcessor(exporter)],
    });
    this.tracer = this.provider.getTracer("agentscope.openclaw");
  }

  async start(sourceRunId: string, input: string): Promise<ActiveRun | undefined> {
    if (this.runs.has(sourceRunId)) return this.runs.get(sourceRunId);
    const root = this.tracer.startSpan("agent.run", { attributes: { "agentscope.span.kind": "AGENT", "openinference.span.kind": "AGENT", "agentscope.source_run_id": sourceRunId } });
    const traceId = root.spanContext().traceId;
    try {
      const remote = await this.client.startRun(input, traceId, sourceRunId);
      root.setAttribute("agentscope.run.id", remote.id);
      root.setAttribute("agentscope.trace.id", traceId);
      const run: ActiveRun = { sourceRunId, platformRunId: remote.id, traceId, root, startedAt: Date.now(), finalAnswer: "", tools: new Map(), llms: new Map() };
      this.runs.set(sourceRunId, run);
      return run;
    } catch (error) {
      root.recordException(error as Error);
      root.end();
      this.log(`AgentScope run start failed: ${String(error)}`);
      return undefined;
    }
  }

  async activate(): Promise<void> {
    try {
      await this.client.ensureAgent();
    } catch (error) {
      this.log(`AgentScope agent registration failed: ${String(error)}`);
    }
  }

  private child(run: ActiveRun, name: string, attributes: Record<string, string | number | boolean>): Span {
    const parent = trace.setSpan(context.active(), run.root);
    return this.tracer.startSpan(name, { attributes: { ...attributes, "agentscope.run.id": run.platformRunId, "agentscope.trace.id": run.traceId } }, parent);
  }

  async modelStarted(runId: string, callId: string, provider: string, model: string): Promise<void> {
    const run = this.runs.get(runId);
    if (!run) return;
    run.llms.set(callId, this.child(run, "llm.call", { "agentscope.span.kind": "LLM", "openinference.span.kind": "LLM", "llm.provider": provider, "llm.model": model }));
  }

  modelInput(runId: string, event: { prompt: string; systemPrompt?: string; historyMessages: unknown[] }): void {
    const run = this.runs.get(runId);
    if (!run) return;
    const span = [...run.llms.values()].at(-1);
    if (span) span.setAttribute("llm.input_summary", this.config.captureContent ? event.prompt : summary({ prompt_length: event.prompt.length, history_messages: event.historyMessages.length }));
    if (event.systemPrompt) span?.setAttribute("llm.system_prompt_captured", this.config.captureContent === true);
  }

  modelOutput(runId: string, event: { assistantTexts: string[]; usage?: { input?: number; output?: number; total?: number } }): void {
    const run = this.runs.get(runId);
    if (!run) return;
    run.finalAnswer = event.assistantTexts.join("\n");
    const span = [...run.llms.values()].at(-1);
    if (!span) return;
    span.setAttribute("llm.output_summary", this.config.captureContent ? run.finalAnswer : `length=${run.finalAnswer.length}`);
    if (event.usage?.input !== undefined) span.setAttribute("llm.input_tokens", event.usage.input);
    if (event.usage?.output !== undefined) span.setAttribute("llm.output_tokens", event.usage.output);
    if (event.usage?.total !== undefined) span.setAttribute("llm.total_tokens", event.usage.total);
  }

  modelEnded(runId: string, callId: string, durationMs: number, outcome: string, error?: string): void {
    const run = this.runs.get(runId);
    const span = run?.llms.get(callId);
    if (!span) return;
    span.setAttribute("llm.duration_ms", durationMs);
    if (outcome !== "completed") span.recordException(new Error(error || outcome));
    span.end();
    run?.llms.delete(callId);
  }

  toolStarted(runId: string, toolCallId: string, toolName: string, params: unknown): void {
    const run = this.runs.get(runId);
    if (!run) return;
    run.tools.set(toolCallId, this.child(run, `tool.${toolName}`, { "agentscope.span.kind": "TOOL", "openinference.span.kind": "TOOL", "tool.name": toolName, "tool.arguments_summary": summary(params) }));
  }

  toolEnded(runId: string, toolCallId: string, toolName: string, result: unknown, durationMs?: number, error?: string): void {
    const run = this.runs.get(runId);
    const span = run?.tools.get(toolCallId);
    if (!span) return;
    span.setAttribute("tool.result_summary", summary(result));
    if (durationMs !== undefined) span.setAttribute("tool.duration_ms", durationMs);
    if (error) span.recordException(new Error(error));
    span.end();
    run?.tools.delete(toolCallId);
    const kind = toolName.toLowerCase().includes("memory") ? "MEMORY" : toolName.toLowerCase().includes("skill") ? "SKILL" : "";
    if (kind) {
      const semantic = this.child(run!, kind === "MEMORY" ? "memory.access" : "skill.call", { "agentscope.span.kind": kind, "openinference.span.kind": kind, "event.name": toolName, "event.output_summary": summary(result) });
      semantic.end();
    }
  }

  async end(runId: string, success: boolean, error?: string): Promise<void> {
    const run = this.runs.get(runId);
    if (!run) return;
    for (const span of run.llms.values()) span.end();
    for (const span of run.tools.values()) span.end();
    const answer = this.child(run, "agent.answer", { "agentscope.span.kind": "FINAL_ANSWER", "openinference.span.kind": "FINAL_ANSWER", "agent.output_summary": this.config.captureContent ? run.finalAnswer : `length=${run.finalAnswer.length}` });
    answer.end();
    if (!success) run.root.recordException(new Error(error || "OpenClaw agent failed"));
    try {
      await this.client.finishRun(run, success, error);
    } catch (finishError) {
      run.root.recordException(finishError as Error);
      this.log(`AgentScope run finish failed: ${String(finishError)}`);
    } finally {
      run.root.end();
      await this.provider.forceFlush();
      this.runs.delete(runId);
    }
  }

  async shutdown(): Promise<void> {
    await this.client.heartbeat("offline").catch((error) => this.log(`AgentScope heartbeat failed: ${String(error)}`));
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

    api.on("gateway_start", async () => { await observer.activate(); });
    api.on("before_agent_run", async (event, ctx) => { await observer.start(sourceRun(ctx.runId, ctx.sessionId || randomUUID()), event.prompt); });
    api.on("model_call_started", async (event) => { await observer.modelStarted(event.runId, event.callId, event.provider, event.model); });
    api.on("llm_input", (event) => observer.modelInput(event.runId, event));
    api.on("llm_output", (event) => observer.modelOutput(event.runId, event));
    api.on("model_call_ended", (event) => observer.modelEnded(event.runId, event.callId, event.durationMs, event.outcome, event.errorCategory));
    api.on("before_tool_call", (event, ctx) => observer.toolStarted(sourceRun(event.runId, ctx.runId || ""), event.toolCallId || event.toolName, event.toolName, event.params));
    api.on("after_tool_call", (event) => observer.toolEnded(event.runId || "", event.toolCallId || event.toolName, event.toolName, event.result, event.durationMs, event.error));
    api.on("agent_end", async (event) => { if (event.runId) await observer.end(event.runId, event.success, event.error); });
    api.on("gateway_stop", async () => { await observer.shutdown(); });
  },
});
