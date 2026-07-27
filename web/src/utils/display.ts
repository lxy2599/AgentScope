export function shortId(value?: string, length = 8): string {
  if (!value) return "—";
  return value.length > length ? value.slice(0, length) : value;
}

export function displayJsonValue(value?: unknown): string {
  if (value === undefined || value === null || value === "") return "—";
  if (typeof value !== "string") {
    const serialized = JSON.stringify(value, null, 2);
    return serialized ?? String(value);
  }
  try {
    const parsed = JSON.parse(value) as unknown;
    if (typeof parsed === "string") return parsed;
    const serialized = JSON.stringify(parsed, null, 2);
    return serialized ?? value;
  } catch {
    return value;
  }
}

export function formatDateTime(value?: string): string {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString("zh-CN");
}

export function evaluatorSummary(config?: string): string {
  if (!config) return "未配置指标";
  try {
    const parsed = JSON.parse(config) as {
      multi_metrics?: Array<{ metric_name?: string; name?: string }>;
      type?: string;
    };
    const names = (parsed.multi_metrics ?? [])
      .map((metric) => metric.metric_name || metric.name)
      .filter(Boolean);
    if (names.length > 0) return names.join(" · ");
    return parsed.type || "规则评测";
  } catch {
    return "规则评测";
  }
}
