import { useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import {
  App,
  Button,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from "antd";
import {
  CheckOutlined,
  MinusCircleOutlined,
  PlusOutlined,
} from "@ant-design/icons";
import {
  useCreateRubricTemplate,
  useCreateRubricVersion,
  usePublishRubricVersion,
  useRubricTemplates,
  useRubricVersions,
} from "../../api/hooks";

const { Title, Text } = Typography;

interface DimensionForm {
  name: string;
  description?: string;
  weight: number;
  pass_threshold: number;
  essential: boolean;
  veto: boolean;
}

export function RubricTemplatesPage() {
  const { projectId = "" } = useParams<{ projectId: string }>();
  const { message } = App.useApp();
  const templates = useRubricTemplates(projectId);
  const createTemplate = useCreateRubricTemplate(projectId);
  const [templateId, setTemplateId] = useState("");
  const selectedTemplateId = templateId || templates.data?.[0]?.id || "";
  const versions = useRubricVersions(projectId, selectedTemplateId);
  const createVersion = useCreateRubricVersion(projectId, selectedTemplateId);
  const publishVersion = usePublishRubricVersion(projectId, selectedTemplateId);
  const [templateOpen, setTemplateOpen] = useState(false);
  const [versionOpen, setVersionOpen] = useState(false);
  const [templateForm] = Form.useForm();
  const [versionForm] = Form.useForm<{ dimensions: DimensionForm[] }>();

  const templateOptions = useMemo(
    () => (templates.data ?? []).map((item) => ({
      value: item.id,
      label: `${item.name} · ${item.description || "无描述"}`,
    })),
    [templates.data],
  );

  async function submitTemplate(values: { name: string; description?: string }) {
    try {
      const created = await createTemplate.mutateAsync(values);
      setTemplateId(created.id);
      setTemplateOpen(false);
      templateForm.resetFields();
      message.success("Rubric 模板已创建");
    } catch (error) {
      message.error(error instanceof Error ? error.message : "创建失败");
    }
  }

  async function submitVersion(values: { dimensions: DimensionForm[] }) {
    try {
      await createVersion.mutateAsync({ dimensions: values.dimensions });
      setVersionOpen(false);
      versionForm.resetFields();
      message.success("草稿版本已创建");
    } catch (error) {
      message.error(error instanceof Error ? error.message : "版本校验失败");
    }
  }

  return (
    <Space orientation="vertical" size="large" style={{ width: "100%" }}>
      <div>
        <Title level={3} style={{ margin: 0 }}>Rubric 模板</Title>
        <Text type="secondary">
          发布版本不可修改。指标和 Judge 应固定引用具体版本，保证评测可复现。
        </Text>
      </div>

      <Space wrap>
        <Select
          aria-label="Rubric 模板"
          value={selectedTemplateId || undefined}
          options={templateOptions}
          placeholder="选择模板"
          style={{ minWidth: 320 }}
          onChange={setTemplateId}
        />
        <Button icon={<PlusOutlined />} onClick={() => setTemplateOpen(true)}>
          新建模板
        </Button>
        <Button
          type="primary"
          icon={<PlusOutlined />}
          disabled={!selectedTemplateId}
          onClick={() => {
            versionForm.setFieldsValue({
              dimensions: [{
                name: "quality",
                description: "",
                weight: 1,
                pass_threshold: 0.7,
                essential: false,
                veto: false,
              }],
            });
            setVersionOpen(true);
          }}
        >
          新建版本
        </Button>
      </Space>

      {!selectedTemplateId ? (
        <Empty description="先创建一个 Rubric 模板" />
      ) : (
        <Table
          rowKey="id"
          loading={versions.isLoading}
          dataSource={versions.data ?? []}
          pagination={false}
          columns={[
            { title: "版本", dataIndex: "version", render: (value) => `v${value}` },
            {
              title: "状态",
              dataIndex: "status",
              render: (status) => (
                <Tag color={status === "published" ? "success" : "default"}>
                  {status === "published" ? "已发布" : "草稿"}
                </Tag>
              ),
            },
            {
              title: "维度",
              dataIndex: "rubric",
              render: (value: string) => {
                try {
                  return JSON.parse(value).dimensions
                    .map((item: DimensionForm) => `${item.name} ${item.weight}`)
                    .join(" · ");
                } catch {
                  return "配置不可解析";
                }
              },
            },
            {
              title: "创建时间",
              dataIndex: "created_at",
              render: (value) => new Date(value).toLocaleString("zh-CN"),
            },
            {
              title: "操作",
              render: (_, record) => record.status === "draft" ? (
                <Button
                  icon={<CheckOutlined />}
                  loading={publishVersion.isPending}
                  onClick={async () => {
                    try {
                      await publishVersion.mutateAsync(record.id);
                      message.success(`v${record.version} 已发布`);
                    } catch (error) {
                      message.error(error instanceof Error ? error.message : "发布失败");
                    }
                  }}
                >
                  发布
                </Button>
              ) : <Text type="secondary">只读</Text>,
            },
          ]}
        />
      )}

      <Modal
        title="新建 Rubric 模板"
        open={templateOpen}
        onCancel={() => setTemplateOpen(false)}
        onOk={templateForm.submit}
        confirmLoading={createTemplate.isPending}
      >
        <Form form={templateForm} layout="vertical" onFinish={submitTemplate}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input placeholder="例如：客服回答质量" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="新建 Rubric 版本"
        open={versionOpen}
        width={900}
        onCancel={() => setVersionOpen(false)}
        onOk={versionForm.submit}
        confirmLoading={createVersion.isPending}
      >
        <Form form={versionForm} layout="vertical" onFinish={submitVersion}>
          <Form.List name="dimensions">
            {(fields, { add, remove }) => (
              <Space orientation="vertical" style={{ width: "100%" }}>
                {fields.map(({ key, name, ...rest }) => (
                  <Space key={key} align="start" wrap>
                    <Form.Item {...rest} name={[name, "name"]} label="维度" rules={[{ required: true }]}>
                      <Input style={{ width: 150 }} />
                    </Form.Item>
                    <Form.Item {...rest} name={[name, "weight"]} label="权重" rules={[{ required: true }]}>
                      <InputNumber min={0.01} max={1} step={0.05} />
                    </Form.Item>
                    <Form.Item {...rest} name={[name, "pass_threshold"]} label="阈值" rules={[{ required: true }]}>
                      <InputNumber min={0} max={1} step={0.05} />
                    </Form.Item>
                    <Form.Item {...rest} name={[name, "essential"]} label="必需" valuePropName="checked">
                      <Switch />
                    </Form.Item>
                    <Form.Item {...rest} name={[name, "veto"]} label="否决" valuePropName="checked">
                      <Switch />
                    </Form.Item>
                    <Button
                      aria-label="删除维度"
                      type="text"
                      danger
                      icon={<MinusCircleOutlined />}
                      onClick={() => remove(name)}
                    />
                  </Space>
                ))}
                <Button
                  block
                  type="dashed"
                  icon={<PlusOutlined />}
                  onClick={() => add({
                    name: "",
                    weight: 0.5,
                    pass_threshold: 0.7,
                    essential: false,
                    veto: false,
                  })}
                >
                  添加维度
                </Button>
              </Space>
            )}
          </Form.List>
        </Form>
      </Modal>
    </Space>
  );
}

