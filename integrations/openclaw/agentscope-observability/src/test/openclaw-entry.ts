export type EventContext = {
  runId?: string;
  sessionId?: string;
};

export type OpenClawEvents = {
  gateway_start: Record<string, never>;
  before_agent_run: {
    prompt: string;
  };
  agent_end: {
    runId?: string;
    success: boolean;
    error?: string;
    messages: unknown[];
  };
  gateway_stop: Record<string, never>;
  model_call_started: {
    runId: string;
    callId: string;
    provider: string;
    model: string;
  };
  llm_input: {
    runId: string;
    provider: string;
    model: string;
    prompt: string;
    systemPrompt?: string;
    historyMessages: unknown[];
  };
  llm_output: {
    runId: string;
    provider: string;
    model: string;
    assistantTexts: string[];
    usage?: {
      input?: number;
      output?: number;
      total?: number;
    };
  };
  model_call_ended: {
    runId: string;
    callId: string;
    provider: string;
    model: string;
    durationMs: number;
    outcome: string;
    errorCategory?: string;
  };
  before_tool_call: {
    runId?: string;
    toolCallId?: string;
    toolName: string;
    params: unknown;
  };
  after_tool_call: {
    runId?: string;
    toolCallId?: string;
    toolName: string;
    result: unknown;
    durationMs?: number;
    error?: string;
  };
};

export type OpenClawPluginApi = {
  pluginConfig: unknown;
  logger: {
    warn(message: string): void;
  };
  on<K extends keyof OpenClawEvents>(
    event: K,
    handler: (
      event: OpenClawEvents[K],
      context: EventContext,
    ) => void | Promise<void>,
  ): void;
};

export function definePluginEntry<T>(entry: T): T {
  return entry;
}
