import { useCallback, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  Card,
  Row,
  Col,
  Button,
  Table,
  Input,
  Typography,
  Space,
  Tag,
  Spin,
  Empty,
  Modal,
  App,
} from "antd";
import {
  BarChartOutlined,
  RightCircleOutlined,
  KeyOutlined,
  EyeOutlined,
  ExperimentOutlined,
  DatabaseOutlined,
  RobotOutlined,
  CheckSquareOutlined,
} from "@ant-design/icons";
import { useCreateProjectToken, useDatasets, useEvaluators, useExperiments, useAgents } from "../../api/hooks";
import { DatasetsPanel } from "../datasets/DatasetsPanel";
import { CasesTable } from "../datasets/CasesTable";
import { CreateExperimentPanel } from "../experiments/CreateExperimentPanel";
import type { Dataset, Experiment } from "../../api/types";
import { TechnicalId } from "../../components/ui/TechnicalId";
import { evaluatorSummary } from "../../utils/display";

const { Title, Text } = Typography;

export function ProjectHomePage() {
  const { projectId } = useParams<{ projectId: string }>();
  const createToken = useCreateProjectToken(projectId!);
  const { data: datasets, isLoading: datasetsLoading } = useDatasets(projectId!);
  const { data: experiments, isLoading: experimentsLoading } = useExperiments(projectId!);
  const { data: agents } = useAgents(projectId!);
  const { data: evaluators = [] } = useEvaluators(projectId!);

  const [tokenModalOpen, setTokenModalOpen] = useState(false);
  const [tokenDesc, setTokenDesc] = useState("");
  const [newToken, setNewToken] = useState("");
  const [activeDatasetId, setActiveDatasetId] = useState<string | null>(null);
  const [activeDatasetName, setActiveDatasetName] = useState("");

  const navigate = useNavigate();
  const { message } = App.useApp();

  const handleCreateToken = async () => {
    if (!tokenDesc.trim()) {
      message.warning("请输入 Token 描述");
      return;
    }
    try {
      const res = await createToken.mutateAsync({
        description: tokenDesc,
        scopes: ["otlp:write", "runner:write"],
      });
      setNewToken(res.plain_token);
      setTokenDesc("");
      message.success("Token 创建成功");
    } catch {
      message.error("创建 Token 失败");
    }
  };

  const handleDatasetCreated = useCallback((ds: Dataset) => {
    setActiveDatasetId(ds.id);
    setActiveDatasetName(ds.name);
  }, []);

  const handleViewExperiment = useCallback(
    (expId: string) => {
      navigate(`/experiments/${expId}`);
    },
    [navigate],
  );

  const statusTag = (status: string) => {
    const map: Record<string, { color: string; text: string }> = {
      pending: { color: "default", text: "待开始" },
      running: { color: "processing", text: "运行中" },
      completed: { color: "success", text: "已完成" },
      failed: { color: "error", text: "失败" },
      queued: { color: "warning", text: "排队中" },
    };
    const s = map[status] || { color: "default", text: status };
    return <Tag color={s.color}>{s.text}</Tag>;
  };

  const expColumns = [
    {
      title: "目标 Agent",
      dataIndex: "target_name",
      key: "target_name",
      render: (name: string, record: Experiment) => (
        <Space orientation="vertical" size={0}>
          <Text strong>{name}</Text>
          <TechnicalId value={record.id} />
        </Space>
      ),
    },
    {
      title: "版本",
      dataIndex: "target_version",
      key: "target_version",
      render: (v: string | undefined) => v || "—",
    },
    {
      title: "数据集",
      dataIndex: "dataset_id",
      key: "dataset_id",
      render: (id: string) => datasets?.find((dataset) => dataset.id === id)?.name || <TechnicalId value={id} />,
    },
    {
      title: "评测方式",
      dataIndex: "evaluator_id",
      key: "evaluator_id",
      render: (id?: string) => {
        if (!id) return <Text type="secondary">仅执行</Text>;
        const evaluator = (Array.isArray(evaluators) ? evaluators : []).find((item: any) => item.id === id);
        return evaluator ? evaluatorSummary(evaluator.config) : <TechnicalId value={id} />;
      },
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      render: (s: string) => statusTag(s),
    },
    {
      title: "用例数",
      dataIndex: "total_cases",
      key: "total_cases",
    },
    {
      title: "通过",
      dataIndex: "passed_cases",
      key: "passed_cases",
      render: (v: number) => <Text style={{ color: "#52c41a" }}>{v}</Text>,
    },
    {
      title: "失败",
      dataIndex: "failed_cases",
      key: "failed_cases",
      render: (v: number) => <Text type="danger">{v}</Text>,
    },
    {
      title: "创建时间",
      dataIndex: "created_at",
      key: "created_at",
      render: (date: string) => new Date(date).toLocaleString("zh-CN"),
    },
    {
      title: "操作",
      key: "action",
      render: (_: unknown, record: Experiment) => (
        <Button
          type="link"
          icon={<EyeOutlined />}
          onClick={() => handleViewExperiment(record.id)}
        >
          查看报告
        </Button>
      ),
    },
  ];

  return (
      <Space orientation="vertical" size="large" style={{ width: "100%" }}>
        {/* Header */}
        <div>
          <Title level={3} style={{ margin: 0 }}>
            项目工作台
          </Title>
          <Text type="secondary" style={{ fontSize: 13 }}>
            技术 ID：<TechnicalId value={projectId} />
          </Text>
        </div>

        {/* Metrics Row */}
        <Row gutter={16}>
          <Col span={4}>
            <Card>
              <Stat
                icon={<DatabaseOutlined />}
                label="数据集"
                value={datasets?.length ?? 0}
              />
            </Card>
          </Col>
          <Col span={5}>
            <Card>
              <Stat
                icon={<BarChartOutlined />}
                label="实验"
                value={experiments?.length ?? 0}
              />
            </Card>
          </Col>
          <Col span={5}>
            <Card>
              <Stat
                icon={<RobotOutlined />}
                label="Agent"
                value={agents?.length ?? 0}
                action={
                  <Button
                    type="link"
                    icon={<RightCircleOutlined />}
                    onClick={() => navigate(`/projects/${projectId}/agents`)}
                  >
                    管理
                  </Button>
                }
              />
            </Card>
          </Col>
          <Col span={5}>
            <Card>
              <Stat
                icon={<ExperimentOutlined />}
                label="运行追踪"
                value={null}
                action={
                  <Button
                    type="link"
                    icon={<RightCircleOutlined />}
                    onClick={() => navigate(`/projects/${projectId}/runs`)}
                  >
                    查看
                  </Button>
                }
              />
            </Card>
          </Col>
          <Col span={4}>
            <Card>
              <Stat
                icon={<CheckSquareOutlined />}
                label="评测配置"
                value={null}
                action={
                  <Button
                    type="link"
                    icon={<RightCircleOutlined />}
                    onClick={() => navigate(`/projects/${projectId}/metric-config`)}
                  >
                    管理
                  </Button>
                }
              />
            </Card>
          </Col>
          <Col span={4}>
            <Card>
              <Stat
                icon={<KeyOutlined />}
                label="Project Token"
                value={null}
                action={
                  <Button
                    type="link"
                    icon={<KeyOutlined />}
                    onClick={() => setTokenModalOpen(true)}
                  >
                    管理
                  </Button>
                }
              />
            </Card>
          </Col>
        </Row>

        {/* Dataset Creation */}
        <DatasetsPanel projectId={projectId!} onCreated={handleDatasetCreated} />

        {/* Existing Datasets */}
        <Card title="已有数据集">
          {datasetsLoading ? (
            <Spin />
          ) : !datasets || datasets.length === 0 ? (
            <Empty description="暂无数据集，请先创建" image={Empty.PRESENTED_IMAGE_SIMPLE} />
          ) : (
            <Row gutter={[12, 12]}>
              {datasets.map((ds) => (
                <Col span={8} key={ds.id}>
                  <Card
                    size="small"
                    hoverable
                    style={{
                      borderColor: activeDatasetId === ds.id ? "#1677ff" : undefined,
                    }}
                    onClick={() => {
                      setActiveDatasetId(ds.id);
                      setActiveDatasetName(ds.name);
                    }}
                  >
                    <Text strong>{ds.name}</Text>
                    <br />
                    <Text type="secondary" style={{ fontSize: 12 }}>
                      {ds.description || "无描述"}
                    </Text>
                  </Card>
                </Col>
              ))}
            </Row>
          )}
        </Card>

        {/* Cases for active dataset */}
        {activeDatasetId && (
          <Card
            title={
              <span>
                用例管理 — <Text strong>{activeDatasetName}</Text>
              </span>
            }
          >
            <Text type="secondary" style={{ display: "block", marginBottom: 8, fontSize: 12 }}>
              技术 ID：<TechnicalId value={activeDatasetId} />
            </Text>
            <CasesTable datasetId={activeDatasetId} />
          </Card>
        )}

        {/* Experiments */}
        <Card title="实验列表">
          {experimentsLoading ? (
            <Spin />
          ) : !experiments || experiments.length === 0 ? (
            <Empty
              description="暂无实验"
              image={Empty.PRESENTED_IMAGE_SIMPLE}
            />
          ) : (
            <Table
              dataSource={experiments}
              columns={expColumns}
              rowKey="id"
              pagination={false}
              size="small"
            />
          )}
        </Card>

        {/* Create Experiment */}
        <CreateExperimentPanel
          projectId={projectId!}
          datasetId={activeDatasetId ?? undefined}
          onCreated={() => {}}
        />

        {/* Token Modal */}
        <Modal
          title="创建 Project Token"
          open={tokenModalOpen}
          onOk={handleCreateToken}
          onCancel={() => {
            setTokenModalOpen(false);
            setNewToken("");
          }}
          confirmLoading={createToken.isPending}
          okText="创建"
          cancelText="取消"
        >
          <Space orientation="vertical" style={{ width: "100%" }}>
            <Input
              placeholder="Token 描述（如：local-runner）"
              value={tokenDesc}
              onChange={(e) => setTokenDesc(e.target.value)}
            />
            {newToken && (
              <Card size="small" style={{ background: "#fffbe6", borderColor: "#ffe58f" }}>
                <Text strong style={{ color: "#d48806" }}>
                  Token 已创建，请立即复制，关闭后将不再显示：
                </Text>
                <div
                  style={{
                    marginTop: 8,
                    padding: "8px 12px",
                    background: "#fff",
                    borderRadius: 4,
                    fontFamily: "monospace",
                    fontSize: 12,
                    wordBreak: "break-all",
                  }}
                >
                  {newToken}
                </div>
              </Card>
            )}
          </Space>
        </Modal>
      </Space>
  );
}

function Stat({
  icon,
  label,
  value,
  action,
}: {
  icon: React.ReactNode;
  label: string;
  value: number | null;
  action?: React.ReactNode;
}) {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
      <div style={{ fontSize: 24, color: "#1677ff" }}>{icon}</div>
      <div>
        <Text type="secondary" style={{ fontSize: 12, display: "block" }}>
          {label}
        </Text>
        {value !== null ? (
          <Title level={4} style={{ margin: 0 }}>
            {value}
          </Title>
        ) : (
          action
        )}
      </div>
    </div>
  );
}
