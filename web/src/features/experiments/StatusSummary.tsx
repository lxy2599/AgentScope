import { Row, Col, Card, Statistic, Typography } from "antd";
import {
  CheckCircleOutlined,
  CloseCircleOutlined,
  SyncOutlined,
  ClockCircleOutlined,
} from "@ant-design/icons";
import type { ExperimentDetail } from "../../api/types";

const { Text } = Typography;

interface Props {
  detail: ExperimentDetail;
}

export function StatusSummary({ detail }: Props) {
  const { total_cases, status_counts } = detail;
  const queued = status_counts?.queued ?? 0;
  const running = status_counts?.running ?? 0;
  const passed = status_counts?.succeeded ?? status_counts?.passed ?? 0;
  const failed = status_counts?.failed ?? 0;
  const passedPct = total_cases > 0 ? ((passed / total_cases) * 100).toFixed(1) : "0";
  const failedPct = total_cases > 0 ? ((failed / total_cases) * 100).toFixed(1) : "0";

  return (
    <div>
      <Text type="secondary" style={{ marginBottom: 16, display: "block" }}>
        共 {total_cases} 个用例
      </Text>
      <Row gutter={16}>
        <Col span={6}>
          <Card>
            <Statistic
              title="通过"
              value={passed}
              suffix={`/ ${total_cases} (${passedPct}%)`}
              prefix={<CheckCircleOutlined />}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="未通过"
              value={failed}
              suffix={`/ ${total_cases} (${failedPct}%)`}
              prefix={<CloseCircleOutlined />}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="运行中"
              value={running}
              prefix={<SyncOutlined spin={running > 0} />}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="排队中"
              value={queued}
              prefix={<ClockCircleOutlined />}
            />
          </Card>
        </Col>
      </Row>
    </div>
  );
}
