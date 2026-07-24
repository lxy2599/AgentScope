export const REALTIME_EVENT_TYPES = [
  "agent.registered",
  "agent.heartbeat",
  "agent.status.changed",
  "run.started",
  "run.state.changed",
  "run.semantic.appended",
  "run.completed",
  "run.failed",
  "ingest.accepted",
  "ingest.rejected",
  "eval.completed",
  "quota.warning",
  "quota.exceeded",
] as const;

export type RealtimeEventType = (typeof REALTIME_EVENT_TYPES)[number];
export type RealtimeStatus =
  | "connecting"
  | "live"
  | "reconnecting"
  | "fallback"
  | "closed";

export interface RealtimeEvent {
  id: number;
  schema_version: "realtime-event-v1";
  project_id: string;
  run_id?: string;
  agent_id?: string;
  agent_instance_id?: string;
  type: RealtimeEventType;
  occurred_at: string;
  payload: Record<string, unknown>;
  payload_redacted: boolean;
}

export interface RealtimeCallbacks {
  onEvent(event: RealtimeEvent): void;
  onReset(reason: string): void;
  onStatus(status: RealtimeStatus): void;
}

type EventSourceFactory = (url: string) => EventSource | undefined;

const MAX_SEEN_EVENT_IDS = 1_000;
const MAX_TRANSPORT_ERRORS = 3;

export class ProjectEventStreamClient {
  private source?: EventSource;
  private readonly seenEventIDs = new Set<number>();
  private transportErrors = 0;

  constructor(
    private readonly projectID: string,
    private readonly callbacks: RealtimeCallbacks,
    private readonly createEventSource: EventSourceFactory = (url) =>
      typeof EventSource === "undefined" ? undefined : new EventSource(url),
  ) {}

  start(): void {
    if (this.source || !this.projectID) return;

    this.callbacks.onStatus("connecting");
    const source = this.createEventSource(
      `/api/projects/${encodeURIComponent(this.projectID)}/events/stream`,
    );
    if (!source) {
      this.callbacks.onStatus("fallback");
      return;
    }
    this.source = source;

    source.onopen = () => {
      this.transportErrors = 0;
      this.callbacks.onStatus("live");
    };
    source.onerror = () => {
      this.transportErrors += 1;
      if (this.transportErrors >= MAX_TRANSPORT_ERRORS) {
        source.close();
        this.source = undefined;
        this.callbacks.onStatus("fallback");
        return;
      }
      this.callbacks.onStatus("reconnecting");
    };

    for (const eventType of REALTIME_EVENT_TYPES) {
      source.addEventListener(eventType, (message) => {
        this.handleEvent(message as MessageEvent<string>);
      });
    }
    source.addEventListener("reset", (message) => {
      const payload = parseJSON<{ reason?: string }>(
        (message as MessageEvent<string>).data,
      );
      this.callbacks.onReset(payload?.reason || "unknown");
    });
  }

  close(): void {
    if (!this.source) return;
    this.source.close();
    this.source = undefined;
    this.callbacks.onStatus("closed");
  }

  private handleEvent(message: MessageEvent<string>): void {
    const event = parseJSON<RealtimeEvent>(message.data);
    if (
      !event ||
      event.schema_version !== "realtime-event-v1" ||
      event.project_id !== this.projectID ||
      !REALTIME_EVENT_TYPES.includes(event.type)
    ) {
      return;
    }
    if (this.seenEventIDs.has(event.id)) return;

    this.seenEventIDs.add(event.id);
    if (this.seenEventIDs.size > MAX_SEEN_EVENT_IDS) {
      const oldest = this.seenEventIDs.values().next().value;
      if (oldest !== undefined) this.seenEventIDs.delete(oldest);
    }
    this.callbacks.onEvent(event);
  }
}

function parseJSON<T>(value: string): T | undefined {
  try {
    return JSON.parse(value) as T;
  } catch {
    return undefined;
  }
}

interface HubSubscriber {
  onEvent(event: RealtimeEvent): void;
  onReset(reason: string): void;
  onStatus(status: RealtimeStatus): void;
}

interface ProjectHub {
  client: ProjectEventStreamClient;
  subscribers: Set<HubSubscriber>;
  status: RealtimeStatus;
}

const projectHubs = new Map<string, ProjectHub>();

export function subscribeProjectEvents(
  projectID: string,
  subscriber: HubSubscriber,
): () => void {
  let hub = projectHubs.get(projectID);
  if (!hub) {
    const subscribers = new Set<HubSubscriber>();
    const created: ProjectHub = {
      subscribers,
      status: "connecting",
      client: undefined as unknown as ProjectEventStreamClient,
    };
    created.client = new ProjectEventStreamClient(projectID, {
      onEvent: (event) => {
        for (const item of created.subscribers) item.onEvent(event);
      },
      onReset: (reason) => {
        for (const item of created.subscribers) item.onReset(reason);
      },
      onStatus: (status) => {
        created.status = status;
        for (const item of created.subscribers) item.onStatus(status);
      },
    });
    hub = created;
    projectHubs.set(projectID, hub);
  }

  hub.subscribers.add(subscriber);
  subscriber.onStatus(hub.status);
  hub.client.start();

  return () => {
    const current = projectHubs.get(projectID);
    if (!current) return;
    current.subscribers.delete(subscriber);
    if (current.subscribers.size === 0) {
      current.client.close();
      projectHubs.delete(projectID);
    }
  };
}
