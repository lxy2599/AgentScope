import clsx from "clsx";

type BadgeVariant = "default" | "success" | "warning" | "danger" | "info";

interface BadgeProps {
  variant?: BadgeVariant;
  children: string;
}

const map: Record<BadgeVariant, string> = {
  default: "bg-slate-100 text-slate-700",
  success: "bg-green-100 text-green-700",
  warning: "bg-amber-100 text-amber-700",
  danger: "bg-red-100 text-red-700",
  info: "bg-blue-100 text-blue-700",
};

export function Badge({ variant = "default", children }: BadgeProps) {
  return (
    <span
      className={clsx(
        "inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium",
        map[variant],
      )}
    >
      {children}
    </span>
  );
}

export function statusBadge(status: string): BadgeVariant {
  switch (status) {
    case "running":
    case "queued":
      return "info";
    case "passed":
    case "completed":
    case "succeeded":
      return "success";
    case "failed":
    case "error":
      return "danger";
    default:
      return "default";
  }
}
