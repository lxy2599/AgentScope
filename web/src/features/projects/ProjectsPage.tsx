import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Card, Table, Modal, Form, Input, Button, Space, Empty, Spin, Typography, App } from "antd";
import { PlusOutlined, RightCircleOutlined } from "@ant-design/icons";
import { useCreateProject, useProjects } from "../../api/hooks";

const { Title } = Typography;

export function ProjectsPage() {
  const { data: projects, isLoading } = useProjects();
  const createProject = useCreateProject();
  const navigate = useNavigate();
  const [modalOpen, setModalOpen] = useState(false);
  const [form] = Form.useForm();
  const { message } = App.useApp();

  const handleCreate = async (values: { name: string; description?: string }) => {
    try {
      const res = await createProject.mutateAsync(values);
      form.resetFields();
      setModalOpen(false);
      message.success("项目创建成功");
      navigate(`/projects/${res.id}`);
    } catch {
      message.error("创建项目失败");
    }
  };

  const columns = [
    {
      title: "项目名称",
      dataIndex: "name",
      key: "name",
      render: (name: string, record: { id: string }) => (
        <Button
          type="link"
          style={{ padding: 0 }}
          onClick={() => navigate(`/projects/${record.id}`)}
        >
          {name}
        </Button>
      ),
    },
    {
      title: "描述",
      dataIndex: "description",
      key: "description",
      render: (desc: string | undefined) => desc || "—",
    },
    {
      title: "创建时间",
      dataIndex: "created_at",
      key: "created_at",
      render: (date: string | undefined) =>
        date ? new Date(date).toLocaleDateString("zh-CN") : "—",
    },
    {
      title: "操作",
      key: "action",
      render: (_: unknown, record: { id: string }) => (
        <Button
          type="primary"
          ghost
          size="small"
          icon={<RightCircleOutlined />}
          onClick={() => navigate(`/projects/${record.id}`)}
        >
          进入工作台
        </Button>
      ),
    },
  ];

  return (
      <Space orientation="vertical" size="large" style={{ width: "100%" }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <Title level={3} style={{ margin: 0 }}>
            项目管理
          </Title>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => setModalOpen(true)}
          >
            创建项目
          </Button>
        </div>

        {isLoading ? (
          <div style={{ textAlign: "center", padding: 48 }}>
            <Spin size="large" />
            <div style={{ marginTop: 16, color: "#999" }}>加载中...</div>
          </div>
        ) : projects && projects.length > 0 ? (
          <Card>
            <Table
              dataSource={projects}
              columns={columns}
              rowKey="id"
              pagination={false}
              onRow={(record) => ({
                onClick: () => navigate(`/projects/${record.id}`),
                style: { cursor: "pointer" },
              })}
            />
          </Card>
        ) : (
          <Card>
            <Empty
              description="暂无项目"
              image={Empty.PRESENTED_IMAGE_SIMPLE}
            >
              <Button
                type="primary"
                icon={<PlusOutlined />}
                onClick={() => setModalOpen(true)}
              >
                创建第一个项目
              </Button>
            </Empty>
          </Card>
        )}

        <Modal
          title="创建项目"
          open={modalOpen}
          onOk={() => form.submit()}
          onCancel={() => {
            form.resetFields();
            setModalOpen(false);
          }}
          confirmLoading={createProject.isPending}
          okText="创建"
          cancelText="取消"
        >
          <Form
            form={form}
            layout="vertical"
            onFinish={handleCreate}
            requiredMark={false}
          >
            <Form.Item
              name="name"
              label="项目名称"
              rules={[{ required: true, message: "请输入项目名称" }]}
            >
              <Input placeholder="例如：客服评测" />
            </Form.Item>
            <Form.Item name="description" label="项目描述">
              <Input.TextArea
                rows={3}
                placeholder="可选，描述项目用途"
              />
            </Form.Item>
          </Form>
        </Modal>
      </Space>
  );
}
