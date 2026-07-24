import {
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";

import type {
  EventContext,
  OpenClawEvents,
  OpenClawPluginApi,
} from "./test/openclaw-entry.js";

type RecordedSpan = {
  name: string;
  attributes: Record<string, unknown>;
  exceptions: unknown[];
  endCount: number;
  ended: boolean;
  traceId: string;
};

const telemetry = vi.hoisted(() => ({
  nextTraceId: 0,
  spans: [] as RecordedSpan[],
}));

vi.mock("@opentelemetry/api", () => ({
  context: {
    active: () => ({}),
  },
  trace: {
    setSpan: (activeContext: unknown) => activeContext,
  },
}));

vi.mock("@opentelemetry/exporter-trace-otlp-proto", () => ({
  OTLPTraceExporter: class {},
}));

vi.mock("@opentelemetry/resources", () => ({
  resourceFromAttributes: (attributes: Record<string, unknown>) => attributes,
}));

vi.mock("@opentelemetry/sdk-trace-base", () => ({
  BasicTracerProvider: class {
    getTracer() {
      return {
        startSpan(
          name: string,
          options?: { attributes?: Record<string, unknown> },
        ) {
          telemetry.nextTraceId += 1;
          const record: RecordedSpan = {
            name,
            attributes: { ...options?.attributes },
            exceptions: [],
            endCount: 0,
            ended: false,
            traceId: telemetry.nextTraceId.toString(16).padStart(32, "0"),
          };
          telemetry.spans.push(record);
          return {
            end() {
              record.endCount += 1;
              record.ended = true;
            },
            recordException(error: unknown) {
              record.exceptions.push(
                error instanceof Error
                  ? { name: error.name, message: error.message }
                  : error,
              );
            },
            setAttribute(key: string, value: unknown) {
              record.attributes[key] = value;
            },
            spanContext() {
              return { traceId: record.traceId };
            },
          };
        },
      };
    }

    async forceFlush() {}

    async shutdown() {}
  },
  SimpleSpanProcessor: class {
    constructor(_exporter: unknown) {}
  },
}));

import plugin from "./index.js";

type Handler = (
  event: OpenClawEvents[keyof OpenClawEvents],
  context: EventContext,
) => void | Promise<void>;

type RequestRecord = {
  body: string;
  url: string;
};

const REDACTED = "[REDACTED]";
let requests: RequestRecord[];
let responseDetail: string | undefined;

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    headers: { "Content-Type": "application/json" },
    status,
  });
}

function requestBody(path: string): Record<string, unknown> {
  const request = requests.find(({ url }) => url.endsWith(path));
  expect(request, `missing request for ${path}`).toBeDefined();
  return JSON.parse(request?.body || "{}") as Record<string, unknown>;
}

function createHarness(config: { captureContent?: boolean } = {}) {
  const handlers = new Map<keyof OpenClawEvents, Handler>();
  const warnings: string[] = [];
  const api: OpenClawPluginApi = {
    pluginConfig: {
      agentName: "privacy-test-agent",
      projectToken: "project-token",
      serverUrl: "http://agentscope.test",
      ...config,
    },
    logger: {
      warn(message: string) {
        warnings.push(message);
      },
    },
    on: ((event: keyof OpenClawEvents, handler: Handler) => {
      handlers.set(event, handler);
    }) as OpenClawPluginApi["on"],
  };

  plugin.register(api);

  return {
    async emit<K extends keyof OpenClawEvents>(
      event: K,
      payload: OpenClawEvents[K],
      context: EventContext = {},
    ): Promise<void> {
      const handler = handlers.get(event);
      expect(handler, `missing ${event} handler`).toBeDefined();
      await handler?.(payload, context);
    },
    warnings,
  };
}

beforeEach(() => {
  requests = [];
  responseDetail = undefined;
  telemetry.nextTraceId = 0;
  telemetry.spans.length = 0;

  vi.stubGlobal("fetch", vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
    const url = typeof input === "string"
      ? input
      : input instanceof URL
        ? input.href
        : input.url;
    requests.push({
      body: typeof init?.body === "string" ? init.body : "",
      url,
    });

    if (url.endsWith("/api/v1/agents/register")) {
      return jsonResponse({ id: "agent-1" });
    }
    if (url.includes("/heartbeat")) {
      return jsonResponse({});
    }
    if (url.endsWith("/api/sdk/runs/start")) {
      if (responseDetail) {
        return new Response(responseDetail, { status: 500 });
      }
      const runNumber = requests.filter(({ url: requestUrl }) =>
        requestUrl.endsWith("/api/sdk/runs/start")
      ).length;
      return jsonResponse({ id: `platform-run-${runNumber}` });
    }
    return jsonResponse({});
  }));
});

describe("captureContent privacy boundary", () => {
  it("defaults to false and removes sensitive content from every export sink", async () => {
    const prompt = "SENTINEL_PROMPT";
    const finalAnswer = "SENTINEL_FINAL_ANSWER";
    const toolParams = "SENTINEL_TOOL_PARAMS";
    const toolResult = "SENTINEL_TOOL_RESULT";
    const modelError = "SENTINEL_MODEL_ERROR";
    const toolError = "SENTINEL_TOOL_ERROR";
    const runError = "SENTINEL_RUN_ERROR";
    const harness = createHarness();

    await harness.emit(
      "before_agent_run",
      { prompt },
      { runId: "run-1", sessionId: "session-1" },
    );
    await harness.emit("model_call_started", {
      callId: "call-1",
      model: "model-1",
      provider: "provider-1",
      runId: "run-1",
    });
    await harness.emit("llm_input", {
      historyMessages: [{ content: prompt }],
      model: "model-1",
      prompt,
      provider: "provider-1",
      runId: "run-1",
      systemPrompt: prompt,
    });
    await harness.emit("llm_output", {
      assistantTexts: [finalAnswer],
      model: "model-1",
      provider: "provider-1",
      runId: "run-1",
      usage: { input: 3, output: 5, total: 8 },
    });
    await harness.emit(
      "before_tool_call",
      {
        params: { value: toolParams },
        runId: "run-1",
        toolCallId: "tool-call-1",
        toolName: "memory_lookup",
      },
      { runId: "run-1" },
    );
    await harness.emit("after_tool_call", {
      durationMs: 12,
      error: toolError,
      result: { value: toolResult },
      runId: "run-1",
      toolCallId: "tool-call-1",
      toolName: "memory_lookup",
    });
    await harness.emit("model_call_ended", {
      callId: "call-1",
      durationMs: 20,
      errorCategory: modelError,
      model: "model-1",
      outcome: "failed",
      provider: "provider-1",
      runId: "run-1",
    });
    await harness.emit("agent_end", {
      error: runError,
      messages: [{ content: finalAnswer, role: "assistant" }],
      runId: "run-1",
      success: false,
    });

    const exported = JSON.stringify({
      requests,
      spans: telemetry.spans,
      warnings: harness.warnings,
    });
    for (const sentinel of [
      prompt,
      finalAnswer,
      toolParams,
      toolResult,
      modelError,
      toolError,
      runError,
    ]) {
      expect(exported).not.toContain(sentinel);
    }

    expect(requestBody("/api/sdk/runs/start").input).toBe(REDACTED);
    expect(requestBody("/api/sdk/runs/platform-run-1/fail").error).toBe(REDACTED);

    const attributeKeys = telemetry.spans.flatMap((span) =>
      Object.keys(span.attributes),
    );
    expect(attributeKeys).not.toContain("llm.input_summary");
    expect(attributeKeys).not.toContain("llm.output_summary");
    expect(attributeKeys).not.toContain("tool.arguments_summary");
    expect(attributeKeys).not.toContain("tool.result_summary");
    expect(attributeKeys).not.toContain("event.output_summary");
    expect(attributeKeys).not.toContain("agent.output_summary");
  });

  it("redacts the successful run output", async () => {
    const finalAnswer = "SENTINEL_SUCCESS_FINAL_ANSWER";
    const harness = createHarness({ captureContent: false });

    await harness.emit(
      "before_agent_run",
      { prompt: "SENTINEL_SUCCESS_PROMPT" },
      { runId: "run-2" },
    );
    await harness.emit("agent_end", {
      messages: [{ content: finalAnswer, role: "assistant" }],
      runId: "run-2",
      success: true,
    });

    expect(requestBody("/api/sdk/runs/platform-run-1/finish").output).toBe(
      REDACTED,
    );
    expect(JSON.stringify({ requests, spans: telemetry.spans })).not.toContain(
      finalAnswer,
    );
  });

  it("does not stringify or coerce sensitive objects when capture is disabled", async () => {
    const harness = createHarness({ captureContent: false });
    const paramsToJSON = vi.fn(() => {
      throw new Error("params toJSON must not run");
    });
    const paramsToString = vi.fn(() => {
      throw new Error("params toString must not run");
    });
    const resultToJSON = vi.fn(() => {
      throw new Error("result toJSON must not run");
    });
    const resultToString = vi.fn(() => {
      throw new Error("result toString must not run");
    });

    await harness.emit(
      "before_agent_run",
      { prompt: "private prompt" },
      { runId: "run-3" },
    );
    await harness.emit(
      "before_tool_call",
      {
        params: { toJSON: paramsToJSON, toString: paramsToString },
        runId: "run-3",
        toolCallId: "tool-call-3",
        toolName: "memory_lookup",
      },
      { runId: "run-3" },
    );
    await harness.emit("after_tool_call", {
      result: { toJSON: resultToJSON, toString: resultToString },
      runId: "run-3",
      toolCallId: "tool-call-3",
      toolName: "memory_lookup",
    });

    expect(paramsToJSON).not.toHaveBeenCalled();
    expect(paramsToString).not.toHaveBeenCalled();
    expect(resultToJSON).not.toHaveBeenCalled();
    expect(resultToString).not.toHaveBeenCalled();
  });

  it("does not leak an AgentScope HTTP error response into spans or logs", async () => {
    const httpDetail = "SENTINEL_HTTP_RESPONSE_DETAIL";
    responseDetail = httpDetail;
    const harness = createHarness({ captureContent: false });

    await harness.emit(
      "before_agent_run",
      { prompt: "private prompt" },
      { runId: "run-http-error" },
    );

    expect(
      JSON.stringify({
        spans: telemetry.spans,
        warnings: harness.warnings,
      }),
    ).not.toContain(httpDetail);
    expect(harness.warnings).toHaveLength(1);
  });

  it("preserves captured content and the 4096 character structured summary limit", async () => {
    const prompt = "VISIBLE_PROMPT";
    const finalAnswer = "VISIBLE_FINAL_ANSWER";
    const longToolValue = `VISIBLE_TOOL_VALUE_${"x".repeat(5000)}`;
    const harness = createHarness({ captureContent: true });

    await harness.emit(
      "before_agent_run",
      { prompt },
      { runId: "run-visible" },
    );
    await harness.emit("model_call_started", {
      callId: "call-visible",
      model: "model-visible",
      provider: "provider-visible",
      runId: "run-visible",
    });
    await harness.emit("llm_input", {
      historyMessages: [],
      model: "model-visible",
      prompt,
      provider: "provider-visible",
      runId: "run-visible",
    });
    await harness.emit("llm_output", {
      assistantTexts: [finalAnswer],
      model: "model-visible",
      provider: "provider-visible",
      runId: "run-visible",
    });
    await harness.emit(
      "before_tool_call",
      {
        params: { value: longToolValue },
        runId: "run-visible",
        toolCallId: "tool-visible",
        toolName: "memory_lookup",
      },
      { runId: "run-visible" },
    );
    await harness.emit("after_tool_call", {
      result: { value: longToolValue },
      runId: "run-visible",
      toolCallId: "tool-visible",
      toolName: "memory_lookup",
    });
    await harness.emit("agent_end", {
      messages: [{ content: finalAnswer, role: "assistant" }],
      runId: "run-visible",
      success: true,
    });

    expect(requestBody("/api/sdk/runs/start").input).toBe(prompt);
    expect(requestBody("/api/sdk/runs/platform-run-1/finish").output).toBe(
      finalAnswer,
    );

    const llm = telemetry.spans.find(({ name }) => name === "llm.call");
    expect(llm?.attributes["llm.input_summary"]).toBe(prompt);
    expect(llm?.attributes["llm.output_summary"]).toBe(finalAnswer);

    const tool = telemetry.spans.find(({ name }) => name === "tool.memory_lookup");
    expect(tool?.attributes["tool.arguments_summary"]).toContain(
      "VISIBLE_TOOL_VALUE",
    );
    expect(tool?.attributes["tool.arguments_summary"]).toHaveLength(4096);
    expect(tool?.attributes["tool.result_summary"]).toContain(
      "VISIBLE_TOOL_VALUE",
    );
    expect(tool?.attributes["tool.result_summary"]).toHaveLength(4096);
  });
});

describe("concurrent hook correlation", () => {
  it("keeps overlapping LLM calls with the same call ID separate", async () => {
    const harness = createHarness({ captureContent: true });

    await harness.emit(
      "before_agent_run",
      { prompt: "run prompt" },
      { runId: "run-llm-overlap" },
    );
    await harness.emit("model_call_started", {
      callId: "shared-call",
      model: "shared-model",
      provider: "shared-provider",
      runId: "run-llm-overlap",
    });
    await harness.emit("llm_input", {
      historyMessages: [],
      model: "shared-model",
      prompt: "first input",
      provider: "shared-provider",
      runId: "run-llm-overlap",
    });
    await harness.emit("model_call_started", {
      callId: "shared-call",
      model: "shared-model",
      provider: "shared-provider",
      runId: "run-llm-overlap",
    });
    await harness.emit("llm_input", {
      historyMessages: [],
      model: "shared-model",
      prompt: "second input",
      provider: "shared-provider",
      runId: "run-llm-overlap",
    });
    await harness.emit("llm_output", {
      assistantTexts: ["first output"],
      model: "shared-model",
      provider: "shared-provider",
      runId: "run-llm-overlap",
    });
    await harness.emit("model_call_ended", {
      callId: "shared-call",
      durationMs: 11,
      model: "shared-model",
      outcome: "completed",
      provider: "shared-provider",
      runId: "run-llm-overlap",
    });
    await harness.emit("llm_output", {
      assistantTexts: ["second output"],
      model: "shared-model",
      provider: "shared-provider",
      runId: "run-llm-overlap",
    });
    await harness.emit("model_call_ended", {
      callId: "shared-call",
      durationMs: 22,
      model: "shared-model",
      outcome: "completed",
      provider: "shared-provider",
      runId: "run-llm-overlap",
    });

    const llms = telemetry.spans.filter(({ name }) => name === "llm.call");
    expect(llms).toHaveLength(2);
    expect(llms.map(({ attributes }) => attributes["llm.input_summary"])).toEqual([
      "first input",
      "second input",
    ]);
    expect(llms.map(({ attributes }) => attributes["llm.output_summary"])).toEqual([
      "first output",
      "second output",
    ]);
    expect(llms.map(({ attributes }) => attributes["llm.duration_ms"])).toEqual([
      11,
      22,
    ]);
    expect(llms.map(({ endCount }) => endCount)).toEqual([1, 1]);
  });

  it("queues overlapping same-name tools when toolCallId is absent", async () => {
    const harness = createHarness({ captureContent: true });

    await harness.emit(
      "before_agent_run",
      { prompt: "run prompt" },
      { runId: "run-tool-overlap" },
    );
    await harness.emit(
      "before_tool_call",
      {
        params: { request: "first" },
        runId: "run-tool-overlap",
        toolName: "search",
      },
      { runId: "run-tool-overlap" },
    );
    await harness.emit(
      "before_tool_call",
      {
        params: { request: "second" },
        runId: "run-tool-overlap",
        toolName: "search",
      },
      { runId: "run-tool-overlap" },
    );
    await harness.emit("after_tool_call", {
      result: { response: "first" },
      runId: "run-tool-overlap",
      toolName: "search",
    });
    await harness.emit("after_tool_call", {
      result: { response: "second" },
      runId: "run-tool-overlap",
      toolName: "search",
    });

    const tools = telemetry.spans.filter(({ name }) => name === "tool.search");
    expect(tools).toHaveLength(2);
    expect(tools.map(({ attributes }) => attributes["tool.arguments_summary"])).toEqual([
      '{"request":"first"}',
      '{"request":"second"}',
    ]);
    expect(tools.map(({ attributes }) => attributes["tool.result_summary"])).toEqual([
      '{"response":"first"}',
      '{"response":"second"}',
    ]);
    expect(tools.map(({ endCount }) => endCount)).toEqual([1, 1]);
  });

  it("keeps interleaved events isolated by run and uses context run IDs for tools", async () => {
    const harness = createHarness({ captureContent: true });

    await harness.emit(
      "before_agent_run",
      { prompt: "prompt A" },
      { runId: "run-a" },
    );
    await harness.emit(
      "before_agent_run",
      { prompt: "prompt B" },
      { runId: "run-b" },
    );
    for (const runId of ["run-a", "run-b"]) {
      await harness.emit("model_call_started", {
        callId: "shared-call",
        model: "shared-model",
        provider: "shared-provider",
        runId,
      });
      await harness.emit("llm_input", {
        historyMessages: [],
        model: "shared-model",
        prompt: `${runId} input`,
        provider: "shared-provider",
        runId,
      });
      await harness.emit(
        "before_tool_call",
        {
          params: { runId },
          toolCallId: "shared-tool",
          toolName: "search",
        },
        { runId },
      );
    }

    await harness.emit("llm_output", {
      assistantTexts: ["run-a output"],
      model: "shared-model",
      provider: "shared-provider",
      runId: "run-a",
    });
    await harness.emit("model_call_ended", {
      callId: "shared-call",
      durationMs: 10,
      model: "shared-model",
      outcome: "completed",
      provider: "shared-provider",
      runId: "run-a",
    });
    await harness.emit(
      "after_tool_call",
      {
        result: { value: "run-a result" },
        toolCallId: "shared-tool",
        toolName: "search",
      },
      { runId: "run-a" },
    );

    const runASpans = telemetry.spans.filter(
      ({ attributes }) => attributes["agentscope.run.id"] === "platform-run-1",
    );
    const runBSpans = telemetry.spans.filter(
      ({ attributes }) => attributes["agentscope.run.id"] === "platform-run-2",
    );
    expect(runASpans.filter(({ name }) => name === "llm.call")[0]?.ended).toBe(true);
    expect(runBSpans.filter(({ name }) => name === "llm.call")[0]?.ended).toBe(false);
    expect(runASpans.filter(({ name }) => name === "tool.search")[0]?.ended).toBe(true);
    expect(runBSpans.filter(({ name }) => name === "tool.search")[0]?.ended).toBe(false);
    expect(
      runASpans.filter(({ name }) => name === "tool.search")[0]
        ?.attributes["tool.result_summary"],
    ).toBe('{"value":"run-a result"}');
    expect(
      runBSpans.filter(({ name }) => name === "tool.search")[0]
        ?.attributes["tool.result_summary"],
    ).toBeUndefined();

    await harness.emit("llm_output", {
      assistantTexts: ["run-b output"],
      model: "shared-model",
      provider: "shared-provider",
      runId: "run-b",
    });
    await harness.emit("model_call_ended", {
      callId: "shared-call",
      durationMs: 20,
      model: "shared-model",
      outcome: "completed",
      provider: "shared-provider",
      runId: "run-b",
    });
    await harness.emit(
      "after_tool_call",
      {
        result: { value: "run-b result" },
        toolCallId: "shared-tool",
        toolName: "search",
      },
      { runId: "run-b" },
    );

    expect(runBSpans.filter(({ name }) => name === "llm.call")[0]?.ended).toBe(true);
    expect(runBSpans.filter(({ name }) => name === "tool.search")[0]?.ended).toBe(true);
  });

  it("ends every outstanding child span exactly once when a run ends", async () => {
    const harness = createHarness({ captureContent: true });

    await harness.emit(
      "before_agent_run",
      { prompt: "run prompt" },
      { runId: "run-cleanup" },
    );
    for (let index = 0; index < 2; index += 1) {
      await harness.emit("model_call_started", {
        callId: "shared-call",
        model: "shared-model",
        provider: "shared-provider",
        runId: "run-cleanup",
      });
      await harness.emit(
        "before_tool_call",
        {
          params: { index },
          runId: "run-cleanup",
          toolCallId: "shared-tool",
          toolName: "search",
        },
        { runId: "run-cleanup" },
      );
    }

    await harness.emit("agent_end", {
      messages: [],
      runId: "run-cleanup",
      success: true,
    });

    const children = telemetry.spans.filter(
      ({ attributes, name }) =>
        attributes["agentscope.run.id"] === "platform-run-1" &&
        (name === "llm.call" || name === "tool.search"),
    );
    expect(children).toHaveLength(4);
    expect(children.map(({ endCount }) => endCount)).toEqual([1, 1, 1, 1]);

    await harness.emit("model_call_ended", {
      callId: "shared-call",
      durationMs: 10,
      model: "shared-model",
      outcome: "completed",
      provider: "shared-provider",
      runId: "run-cleanup",
    });
    await harness.emit("after_tool_call", {
      result: {},
      runId: "run-cleanup",
      toolCallId: "shared-tool",
      toolName: "search",
    });
    expect(children.map(({ endCount }) => endCount)).toEqual([1, 1, 1, 1]);
  });
});
