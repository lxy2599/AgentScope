import type { ReactNode } from "react";

export interface RunSignal {
  label: string;
  value: ReactNode;
  tone?: "default" | "success" | "warning" | "danger" | "info";
}

export function RunSignalStrip({ signals }: { signals: RunSignal[] }) {
  return (
    <div className="run-signal-strip" aria-label="运行关键信号">
      {signals.map((signal) => (
        <div className={`run-signal run-signal--${signal.tone ?? "default"}`} key={signal.label}>
          <span className="run-signal__label">{signal.label}</span>
          <strong className="run-signal__value">{signal.value}</strong>
        </div>
      ))}
    </div>
  );
}
