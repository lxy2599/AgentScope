import { ArrowDownOutlined, ArrowUpOutlined, MinusOutlined } from "@ant-design/icons";

interface MetricDeltaProps {
  value: string | number;
  delta?: number | null;
  unit?: string;
  inverse?: boolean;
  baselineLabel?: string;
}

export function MetricDelta({ value, delta, unit = "", inverse = false, baselineLabel }: MetricDeltaProps) {
  const neutral = delta === null || delta === undefined || delta === 0;
  const positive = !neutral && (inverse ? delta! < 0 : delta! > 0);
  const Icon = neutral ? MinusOutlined : delta! > 0 ? ArrowUpOutlined : ArrowDownOutlined;
  return (
    <div className="metric-delta">
      <div className="metric-delta__value">{value}{unit}</div>
      {delta !== null && delta !== undefined && (
        <div className={`metric-delta__change ${neutral ? "is-neutral" : positive ? "is-positive" : "is-negative"}`}>
          <Icon /> {Math.abs(delta).toFixed(1)}% {baselineLabel && <span>较{baselineLabel}</span>}
        </div>
      )}
    </div>
  );
}
