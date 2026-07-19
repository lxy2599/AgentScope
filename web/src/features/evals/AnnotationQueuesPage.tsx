import { useState } from "react";
import { useParams } from "react-router-dom";
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
} from "antd";
import {
  ThunderboltOutlined,
  EyeOutlined,
  ArrowUpOutlined,
} from "@ant-design/icons";
import {
  useAnnotationQueues,
  useCreateAnnotationQueue,
  useAnnotationQueueItems,
  useUpdateAnnotationItem,
  usePromoteAnnotationItem,
  useDatasets,
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

export function AnnotationQueuesPage() {
  const { projectId } = useParams<{ projectId: string }>();
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
  const { data: datasets } = useDatasets(projectId!);

  const [editItemId, setEditItemId] = useState<string | null>(null);
  const [editCorrected, setEditCorrected] = useState("");
  const [editToolPath, setEditToolPath] = useState("");
  const [editComment, setEditComment] = useState("");

  const [promoteItemId, setPromoteItemId] = useState<string | null>(null);
  const [promoteDatasetId, setPromoteDatasetId] = useState<string | undefined>(undefined);

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
    setEditToolPath(item.expected_tool_path ?? "");
    setEditComment(item.comment ?? "");
  };

  const handleSaveEdit = async () => {
    if (!editItemId) return;
    try {
      await updateItem.mutateAsync({
        itemId: editItemId,
        corrected_output: editCorrected,
        expected_tool_path: editToolPath,
        comment: editComment,
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

  const columns = [
    {
      title: "Run ID",
      dataIndex: "run_id",
      key: "run_id",
      render: (runId: string) => (
        <Text copyable style={{ fontSize: 12 }}>{runId.slice(0, 8)}...</Text>
      ),
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
      title: "优先级",
      dataIndex: "priority",
      key: "priority",
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
      title: "操作",
      key: "action",
      width: 280,
      render: (_: unknown, record: AnnotationItem) => (
        <Space>
          <Button type="link" size="small" icon={<EyeOutlined />} onClick={() => handleEdit(record)}>
            编辑
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
            <Button
              type="link"
              size="small"
              icon={<ArrowUpOutlined />}
              onClick={() => setPromoteItemId(record.id)}
            >
              提升
            </Button>
          )}
        </Space>
      ),
    },
  ];

  return (
    <Space direction="vertical" size="large" style={{ width: "100%" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
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
          <Empty description="暂无标注队列" image={Empty.PRESENTED_IMAGE_SIMPLE} />
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
            <Empty description="暂无标注项" image={Empty.PRESENTED_IMAGE_SIMPLE} />
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
        <Space direction="vertical" style={{ width: "100%" }}>
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
        <Space direction="vertical" style={{ width: "100%" }}>
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
            <Input
              value={editToolPath}
              onChange={(e) => setEditToolPath(e.target.value)}
              placeholder="可选：期望的工具调用路径（JSON）"
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
        <Space direction="vertical" style={{ width: "100%" }}>
          <Text>选择目标数据集：</Text>
          {!datasets || datasets.length === 0 ? (
            <Empty description="暂无数据集，请先创建" image={Empty.PRESENTED_IMAGE_SIMPLE} />
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
