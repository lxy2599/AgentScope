let onUnauthorized: (() => void) | undefined;

/** Registers the app-level cleanup invoked when the server rejects a session. */
export function setUnauthorizedHandler(handler?: () => void) {
  onUnauthorized = handler;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export const benchmarkApiPaths = {
  suites: (projectId: string) => `/api/projects/${projectId}/benchmark-suites`,
  catalog: (projectId: string) => `/api/projects/${projectId}/benchmark-catalog`,
  targets: (projectId: string) => `/api/projects/${projectId}/benchmark-targets`,
  workers: (projectId: string) => `/api/projects/${projectId}/benchmark-workers`,
  preflight: (projectId: string) => `/api/projects/${projectId}/benchmark-preflight`,
  experiments: (projectId: string) => `/api/projects/${projectId}/benchmark-experiments`,
  launch: (projectId: string, experimentId: string) =>
    `/api/projects/${projectId}/benchmark-experiments/${experimentId}/launch`,
  report: (projectId: string, experimentId: string) =>
    `/api/projects/${projectId}/benchmark-experiments/${experimentId}/report`,
  cancel: (projectId: string, experimentId: string) =>
    `/api/projects/${projectId}/benchmark-experiments/${experimentId}/cancel`,
  cancellationAudit: (projectId: string, experimentId: string) =>
    `/api/projects/${projectId}/benchmark-experiments/${experimentId}/cancellation-audit`,
};

export async function apiRequest<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const response = await fetch(path, {
    ...options,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(options.headers ?? {}),
    },
  });
  const data = await response.json().catch(() => null);
  if (!response.ok) {
    if (response.status === 401) {
      onUnauthorized?.();
    }
    const err = data?.error;
    throw new ApiError(
      response.status,
      data?.code ?? err?.code ?? "request_failed",
      data?.message ?? err?.message ?? `Request failed with status ${response.status}`,
    );
  }
  return data as T;
}
