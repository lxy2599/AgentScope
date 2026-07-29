import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  ProjectEventStreamClient,
  REALTIME_EVENT_TYPES,
  type RealtimeEvent,
  type RealtimeStatus,
} from "../realtime";

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  readonly listeners = new Map<string, Array<(event: MessageEvent) => void>>();
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;

  constructor(public readonly url: string) {
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: EventListener) {
    const current = this.listeners.get(type) ?? [];
    current.push(listener as (event: MessageEvent) => void);
    this.listeners.set(type, current);
  }

  emit(type: string, payload: unknown, lastEventId = "") {
    const event = {
      data: JSON.stringify(payload),
      lastEventId,
    } as MessageEvent;
    for (const listener of this.listeners.get(type) ?? []) {
      listener(event);
    }
  }

  close() {
    this.closed = true;
  }
}

describe("ProjectEventStreamClient", () => {
  beforeEach(() => {
    FakeEventSource.instances = [];
  });

  it("opens the project-scoped SSE endpoint and reports live", () => {
    const statuses: RealtimeStatus[] = [];
    const client = new ProjectEventStreamClient(
      "project-1",
      {
        onEvent: vi.fn(),
        onReset: vi.fn(),
        onStatus: (status) => statuses.push(status),
      },
      (url) => new FakeEventSource(url) as unknown as EventSource,
    );

    client.start();
    const source = FakeEventSource.instances[0];
    expect(source.url).toBe("/api/projects/project-1/events/stream");
    expect(statuses).toEqual(["connecting"]);

    source.onopen?.();
    expect(statuses).toEqual(["connecting", "live"]);
  });

  it("listens to every named event and deduplicates by event id", () => {
    const onEvent = vi.fn();
    const client = new ProjectEventStreamClient(
      "project-1",
      { onEvent, onReset: vi.fn(), onStatus: vi.fn() },
      (url) => new FakeEventSource(url) as unknown as EventSource,
    );
    client.start();
    const source = FakeEventSource.instances[0];
    const event: RealtimeEvent = {
      id: 42,
      schema_version: "realtime-event-v1",
      project_id: "project-1",
      run_id: "run-1",
      type: "run.state.changed",
      occurred_at: "2026-07-24T09:00:00Z",
      payload: { status: "thinking" },
      payload_redacted: true,
    };

    source.emit("run.state.changed", event, "42");
    source.emit("run.state.changed", event, "42");

    expect(REALTIME_EVENT_TYPES).toContain("run.state.changed");
    expect(onEvent).toHaveBeenCalledTimes(1);
    expect(onEvent).toHaveBeenCalledWith(event);
  });

  it("resets the EventSource cursor after a server reset", () => {
    const onReset = vi.fn();
    const statuses: RealtimeStatus[] = [];
    const client = new ProjectEventStreamClient(
      "project-1",
      {
        onEvent: vi.fn(),
        onReset,
        onStatus: (status) => statuses.push(status),
      },
      (url) => new FakeEventSource(url) as unknown as EventSource,
    );
    client.start();
    const source = FakeEventSource.instances[0];

    source.emit("reset", { reason: "cursor_expired" });
    expect(onReset).toHaveBeenCalledWith("cursor_expired");
    expect(source.closed).toBe(true);
    expect(FakeEventSource.instances).toHaveLength(2);
    expect(FakeEventSource.instances[1].url).toBe(
      "/api/projects/project-1/events/stream",
    );
    expect(statuses.at(-1)).toBe("connecting");
  });

  it("falls back after repeated transport errors", () => {
    const statuses: RealtimeStatus[] = [];
    const client = new ProjectEventStreamClient(
      "project-1",
      {
        onEvent: vi.fn(),
        onReset: vi.fn(),
        onStatus: (status) => statuses.push(status),
      },
      (url) => new FakeEventSource(url) as unknown as EventSource,
    );
    client.start();
    const source = FakeEventSource.instances[0];

    source.onerror?.();
    source.onerror?.();
    expect(statuses.at(-1)).toBe("reconnecting");
    source.onerror?.();
    expect(statuses.at(-1)).toBe("fallback");
    expect(source.closed).toBe(true);
  });

  it("closes the underlying EventSource", () => {
    const client = new ProjectEventStreamClient(
      "project-1",
      { onEvent: vi.fn(), onReset: vi.fn(), onStatus: vi.fn() },
      (url) => new FakeEventSource(url) as unknown as EventSource,
    );
    client.start();
    const source = FakeEventSource.instances[0];

    client.close();

    expect(source.closed).toBe(true);
  });
});
