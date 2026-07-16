import { useState } from "react";
import { Card, Form, Input, Button, Modal, Typography, App } from "antd";
import { PlusOutlined, DatabaseOutlined } from "@ant-design/icons";
import { useCreateDataset } from "../../api/hooks";
import type { Dataset } from "../../api/types";

const { Text } = Typography;

interface Props {
  projectId: string;
  onCreated: (ds: Dataset) => void;
}

export function DatasetsPanel({ projectId, onCreated }: Props) {
  const createDataset = useCreateDataset(projectId);
  const [modalOpen, setModalOpen] = useState(false);
  const [form] = Form.useForm();
  const { message } = App.useApp();

  const handleCreate = async (values: { name: string; description?: string }) => {
    try {
      const ds = await createDataset.mutateAsync(values);
      form.resetFields();
      setModalOpen(false);
      message.success("数据集创建成功");
      onCreated(ds);
    } catch {
      message.error("创建数据集失败");
    }
  };

  return (
    <>
      <Card
        title={
          <span>
            <DatabaseOutlined style={{ marginRight: 8 }} />
            数据集
          </span>
        }
        extra={
          <Button
            type="primary"
            size="small"
            icon={<PlusOutlined />}
            onClick={() => setModalOpen(true)}
          >
            创建数据集
          </Button>
        }
      >
        <Text type="secondary" style={{ fontSize: 13 }}>
          数据集是评测用例的集合，创建数据集后可在工作台中添加具体用例。
        </Text>
      </Card>

      <Modal
        title="创建数据集"
        open={modalOpen}
        onOk={() => form.submit()}
        onCancel={() => {
          form.resetFields();
          setModalOpen(false);
        }}
        confirmLoading={createDataset.isPending}
        okText="创建"
        cancelText="取消"
      >
        <Form form={form} layout="vertical" onFinish={handleCreate} requiredMark={false}>
          <Form.Item
            name="name"
            label="数据集名称"
            rules={[{ required: true, message: "请输入数据集名称" }]}
          >
            <Input placeholder="例如：客服测试用例" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} placeholder="可选，描述数据集的用途" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
