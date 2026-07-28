import { useEffect, useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import {
  App,
  Button,
  Drawer,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import {
  ClockCircleOutlined,
  CopyOutlined,
  HistoryOutlined,
  PlusOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  StopOutlined,
} from "@ant-design/icons";
import type {
  CreateProjectTokenInput,
  ProjectToken,
  ProjectTokenEnvironment,
  ProjectTokenPolicy,
  ProjectTokenType,
} from "../../api/types";
import {
  useCreateProjectToken,
  useProjectTokenAudit,
  useProjectTokenPolicy,
  useProjectTokenRiskSummary,
  useProjectTokens,
  useRevokeProjectToken,
  useRotateProjectToken,
  useUpdateProjectTokenPolicy,
} from "../../api/hooks";
import QuotaPanel from "../ingest/QuotaPanel";
import { DataTable } from "../../components/ui/DataTable";
import { EmptyState } from "../../components/ui/EmptyState";
import { ErrorState } from "../../components/ui/ErrorState";
import { PageHeader } from "../../components/ui/PageHeader";
import { PageSection } from "../../components/ui/PageSection";
import { StatusBadge } from "../../components/ui/StatusBadge";

const { Paragraph, Text } = Typography;

type TokenTemplate = "trace" | "runner" | "runtime" | "full";
type TokenForm = {
  description: string;
  template: TokenTemplate;
  environment: ProjectTokenEnvironment;
  expiresIn: number;
};

const TOKEN_TEMPLATES: Record<
  TokenTemplate,
  { label: string; tokenType: ProjectTokenType; scopes: string[]; description: string }
> = {
  trace: {
    label: "Trace 上报 SDK",
    tokenType: "trace_sdk",
    scopes: ["otlp:write"],
    description: "仅允许 OTLP Trace 上报。适用于 Python / Go SDK。",
  },
  runner: {
    label: "Runner 执行器",
    tokenType: "runner",
    scopes: ["runner:write"],
    description: "仅允许领取与更新离线评测任务。",
  },
  runtime: {
    label: "Agent Runtime",
    tokenType: "agent_runtime",
    scopes: ["agent:register", "agent:runtime"],
    description: "允许 Agent 注册、心跳和实时状态上报。",
  },
  full: {
    label: "完整接入（高权限）",
    tokenType: "custom",
    scopes: ["otlp:write", "runner:write", "agent:register", "agent:runtime"],
    description: "同时授予所有接入权限；只在明确需要的受控场景使用。",
  },
};

const DEFAULT_POLICY: ProjectTokenPolicy = {
  project_id: "",
  allow_non_expiring: true,
  expiry_warning_days: [14, 7, 1],
  unused_warning_days: 60,
};

function expirationFromDays(days: number) {
  return days > 0 ? new Date(Date.now() + days * 86_400_000).toISOString() : undefined;
}

function formatDate(value?: string, fallback = "—") {
  return value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : fallback;
}

function getRiskMetrics(tokens: ProjectToken[], policy: ProjectTokenPolicy) {
  const expiryDays = Math.max(...policy.expiry_warning_days, 0);
  const expiryThreshold = Date.now() + expiryDays * 86_400_000;
  const unusedThreshold = Date.now() - policy.unused_warning_days * 86_400_000;

  return {
    expiring: tokens.filter((token) =>
      token.status === "active" && token.expires_at && new Date(token.expires_at).getTime() <= expiryThreshold,
    ).length,
    unused: tokens.filter((token) =>
      token.status === "active" && (!token.last_used_at || new Date(token.last_used_at).getTime() < unusedThreshold),
    ).length,
    broad: tokens.filter((token) =>
      token.status === "active" && token.environment === "production" && token.scopes.length === 4,
    ).length,
  };
}

function auditSummary(event: { event_type: string; metadata: Record<string, unknown> }) {
  switch (event.event_type) {
    case "created":
      return "签发了新的接入凭证";
    case "rotation_started":
      return "创建了替代凭证；旧凭证不会被自动撤销";
    case "revoked":
      return typeof event.metadata.reason === "string" && event.metadata.reason
        ? `撤销原因：${event.metadata.reason}`
        : "凭证已撤销";
    default:
      return "记录了 Token 生命周期事件";
  }
}

export function ProjectSettingsPage() {
  const { projectId = "" } = useParams<{ projectId: string }>();
  const { message, modal } = App.useApp();
  const [form] = Form.useForm<TokenForm>();
  const {
    data: tokens = [],
    isLoading: tokensLoading,
    isError: tokensError,
    refetch: refetchTokens,
  } = useProjectTokens(projectId);
  const { data: policyData, isError: policyError } = useProjectTokenPolicy(projectId);
  const { data: riskSummary, isError: riskError } = useProjectTokenRiskSummary(projectId);
  const createToken = useCreateProjectToken(projectId);
  const rotateToken = useRotateProjectToken(projectId);
  const revokeToken = useRevokeProjectToken(projectId);
  const updatePolicy = useUpdateProjectTokenPolicy(projectId);

  const [policyDraft, setPolicyDraft] = useState<ProjectTokenPolicy>(DEFAULT_POLICY);
  const [modalOpen, setModalOpen] = useState(false);
  const [rotationTarget, setRotationTarget] = useState<ProjectToken | null>(null);
  const [plainToken, setPlainToken] = useState("");
  const [selectedToken, setSelectedToken] = useState<ProjectToken | null>(null);
  const [statusFilter, setStatusFilter] = useState<"all" | ProjectToken["status"]>("all");
  const [environmentFilter, setEnvironmentFilter] = useState<"all" | ProjectTokenEnvironment>("all");
  const [scopeFilter, setScopeFilter] = useState<"all" | string>("all");
  const [expiryFilter, setExpiryFilter] = useState<"all" | "expiring" | "non_expiring">("all");
  const { data: audit = [], isLoading: auditLoading } = useProjectTokenAudit(projectId, selectedToken?.id ?? "");

  useEffect(() => {
    if (policyData) setPolicyDraft(policyData);
  }, [policyData]);

  const risks = riskSummary
    ? { expiring: riskSummary.expiring_soon, unused: riskSummary.unused, broad: riskSummary.broad_production }
    : getRiskMetrics(tokens, policyData ?? policyDraft);
  const visibleTokens = useMemo(() => {
    const expiryThreshold = Date.now() + Math.max(...policyDraft.expiry_warning_days, 0) * 86_400_000;
    return tokens.filter((token) => {
      if (statusFilter !== "all" && token.status !== statusFilter) return false;
      if (environmentFilter !== "all" && token.environment !== environmentFilter) return false;
      if (scopeFilter !== "all" && !token.scopes.includes(scopeFilter)) return false;
      if (expiryFilter === "expiring" && (!token.expires_at || new Date(token.expires_at).getTime() > expiryThreshold)) return false;
      if (expiryFilter === "non_expiring" && token.expires_at) return false;
      return true;
    });
  }, [tokens, policyDraft.expiry_warning_days, statusFilter, environmentFilter, scopeFilter, expiryFilter]);

  const closeTokenModal = () => {
    setModalOpen(false);
    setPlainToken("");
    setRotationTarget(null);
    form.resetFields();
  };

  const showTokenModal = (token?: ProjectToken) => {
    setRotationTarget(token ?? null);
    setPlainToken("");
    form.setFieldsValue({
      description: token ? `${token.description || token.token_prefix} replacement` : "",
      template: "trace",
      environment: token?.environment ?? "development",
      expiresIn: token?.expires_at ? 30 : 0,
    });
    setModalOpen(true);
  };

  const submitToken = async () => {
    const values = await form.validateFields();
    const preset = TOKEN_TEMPLATES[values.template];
    if (values.template === "full") {
      const confirmed = await new Promise<boolean>((resolve) => {
        modal.confirm({
          title: "确认创建高权限 Token？",
          content: "完整接入 Token 包含全部 Scope。请仅在受控部署中使用，并设置合理的有效期。",
          okText: "确认创建",
          cancelText: "取消",
          okButtonProps: { danger: true },
          onOk: () => resolve(true),
          onCancel: () => resolve(false),
        });
      });
      if (!confirmed) return;
    }
    const input: CreateProjectTokenInput = {
      description: values.description.trim(),
      scopes: preset.scopes,
      expires_at: expirationFromDays(values.expiresIn),
      environment: values.environment,
      token_type: preset.tokenType,
    };

    try {
      const result = rotationTarget
        ? await rotateToken.mutateAsync({ tokenId: rotationTarget.id, ...input })
        : await createToken.mutateAsync(input);
      setPlainToken(result.plain_token);
      message.success(rotationTarget ? "替代 Token 已创建，请完成部署切换" : "Token 已创建，请立即复制");
    } catch {
      message.error("Token 操作失败，请检查管理员权限、TTL 策略和 Scope 配置");
    }
  };

  const revoke = async (token: ProjectToken) => {
    try {
      await revokeToken.mutateAsync({ tokenId: token.id, reason: "管理员主动撤销" });
      message.success(`${token.token_prefix} 已撤销`);
    } catch {
      message.error("撤销失败，请稍后重试");
    }
  };

  const savePolicy = async () => {
    try {
      await updatePolicy.mutateAsync(policyDraft);
      message.success("Token 策略已保存");
    } catch {
      message.error("保存 Token 策略失败，请检查管理员权限");
    }
  };

  if (tokensError || policyError || riskError) {
    return (
      <ErrorState
        title="无法读取 Token 治理数据"
        description="只有项目 owner 和 admin 可以查看或管理项目接入凭证。"
        onRetry={() => { void refetchTokens(); }}
      />
    );
  }

  return (
    <div>
      <PageHeader
        eyebrow="项目设置"
        title="接入 Token 与凭证治理"
        description="创建最小权限接入凭证、无中断轮换生产 Token，并处理即将到期、长期闲置或权限过宽的风险。"
        extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => showTokenModal()}>创建 Token</Button>}
      />

      <div className="signal-overview">
        <div className={`signal-card ${risks.expiring ? "signal-card--warning" : "signal-card--success"}`}>
          <span className="signal-card__label">临近到期</span>
          <strong className="signal-card__value">{risks.expiring}</strong>
          <span className="signal-card__hint">{Math.max(...policyDraft.expiry_warning_days, 0)} 天内应完成轮换</span>
        </div>
        <div className={`signal-card ${risks.unused ? "signal-card--warning" : ""}`}>
          <span className="signal-card__label">长期未使用</span>
          <strong className="signal-card__value">{risks.unused}</strong>
          <span className="signal-card__hint">{policyDraft.unused_warning_days} 天无使用记录</span>
        </div>
        <div className={`signal-card ${risks.broad ? "signal-card--danger" : ""}`}>
          <span className="signal-card__label">生产高权限</span>
          <strong className="signal-card__value">{risks.broad}</strong>
          <span className="signal-card__hint">拥有全部接入 Scope</span>
        </div>
      </div>

      <PageSection
        title="接入 Token"
        description="仅展示前缀与治理元数据；完整 Token 只会在创建或轮换时出现一次。"
        extra={<SafetyCertificateOutlined />}
      >
        <Space wrap style={{ marginBottom: 16 }}>
          <Select value={statusFilter} onChange={setStatusFilter} style={{ minWidth: 110 }} options={[{ value: "all", label: "全部状态" }, { value: "active", label: "生效" }, { value: "revoked", label: "已撤销" }, { value: "expired", label: "已过期" }]} />
          <Select value={environmentFilter} onChange={setEnvironmentFilter} style={{ minWidth: 120 }} options={[{ value: "all", label: "全部环境" }, ...["development", "staging", "production", "ci", "local"].map((value) => ({ value, label: value }))]} />
          <Select value={scopeFilter} onChange={setScopeFilter} style={{ minWidth: 150 }} options={[{ value: "all", label: "全部权限" }, ...["otlp:write", "runner:write", "agent:register", "agent:runtime"].map((value) => ({ value, label: value }))]} />
          <Select value={expiryFilter} onChange={setExpiryFilter} style={{ minWidth: 140 }} options={[{ value: "all", label: "全部有效期" }, { value: "expiring", label: "临近到期" }, { value: "non_expiring", label: "永不过期" }]} />
        </Space>
        {tokens.length > 0 ? (
          <DataTable<ProjectToken>
            loading={tokensLoading}
            rowKey="id"
            dataSource={visibleTokens}
            scroll={{ x: 1080 }}
            pagination={{ pageSize: 10, showSizeChanger: false }}
            columns={[
              {
                title: "名称 / 前缀",
                key: "name",
                render: (_, token) => (
                  <Space direction="vertical" size={0}>
                    <Text strong>{token.description || "未命名 Token"}</Text>
                    <Text code>{token.token_prefix}</Text>
                  </Space>
                ),
              },
              { title: "环境", dataIndex: "environment", render: (value: string) => <Tag>{value}</Tag> },
              {
                title: "权限",
                dataIndex: "scopes",
                render: (scopes: string[]) => (
                  <Space size={[4, 4]} wrap>
                    {scopes.map((scope) => <Tag key={scope} color="blue">{scope}</Tag>)}
                  </Space>
                ),
              },
              {
                title: "最近使用",
                key: "last_used_at",
                render: (_, token) => (
                  <Space direction="vertical" size={0}>
                    <span>{formatDate(token.last_used_at, "从未使用")}</span>
                    {token.last_used_endpoint && <Text type="secondary">{token.last_used_endpoint}</Text>}
                  </Space>
                ),
              },
              { title: "到期", dataIndex: "expires_at", render: (value?: string) => formatDate(value, "永不过期") },
              {
                title: "状态",
                dataIndex: "status",
                render: (status: ProjectToken["status"]) => <StatusBadge status={status} />,
              },
              {
                title: "操作",
                key: "action",
                fixed: "right",
                render: (_, token) => {
                  const successor = tokens.find((candidate) => candidate.rotates_token_id === token.id);
                  const canRetire = !successor || !!successor.last_used_at;
                  return (
                    <Space size={0}>
                      <Button type="link" icon={<HistoryOutlined />} onClick={() => setSelectedToken(token)}>审计</Button>
                      {token.status === "active" && (
                        <>
                          <Button type="link" icon={<ReloadOutlined />} onClick={() => showTokenModal(token)}>轮换</Button>
                          <Popconfirm
                            title={`撤销 ${token.token_prefix}？`}
                            description="撤销后所有使用该 Token 的请求将立即被拒绝。"
                            okText="撤销"
                            cancelText="取消"
                            okButtonProps={{ danger: true }}
                            disabled={!canRetire}
                            onConfirm={() => void revoke(token)}
                          >
                            <Tooltip title={canRetire ? undefined : "替代 Token 尚未产生首次使用记录，暂不能完成轮换。"}>
                              <Button type="link" danger disabled={!canRetire} icon={<StopOutlined />}>撤销</Button>
                            </Tooltip>
                          </Popconfirm>
                        </>
                      )}
                    </Space>
                  );
                },
              },
            ]}
          />
        ) : (
          <EmptyState
            title="尚未创建接入 Token"
            description="选择最小权限模板，为 SDK、Runner 或 Agent Runtime 签发凭证。"
            actionLabel="创建 Token"
            onAction={() => showTokenModal()}
          />
        )}
      </PageSection>

      <div className="surface-grid surface-grid--2" style={{ marginTop: 16 }}>
        <PageSection
          title="项目 Token 策略"
          description="用项目规则约束生产与 CI 凭证的有效期，并定义风险提醒阈值。"
          extra={<ClockCircleOutlined />}
        >
          <Space direction="vertical" size="middle" style={{ width: "100%" }}>
            <Space>
              <Switch
                checked={policyDraft.allow_non_expiring}
                onChange={(allow_non_expiring) => setPolicyDraft((current) => ({ ...current, allow_non_expiring }))}
              />
              <span>允许永不过期 Token</span>
            </Space>
            <Space wrap>
              <span>生产最长有效期</span>
              <InputNumber
                min={1}
                value={policyDraft.max_production_ttl_days}
                placeholder="不限制"
                onChange={(max_production_ttl_days) => setPolicyDraft((current) => ({
                  ...current,
                  max_production_ttl_days: max_production_ttl_days ?? undefined,
                }))}
                addonAfter="天"
              />
              <span>CI 最长有效期</span>
              <InputNumber
                min={1}
                value={policyDraft.max_ci_ttl_days}
                placeholder="不限制"
                onChange={(max_ci_ttl_days) => setPolicyDraft((current) => ({
                  ...current,
                  max_ci_ttl_days: max_ci_ttl_days ?? undefined,
                }))}
                addonAfter="天"
              />
            </Space>
            <Space wrap>
              <span>长期未使用阈值</span>
              <InputNumber
                min={1}
                value={policyDraft.unused_warning_days}
                onChange={(unused_warning_days) => setPolicyDraft((current) => ({
                  ...current,
                  unused_warning_days: unused_warning_days ?? DEFAULT_POLICY.unused_warning_days,
                }))}
                addonAfter="天"
              />
              <Button onClick={() => void savePolicy()} loading={updatePolicy.isPending}>保存策略</Button>
            </Space>
          </Space>
        </PageSection>
        <PageSection
          title="配额与降级"
          description="配额保护采集链路与实时连接；凭证治理与配额共同限制接入风险。"
        >
          <QuotaPanel projectId={projectId} />
        </PageSection>
      </div>

      <Modal
        title={rotationTarget ? `轮换 ${rotationTarget.token_prefix}` : "创建 Project Token"}
        open={modalOpen}
        onCancel={closeTokenModal}
        onOk={() => void submitToken()}
        confirmLoading={createToken.isPending || rotateToken.isPending}
        okText={rotationTarget ? "创建替代 Token" : "创建 Token"}
        cancelText="取消"
        width={560}
        footer={plainToken ? <Button type="primary" onClick={closeTokenModal}>我已安全保存</Button> : undefined}
      >
        {plainToken ? (
          <div style={{ padding: 14, background: "var(--as-warning-soft)", border: "1px solid #f8df9c", borderRadius: 8 }}>
            <Text strong>请立即复制。关闭窗口后不能再次查看完整 Token。</Text>
            <Paragraph
              copyable={{ icon: <CopyOutlined />, text: plainToken }}
              code
              style={{ margin: "10px 0 0", display: "block", wordBreak: "break-all" }}
            >
              {plainToken}
            </Paragraph>
            {rotationTarget && (
              <Text type="secondary">下一步：更新部署环境变量，确认新 Token 有使用记录后，再撤销旧 Token。</Text>
            )}
          </div>
        ) : (
          <Form form={form} layout="vertical" requiredMark={false}>
            <Form.Item name="description" label="名称 / 描述" rules={[{ required: true, message: "请输入 Token 用途" }]}>
              <Input placeholder="例如：production-python-sdk" autoFocus />
            </Form.Item>
            <Form.Item name="template" label="权限模板" rules={[{ required: true }]}>
              <Select
                options={Object.entries(TOKEN_TEMPLATES).map(([value, template]) => ({
                  value,
                  label: template.label,
                  description: template.description,
                }))}
                optionRender={(option) => (
                  <div>
                    <strong>{option.data.label}</strong><br />
                    <Text type="secondary">{option.data.description}</Text>
                  </div>
                )}
              />
            </Form.Item>
            <Form.Item name="environment" label="部署环境">
              <Select options={["development", "staging", "production", "ci", "local"].map((value) => ({ value, label: value }))} />
            </Form.Item>
            <Form.Item name="expiresIn" label="有效期">
              <Select options={[
                { value: 0, label: "永不过期（受项目策略限制）" },
                { value: 7, label: "7 天" },
                { value: 30, label: "30 天" },
                { value: 90, label: "90 天" },
              ]} />
            </Form.Item>
          </Form>
        )}
      </Modal>

      <Drawer
        title={selectedToken ? `审计 · ${selectedToken.token_prefix}` : "Token 审计"}
        open={!!selectedToken}
        onClose={() => setSelectedToken(null)}
        width={620}
      >
        {auditLoading ? <Text>加载中…</Text> : audit.length > 0 ? (
          <Space direction="vertical" style={{ width: "100%" }}>
            {audit.map((event) => (
              <div key={event.id} className="signal-card">
                <Text strong>{event.event_type}</Text><br />
                <Text>{auditSummary(event)}</Text><br />
                <Text type="secondary">{formatDate(event.created_at)} · 操作者 {event.actor_user_id || "系统"}</Text>
              </div>
            ))}
          </Space>
        ) : (
          <EmptyState title="暂无审计事件" description="创建、撤销和轮换事件会保留在这里。" />
        )}
      </Drawer>
    </div>
  );
}
