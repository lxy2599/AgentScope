import type { BenchmarkExecutionKind } from "../../api/types";

export const EXECUTION_KIND_LABELS: Record<string, string> = {
  function: "函数目标",
  harbor: "Harbor 容器环境",
  tau2: "τ² 模拟环境",
  browsergym: "BrowserGym 浏览器环境",
  inspect: "Inspect 安全环境",
};

export function executionKindLabel(kind?: BenchmarkExecutionKind) {
  if (!kind) return "未声明执行环境";
  return EXECUTION_KIND_LABELS[kind] ?? kind;
}

export function displayMetricValue(value: number | string | boolean | null | undefined) {
  if (value === null || value === undefined) return "—";
  if (typeof value === "number") return Number.isInteger(value) ? String(value) : value.toFixed(3);
  if (typeof value === "boolean") return value ? "是" : "否";
  return value;
}

export function formatPercent(value?: number) {
  if (value === undefined || value === null) return "—";
  const normalized = value <= 1 ? value * 100 : value;
  return `${normalized.toFixed(1)}%`;
}

export function shortCommit(commit?: string) {
  return commit ? commit.slice(0, 10) : "未固定";
}
