import { Alert, Card, Empty, Spin, Table, Tag, Typography } from "antd";
import { useRunSnapshot } from "../../api/hooks";

const { Text } = Typography;

interface DiffRow {
  key: string;
  path: string;
  baseline: unknown;
  candidate: unknown;
}

export function buildSnapshotDiff(
  baseline: Record<string, unknown>,
  candidate: Record<string, unknown>,
): DiffRow[] {
  const rows: DiffRow[] = [];
  walk("", baseline, candidate, rows);
  return rows.slice(0, 200);
}

export function RunSnapshotDiffPanel({
  baselineRunId,
  candidateRunId,
}: {
  baselineRunId: string;
  candidateRunId: string;
}) {
  const baseline = useRunSnapshot(baselineRunId);
  const candidate = useRunSnapshot(candidateRunId);

  if (baseline.isLoading || candidate.isLoading) {
    return <Spin size="small" />;
  }
  if (baseline.error || candidate.error || !baseline.data || !candidate.data) {
    return (
      <Alert
        type="info"
        title="部分运行没有不可变快照，无法比较运行配置"
        showIcon
      />
    );
  }

  const rows = buildSnapshotDiff(
    baseline.data.canonical_payload,
    candidate.data.canonical_payload,
  );
  return (
    <Card
      title="运行配置差异"
      extra={
        <span>
          <Tag>{baseline.data.payload_hash.slice(0, 8)}</Tag>
          <Text type="secondary">→</Text>
          <Tag color="blue">{candidate.data.payload_hash.slice(0, 8)}</Tag>
        </span>
      }
      style={{ marginBottom: 16 }}
    >
      {rows.length === 0 ? (
        <Empty
          image={Empty.PRESENTED_IMAGE_SIMPLE}
          description="两次运行的配置快照一致"
        />
      ) : (
        <Table
          rowKey="key"
          size="small"
          pagination={false}
          dataSource={rows}
          columns={[
            { title: "配置路径", dataIndex: "path", width: "34%" },
            {
              title: "Baseline",
              dataIndex: "baseline",
              render: renderValue,
            },
            {
              title: "Candidate",
              dataIndex: "candidate",
              render: renderValue,
            },
          ]}
        />
      )}
    </Card>
  );
}

function walk(
  path: string,
  baseline: unknown,
  candidate: unknown,
  rows: DiffRow[],
): void {
  if (isRecord(baseline) && isRecord(candidate)) {
    const keys = new Set([...Object.keys(baseline), ...Object.keys(candidate)]);
    for (const key of [...keys].sort()) {
      walk(path ? `${path}.${key}` : key, baseline[key], candidate[key], rows);
    }
    return;
  }
  if (JSON.stringify(baseline) === JSON.stringify(candidate)) return;
  rows.push({ key: path, path, baseline, candidate });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function renderValue(value: unknown) {
  if (value === undefined) return <Text type="secondary">未设置</Text>;
  const formatted =
    typeof value === "string" ? value : JSON.stringify(value, null, 2);
  return (
    <Text code style={{ whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
      {formatted}
    </Text>
  );
}
