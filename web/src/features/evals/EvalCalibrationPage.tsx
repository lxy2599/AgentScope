import { useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import {
  Alert,
  App,
  Button,
  Card,
  Empty,
  InputNumber,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from "antd";
import { ExperimentOutlined } from "@ant-design/icons";
import {
  useEvalCalibrations,
  useProjectAnnotationItems,
  useRubricTemplates,
  useRubricVersions,
  useRunEvalCalibration,
} from "../../api/hooks";

const { Title, Text } = Typography;

export function EvalCalibrationPage() {
  const { projectId = "" } = useParams<{ projectId: string }>();
  const { message } = App.useApp();
  const [templateId, setTemplateId] = useState("");
  const [versionId, setVersionId] = useState("");
  const [sampleRunIds, setSampleRunIds] = useState<string[]>([]);
  const [threshold, setThreshold] = useState(0.5);
  const { data: templates = [] } = useRubricTemplates(projectId);
  const { data: versions = [] } = useRubricVersions(projectId, templateId);
  const { data: items = [] } = useProjectAnnotationItems(projectId);
  const { data: calibrations = [], isLoading } = useEvalCalibrations(projectId);
  const runCalibration = useRunEvalCalibration(projectId);
  const goldItems = useMemo(() => items.filter((item) => item.is_gold), [items]);

  const submit = async () => {
    if (!versionId || sampleRunIds.length === 0) {
      message.warning("请选择已发布 Rubric 版本和 Gold 样本");
      return;
    }
    try {
      await runCalibration.mutateAsync({
        rubric_version_id: versionId,
        sample_run_ids: sampleRunIds,
        pass_threshold: threshold,
      });
      message.success("校准报告已生成");
    } catch {
      message.error("校准失败，请确认样本拥有对应 Judge 聚合结果");
    }
  };

  return (
    <Space direction="vertical" size="large" style={{ width: "100%" }}>
      <div>
        <Title level={3} style={{ margin: 0 }}>
          <ExperimentOutlined /> Judge 校准
        </Title>
        <Text type="secondary">使用人工 Gold 样本验证固定 Rubric 版本的可信度</Text>
      </div>

      <Card title="创建校准运行" size="small">
        <Space wrap>
          <Select
            style={{ width: 220 }}
            placeholder="Rubric 模板"
            value={templateId || undefined}
            onChange={(value) => {
              setTemplateId(value);
              setVersionId("");
            }}
            options={templates.map((template) => ({ value: template.id, label: template.name }))}
          />
          <Select
            style={{ width: 180 }}
            placeholder="已发布版本"
            value={versionId || undefined}
            onChange={setVersionId}
            options={versions
              .filter((version) => version.status === "published")
              .map((version) => ({ value: version.id, label: `v${version.version}` }))}
          />
          <Select
            mode="multiple"
            style={{ minWidth: 360 }}
            placeholder="选择 Gold Run"
            value={sampleRunIds}
            onChange={setSampleRunIds}
            options={goldItems.map((item) => ({
              value: item.run_id,
              label: `${item.agent_name || "Agent"} · ${item.run_id.slice(0, 8)}`,
            }))}
          />
          <InputNumber
            min={0}
            max={1}
            step={0.05}
            value={threshold}
            onChange={(value) => setThreshold(value ?? 0.5)}
            addonBefore="通过阈值"
          />
          <Button type="primary" loading={runCalibration.isPending} onClick={submit}>
            运行校准
          </Button>
        </Space>
      </Card>

      {calibrations.length === 0 && !isLoading ? (
        <Empty description="暂无校准报告" />
      ) : (
        calibrations.map((calibration) => (
          <Card
            key={calibration.id}
            size="small"
            title={`校准 ${calibration.id.slice(0, 8)}`}
            extra={<Tag color={calibration.status === "completed" ? "success" : "warning"}>{calibration.status}</Tag>}
          >
            <Space style={{ marginBottom: 12 }} wrap>
              <Text>样本 {calibration.report.sample_count ?? 0}</Text>
              <Text>MAE {(calibration.report.mae ?? 0).toFixed(4)}</Text>
              <Text>一致率 {((calibration.report.agreement_rate ?? 0) * 100).toFixed(1)}%</Text>
              <Text>
                Kappa {calibration.report.cohens_kappa == null ? "不可计算" : calibration.report.cohens_kappa.toFixed(4)}
              </Text>
            </Space>
            {(calibration.report.warnings || []).map((warning) => (
              <Alert key={warning} type="warning" showIcon message={warning} style={{ marginBottom: 8 }} />
            ))}
            <Table
              size="small"
              pagination={false}
              rowKey="dimension"
              dataSource={Object.entries(calibration.report.dimensions || {}).map(([dimension, data]) => ({
                dimension,
                ...data,
              }))}
              columns={[
                { title: "维度", dataIndex: "dimension" },
                { title: "样本", dataIndex: "sample_count" },
                { title: "MAE", dataIndex: "mae", render: (value: number) => value.toFixed(4) },
                { title: "偏差", dataIndex: "mean_bias", render: (value: number) => value.toFixed(4) },
                { title: "一致率", dataIndex: "agreement_rate", render: (value: number) => `${(value * 100).toFixed(1)}%` },
                { title: "Kappa", dataIndex: "cohens_kappa", render: (value?: number) => value == null ? "不可计算" : value.toFixed(4) },
              ]}
            />
          </Card>
        ))
      )}
    </Space>
  );
}
