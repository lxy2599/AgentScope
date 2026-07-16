import { useState } from "react";
import { Form, Input, Button, Card, Typography, App } from "antd";
import { ExperimentOutlined } from "@ant-design/icons";
import { useCreateExperiment } from "../../api/hooks";

const { Text } = Typography;

interface Props {
  projectId: string;
  datasetId?: string;
  onCreated?: () => void;
}

export function CreateExperimentPanel({ projectId, datasetId, onCreated }: Props) {
  const create = useCreateExperiment(projectId);
  const [form] = Form.useForm();
  const { message } = App.useApp();

  const handleSubmit = async (values: {
    dataset_id: string;
    target_name: string;
    target_version?: string;
    case_ids: string;
  }) => {
    const ids = values.case_ids
      ? values.case_ids
          .split("\n")
          .map((s) => s.trim())
          .filter(Boolean)
      : [];

    if (ids.length === 0) {
      message.warning("请至少输入一个用例 ID");
      return;
    }

    try {
      await create.mutateAsync({
        dataset_id: values.dataset_id.trim(),
        target_name: values.target_name.trim(),
        target_version: values.target_version?.trim() || undefined,
        case_ids: ids,
      });
      form.resetFields();
      message.success("实验创建成功");
      onCreated?.();
    } catch (err: unknown) {
      message.error(
        err instanceof Error ? err.message : "创建实验失败",
      );
    }
  };

  return (
    <Card
      title={
        <span>
          <ExperimentOutlined style={{ marginRight: 8 }} />
          创建实验
        </span>
      }
    >
      <Form
        form={form}
        layout="vertical"
        onFinish={handleSubmit}
        requiredMark={false}
        initialValues={{ dataset_id: datasetId || "" }}
      >
        <Form.Item
          name="dataset_id"
          label="数据集 ID"
          rules={[{ required: true, message: "请输入数据集 ID" }]}
          help="请填写数据集的 UUID"
        >
          <Input placeholder="d123e4567-e89b-12d3-a456-426614174000" />
        </Form.Item>
        <Form.Item
          name="target_name"
          label="目标 Agent"
          rules={[{ required: true, message: "请输入目标 Agent 名称" }]}
        >
          <Input placeholder="例如：support-agent" />
        </Form.Item>
        <Form.Item name="target_version" label="版本（可选）">
          <Input placeholder="例如：1.0.0" />
        </Form.Item>
        <Form.Item
          name="case_ids"
          label="用例 ID 列表"
          rules={[{ required: true, message: "请至少输入一个用例 ID" }]}
          extra="每行输入一个用例 ID"
        >
          <Input.TextArea
            rows={4}
            placeholder={"case-uuid-1\ncase-uuid-2\ncase-uuid-3"}
          />
        </Form.Item>
        <Button type="primary" htmlType="submit" loading={create.isPending}>
          创建实验
        </Button>
      </Form>
    </Card>
  );
}
