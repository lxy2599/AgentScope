import { useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import {
  Card,
  Table,
  Button,
  Select,
  Modal,
  Input,
  Space,
  Typography,
  Tag,
  App,
  Empty,
  Spin,
  Tooltip,
  Switch,
  InputNumber,
} from "antd";
import {
  ThunderboltOutlined,
  EyeOutlined,
  ArrowUpOutlined,
  LinkOutlined,
  DeleteOutlined,
  PlusOutlined,
} from "@ant-design/icons";
import {
  useAnnotationQueues,
  useCreateAnnotationQueue,
  useAnnotationQueueItems,
  useUpdateAnnotationItem,
  usePromoteAnnotationItem,
  useDatasets,
  useSubmitAnnotationGold,
} from "../../api/hooks";
import type { AnnotationItem } from "../../api/types";

const { Title, Text } = Typography;
const { TextArea } = Input;

const STATUS_MAP: Record<string, { color: string; text: string }> = {
  pending: { color: "default", text: "待处理" },
  in_progress: { color: "processing", text: "处理中" },
  completed: { color: "success", text: "已完成" },
  skipped: { color: "warning", text: "已跳过" },
  promoted: { color: "purple", text: "已提升" },
};

const FAILURE_CATEGORIES = [
  { value: "hallucination", label: "事实幻觉" },
  { value: "safety", label: "安全问题" },
  { value: "tool_error", label: "工具调用错误" },
  { value: "retrieval_error", label: "检索错误" },
  { value: "instruction_following", label: "未遵循指令" },
  { value: "other", label: "其他" },
];

interface GoldScoreEntry {
  key: string;
  dimension: string;
  score: number;
}

export function AnnotationQueuesPage() {
  const { projectId } = useParams<{ projectId: string }>();
  const navigate = useNavigate();
  const { message } = App.useApp();
  const { data: queues, isLoading: queuesLoading } = useAnnotationQueues(projectId!);
  const createQueue = useCreateAnnotationQueue(projectId!);

  const [activeQueueId, setActiveQueueId] = useState<string | null>(null);
  const [queueModalOpen, setQueueModalOpen] = useState(false);
  const [queueName, setQueueName] = useState("");
  const [queueDesc, setQueueDesc] = useState("");

  const { data: items, isLoading: itemsLoading } = useAnnotationQueueItems(activeQueueId ?? "");
  const updateItem = useUpdateAnnotationItem();
  const promoteItem = usePromoteAnnotationItem();
  const submitGold = useSubmitAnnotationGold();
  const { data: datasets } = useDatasets(projectId!);

  const [editItemId, setEditItemId] = useState<string | null>(null);
  const [editCorrected, setEditCorrected] = useState("");
  const [editToolPath, setEditToolPath] = useState<string[]>([]);
  const [editComment, setEditComment] = useState("");
  const [editFailureCategory, setEditFailureCategory] = useState("");
  const [editPrivacyReviewed, setEditPrivacyReviewed] = useState(false);
  const [editReviewerNote, setEditReviewerNote] = useState("");

  const [promoteItemId, setPromoteItemId] = useState<string | null>(null);
  const [promoteDatasetId, setPromoteDatasetId] = useState<string | undefined>(undefined);
  const [goldItemId, setGoldItemId] = useState<string | null>(null);
  const [goldScores, setGoldScores] = useState<GoldScoreEntry[]>([
    { key: "quality", dimension: "quality", score: 1 },
    { key: "safety", dimension: "safety", score: 1 },
  ]);

  const handleCreateQueue = async () => {
    if (!queueName.trim()) {
      message.warning("请输入队列名称");
      return;
    }
    try {
      const q = await createQueue.mutateAsync({ name: queueName, description: queueDesc });
      setActiveQueueId(q.id);
      setQueueModalOpen(false);
      setQueueName("");
      setQueueDesc("");
      message.success("队列创建成功");
    } catch {
      message.error("创建失败");
    }
  };

  const handleEdit = (item: AnnotationItem) => {
    setEditItemId(item.id);
    setEditCorrected(item.corrected_output ?? "");
    try {
      const parsed = JSON.parse(item.expected_tool_path || "[]") as unknown;
      setEditToolPath(Array.isArray(parsed) ? parsed.map(String) : []);
    } catch {
      setEditToolPath(item.expected_tool_path ? [item.expected_tool_path] : []);
    }
    setEditComment(item.comment ?? "");
    setEditFailureCategory(item.failure_category ?? "");
    setEditPrivacyReviewed(item.privacy_reviewed);
    setEditReviewerNote(item.reviewer_note ?? "");
  };

  const handleSaveEdit = async () => {
    if (!editItemId) return;
    try {
      await updateItem.mutateAsync({
        itemId: editItemId,
        corrected_output: editCorrected,
        expected_tool_path: editToolPath.length > 0 ? JSON.stringify(editToolPath) : "",
        comment: editComment,
        failure_category: editFailureCategory,
        privacy_reviewed: editPrivacyReviewed,
        reviewer_note: editReviewerNote,
      });
      message.success("标注已保存");
      setEditItemId(null);
    } catch {
      message.error("保存失败");
    }
  };

  const handlePromote = async () => {
    if (!promoteItemId || !promoteDatasetId) {
      message.warning("请选择数据集");
      return;
    }
    try {
      await promoteItem.mutateAsync({ itemId: promoteItemId, datasetId: promoteDatasetId });
      message.success("已提升为数据集用例");
      setPromoteItemId(null);
      setPromoteDatasetId(undefined);
    } catch {
      message.error("提升失败");
    }
  };

  const handleStatusChange = async (itemId: string, status: string) => {
    try {
      await updateItem.mutateAsync({ itemId, status });
      message.success("状态已更新");
    } catch {
      message.error("状态更新失败");
    }
  };

  const handleSubmitGold = async () => {
    if (!goldItemId) return;
    try {
      const normalized = goldScores.map((entry) => ({ ...entry, dimension: entry.dimension.trim() }));
      if (normalized.length === 0 || normalized.some((entry) => !entry.dimension)) {
        message.warning("请填写至少一个评测维度");
        return;
      }
      if (new Set(normalized.map((entry) => entry.dimension)).size !== normalized.length) {
        message.warning("评测维度名称不能重复");
        return;
      }
      const scores = Object.fromEntries(normalized.map((entry) => [entry.dimension, entry.score]));
      await submitGold.mutateAsync({ itemId: goldItemId, goldScores: scores });
      message.success("Gold 标注已提交");
      setGoldItemId(null);
    } catch {
      message.error("Gold 分数提交失败，请检查每个维度是否在 0 到 1 之间");
    }
  };

  const handleOpenGold = (item: AnnotationItem) => {
    let scores: Record<string, number> = { quality: 1, safety: 1 };
    try {
      const parsed = JSON.parse(item.gold_scores || "{}") as Record<string, number>;
      if (Object.keys(parsed).length > 0) scores = parsed;
    } catch {
      // Use the default dimensions for legacy malformed values.
    }
    setGoldScores(Object.entries(scores).map(([dimension, score]) => ({ key: dimension, dimension, score })));
    setGoldItemId(item.id);
  };

  const columns = [
    {
      title: "Agent",
      dataIndex: "agent_name",
      key: "agent_name",
      render: (v: string | undefined) => v || "—",
    },
    {
      title: "输入摘要",
      dataIndex: "input_summary",
      key: "input_summary",
      ellipsis: true,
      render: (v: string | undefined) => v || "—",
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      render: (status: string) => {
        const s = STATUS_MAP[status] || { color: "default", text: status };
        return <Tag color={s.color}>{s.text}</Tag>;
      },
    },
    {
      title: "隐私审核",
      dataIndex: "privacy_reviewed",
      key: "privacy_reviewed",
      render: (v: boolean) =>
        v ? (
          <Tag color="success">已审核</Tag>
        ) : (
          <Tag color="warning">未审核</Tag>
        ),
    },
    {
      title: "失败分类",
      dataIndex: "failure_category",
      key: "failure_category",
      render: (v: string | undefined) => v || "—",
    },
    {
      title: "修正输出",
      dataIndex: "corrected_output",
      key: "corrected_output",
      ellipsis: true,
      render: (v: string | undefined) => v || "—",
    },
    {
      title: "备注",
      dataIndex: "comment",
      key: "comment",
      ellipsis: true,
      render: (v: string | undefined) => v || "—",
    },
    {
      title: "Gold",
      dataIndex: "is_gold",
      key: "is_gold",
      render: (value: boolean, record: AnnotationItem) =>
        value ? <Tag color="gold">已标注</Tag> : (
          <Button type="link" size="small" onClick={() => handleOpenGold(record)}>
            标为 Gold
          </Button>
        ),
    },
    {
      title: "操作",
      key: "action",
      width: 340,
      render: (_: unknown, record: AnnotationItem) => (
        <Space>
          <Button
            type="link"
            size="small"
            icon={<EyeOutlined />}
            onClick={() => handleEdit(record)}
          >
            编辑
          </Button>
          <Button
            type="link"
            size="small"
            icon={<LinkOutlined />}
            onClick={() => navigate(`/runs/${record.run_id}`)}
          >
            查看运行
          </Button>
          <Select
            size="small"
            style={{ width: 100 }}
            value={record.status}
            onChange={(val) => handleStatusChange(record.id, val)}
            options={[
              { value: "pending", label: "待处理" },
              { value: "in_progress", label: "处理中" },
              { value: "completed", label: "已完成" },
              { value: "skipped", label: "跳过" },
            ]}
          />
          {record.status !== "promoted" && (
            <Tooltip
              title={
                !record.privacy_reviewed
                  ? "请先完成隐私审核再提升"
                  : undefined
              }
            >
              <Button
                type="link"
                size="small"
                icon={<ArrowUpOutlined />}
                onClick={() => setPromoteItemId(record.id)}
                disabled={!record.privacy_reviewed}
              >
                提升
              </Button>
            </Tooltip>
          )}
        </Space>
      ),
    },
  ];

  return (
    <Space orientation="vertical" size="large" style={{ width: "100%" }}>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
        }}
      >
        <div>
          <Title level={3} style={{ margin: 0 }}>
            <ThunderboltOutlined /> 标注队列
          </Title>
          <Text type="secondary">查看和处理线上评测产生的待标注数据</Text>
        </div>
        <Button type="primary" onClick={() => setQueueModalOpen(true)}>
          创建队列
        </Button>
      </div>

      <Card size="small" title="选择队列">
        {queuesLoading ? (
          <Spin />
        ) : !queues || queues.length === 0 ? (
          <Empty
            description="暂无标注队列"
            image={Empty.PRESENTED_IMAGE_SIMPLE}
          />
        ) : (
          <Space wrap>
            {queues.map((q) => (
              <Button
                key={q.id}
                type={activeQueueId === q.id ? "primary" : "default"}
                onClick={() => setActiveQueueId(q.id)}
              >
                {q.name}
              </Button>
            ))}
          </Space>
        )}
      </Card>

      {activeQueueId && (
        <Card title="标注项列表">
          {itemsLoading ? (
            <Spin />
          ) : !items || items.length === 0 ? (
            <Empty
              description="暂无标注项"
              image={Empty.PRESENTED_IMAGE_SIMPLE}
            />
          ) : (
            <Table
              dataSource={items}
              columns={columns}
              rowKey="id"
              pagination={{ pageSize: 10 }}
              size="small"
            />
          )}
        </Card>
      )}

      <Modal
        title="创建标注队列"
        open={queueModalOpen}
        onOk={handleCreateQueue}
        onCancel={() => setQueueModalOpen(false)}
        confirmLoading={createQueue.isPending}
        okText="创建"
        cancelText="取消"
      >
        <Space orientation="vertical" style={{ width: "100%" }}>
          <div>
            <Text>队列名称</Text>
            <Input
              placeholder="如：线上评测标注"
              value={queueName}
              onChange={(e) => setQueueName(e.target.value)}
            />
          </div>
          <div>
            <Text>描述</Text>
            <Input
              placeholder="可选"
              value={queueDesc}
              onChange={(e) => setQueueDesc(e.target.value)}
            />
          </div>
        </Space>
      </Modal>

      <Modal
        title="编辑标注"
        open={!!editItemId}
        onOk={handleSaveEdit}
        onCancel={() => setEditItemId(null)}
        confirmLoading={updateItem.isPending}
        okText="保存"
        cancelText="取消"
        width={640}
      >
        <Space orientation="vertical" style={{ width: "100%" }}>
          <div>
            <Text strong>修正输出</Text>
            <TextArea
              rows={3}
              value={editCorrected}
              onChange={(e) => setEditCorrected(e.target.value)}
              placeholder="输入修正后的预期输出"
            />
          </div>
          <div>
            <Text strong>预期工具路径</Text>
            <Select
              mode="tags"
              style={{ width: "100%" }}
              value={editToolPath}
              onChange={setEditToolPath}
              tokenSeparators={[","]}
              placeholder="按调用顺序输入工具名称，回车添加"
            />
            <Text type="secondary" style={{ fontSize: 12 }}>标签顺序就是期望的工具调用顺序。</Text>
          </div>
          <div>
            <Text strong>失败分类</Text>
            <Select
              allowClear
              showSearch
              style={{ width: "100%" }}
              value={editFailureCategory || undefined}
              onChange={(value) => setEditFailureCategory(value || "")}
              options={FAILURE_CATEGORIES}
              placeholder="选择最主要的失败原因"
            />
          </div>
          <div>
            <Text strong>隐私审核完成</Text>
            <div style={{ marginTop: 4 }}>
              <Switch
                checked={editPrivacyReviewed}
                onChange={setEditPrivacyReviewed}
                checkedChildren="已审核"
                unCheckedChildren="未审核"
              />
              {!editPrivacyReviewed && (
                <Text type="warning" style={{ marginLeft: 8, fontSize: 12 }}>
                  隐私未审核时无法提升为数据集用例
                </Text>
              )}
            </div>
          </div>
          <div>
            <Text strong>审核者备注</Text>
            <TextArea
              rows={2}
              value={editReviewerNote}
              onChange={(e) => setEditReviewerNote(e.target.value)}
              placeholder="添加审核备注"
            />
          </div>
          <div>
            <Text strong>备注</Text>
            <TextArea
              rows={2}
              value={editComment}
              onChange={(e) => setEditComment(e.target.value)}
              placeholder="添加备注说明"
            />
          </div>
        </Space>
      </Modal>

      <Modal
        title="提交人工 Gold 分数"
        open={!!goldItemId}
        onOk={handleSubmitGold}
        onCancel={() => setGoldItemId(null)}
        confirmLoading={submitGold.isPending}
        okText="提交"
        cancelText="取消"
      >
        <Text type="secondary">按评测维度填写 0 到 1 的人工标准分。</Text>
        <Space orientation="vertical" style={{ width: "100%", marginTop: 12 }}>
          {goldScores.map((entry, index) => (
            <Space key={entry.key} style={{ width: "100%" }} align="start">
              <Input
                aria-label={`Gold 维度 ${index + 1}`}
                value={entry.dimension}
                placeholder="维度名称"
                onChange={(event) => setGoldScores((previous) => previous.map((item) =>
                  item.key === entry.key ? { ...item, dimension: event.target.value } : item))}
                style={{ width: 240 }}
              />
              <InputNumber
                aria-label={`${entry.dimension || `维度 ${index + 1}`}分数`}
                min={0}
                max={1}
                step={0.05}
                value={entry.score}
                onChange={(value) => setGoldScores((previous) => previous.map((item) =>
                  item.key === entry.key ? { ...item, score: value ?? 0 } : item))}
                style={{ width: 130 }}
              />
              <Button
                aria-label="删除维度"
                icon={<DeleteOutlined />}
                disabled={goldScores.length === 1}
                onClick={() => setGoldScores((previous) => previous.filter((item) => item.key !== entry.key))}
              />
            </Space>
          ))}
          <Button
            type="dashed"
            icon={<PlusOutlined />}
            onClick={() => setGoldScores((previous) => [...previous, {
              key: `dimension-${Date.now()}`,
              dimension: "",
              score: 1,
            }])}
          >
            添加评测维度
          </Button>
        </Space>
      </Modal>

      <Modal
        title="提升为数据集用例"
        open={!!promoteItemId}
        onOk={handlePromote}
        onCancel={() => {
          setPromoteItemId(null);
          setPromoteDatasetId(undefined);
        }}
        confirmLoading={promoteItem.isPending}
        okText="提升"
        cancelText="取消"
      >
        <Space orientation="vertical" style={{ width: "100%" }}>
          <Text>选择目标数据集：</Text>
          {!datasets || datasets.length === 0 ? (
            <Empty
              description="暂无数据集，请先创建"
              image={Empty.PRESENTED_IMAGE_SIMPLE}
            />
          ) : (
            <Select
              style={{ width: "100%" }}
              placeholder="选择数据集"
              value={promoteDatasetId}
              onChange={setPromoteDatasetId}
              options={datasets.map((ds) => ({
                value: ds.id,
                label: ds.name,
              }))}
            />
          )}
        </Space>
      </Modal>
    </Space>
  );
}
