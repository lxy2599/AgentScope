import { useState } from "react";
import { useParams } from "react-router-dom";
import { App, Button, Input, Modal, Space, Typography } from "antd";
import { CopyOutlined, KeyOutlined, PlusOutlined } from "@ant-design/icons";
import { useCreateProjectToken } from "../../api/hooks";
import QuotaPanel from "../ingest/QuotaPanel";
import { PageHeader } from "../../components/ui/PageHeader";
import { PageSection } from "../../components/ui/PageSection";

const { Text, Paragraph } = Typography;

export function ProjectSettingsPage() {
  const { projectId = "" } = useParams<{ projectId: string }>();
  const createToken = useCreateProjectToken(projectId);
  const { message } = App.useApp();
  const [open, setOpen] = useState(false); const [description, setDescription] = useState(""); const [token, setToken] = useState("");
  const create = async () => { if (!description.trim()) { message.warning("请输入 Token 描述"); return; } try { const result = await createToken.mutateAsync({ description, scopes: ["otlp:write", "runner:write", "agent:register", "agent:runtime"] }); setToken(result.plain_token); setDescription(""); message.success("Token 已创建，请立即复制"); } catch { message.error("创建 Token 失败"); } };
  return <div>
    <PageHeader eyebrow="项目设置" title="接入与配额" description="在此创建用于 SDK / Runner 接入的项目 Token，并查看平台使用配额。" extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => { setToken(""); setOpen(true); }}>创建 Project Token</Button>} />
    <div className="surface-grid surface-grid--2"><PageSection title="接入 Token" description="Token 只在创建后显示一次。请通过环境变量或密钥管理系统保存，不要写入 Prompt、日志或代码仓库。" extra={<KeyOutlined />}><Paragraph>新 Token 默认授予 OTLP 写入、Runner 写入、Agent 注册与运行时上报权限。</Paragraph><Button onClick={() => { setToken(""); setOpen(true); }}>创建接入 Token</Button></PageSection><PageSection title="配额与降级" description="配额限制保护采集链路与实时连接；超限事件会保留在项目审计中。"><QuotaPanel projectId={projectId} /></PageSection></div>
    <Modal title="创建 Project Token" open={open} onCancel={() => setOpen(false)} onOk={create} confirmLoading={createToken.isPending} okText="创建 Token">
      <Space direction="vertical" style={{ width: "100%" }} size="middle"><Input value={description} onChange={(event) => setDescription(event.target.value)} placeholder="例如：production-python-sdk" autoFocus />{token && <div style={{ padding: 14, background: "var(--as-warning-soft)", border: "1px solid #f8df9c", borderRadius: 8 }}><Text strong>请立即复制。关闭窗口后无法再次查看完整 Token。</Text><Paragraph copyable={{ icon: <CopyOutlined />, text: token }} code style={{ margin: "10px 0 0", display: "block", wordBreak: "break-all" }}>{token}</Paragraph></div>}</Space>
    </Modal>
  </div>;
}
