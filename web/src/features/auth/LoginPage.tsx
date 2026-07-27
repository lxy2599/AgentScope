import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Card, Form, Input, Button, Typography, message, Tabs } from "antd";
import {
  MailOutlined,
  LockOutlined,
  UserOutlined,
  EyeInvisibleOutlined,
  EyeTwoTone,
} from "@ant-design/icons";

const { Title, Text } = Typography;

interface LoginPageProps {
  onLogin: (email: string, password: string) => Promise<void>;
  onRegister: (email: string, password: string, displayName: string) => Promise<void>;
}

export function LoginPage({ onLogin, onRegister }: LoginPageProps) {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(false);

  const handleLogin = async (values: { email: string; password: string }) => {
    setLoading(true);
    try {
      await onLogin(values.email, values.password);
      navigate("/projects");
    } catch (err: unknown) {
      message.error(
        err instanceof Error ? err.message : "登录失败，请重试",
      );
    } finally {
      setLoading(false);
    }
  };

  const handleRegister = async (values: {
    email: string;
    password: string;
    displayName: string;
  }) => {
    setLoading(true);
    try {
      await onRegister(values.email, values.password, values.displayName);
      navigate("/projects");
    } catch (err: unknown) {
      message.error(
        err instanceof Error ? err.message : "注册失败，请重试",
      );
    } finally {
      setLoading(false);
    }
  };

  const loginFormItems = (
    <Form onFinish={handleLogin} layout="vertical" size="large" requiredMark={false}>
      <Form.Item
        name="email"
        label="邮箱"
        rules={[
          { required: true, message: "请输入邮箱" },
          { type: "email", message: "邮箱格式不正确" },
        ]}
      >
        <Input prefix={<MailOutlined />} placeholder="请输入邮箱" autoComplete="email" />
      </Form.Item>
      <Form.Item
        name="password"
        label="密码"
        rules={[{ required: true, message: "请输入密码" }]}
      >
        <Input.Password
          prefix={<LockOutlined />}
          placeholder="请输入密码"
          autoComplete="current-password"
          iconRender={(visible) =>
            visible ? <EyeTwoTone /> : <EyeInvisibleOutlined />
          }
        />
      </Form.Item>
      <Form.Item>
        <Button type="primary" htmlType="submit" loading={loading} block>
          登录
        </Button>
      </Form.Item>
    </Form>
  );

  const registerFormItems = (
    <Form onFinish={handleRegister} layout="vertical" size="large" requiredMark={false}>
      <Form.Item
        name="displayName"
        label="显示名称"
        rules={[{ required: true, message: "请输入显示名称" }]}
      >
        <Input prefix={<UserOutlined />} placeholder="如：张三" />
      </Form.Item>
      <Form.Item
        name="email"
        label="邮箱"
        rules={[
          { required: true, message: "请输入邮箱" },
          { type: "email", message: "邮箱格式不正确" },
        ]}
      >
        <Input prefix={<MailOutlined />} placeholder="请输入邮箱" autoComplete="email" />
      </Form.Item>
      <Form.Item
        name="password"
        label="密码"
        rules={[
          { required: true, message: "请输入密码" },
          { min: 6, message: "密码至少 6 位" },
        ]}
      >
        <Input.Password
          prefix={<LockOutlined />}
          placeholder="请输入密码（至少6位）"
          autoComplete="new-password"
          iconRender={(visible) =>
            visible ? <EyeTwoTone /> : <EyeInvisibleOutlined />
          }
        />
      </Form.Item>
      <Form.Item>
        <Button type="primary" htmlType="submit" loading={loading} block>
          创建账号
        </Button>
      </Form.Item>
    </Form>
  );

  return (
    <main className="auth-page">
      <section className="auth-page__intro" aria-label="AgentScope 产品介绍">
        <div className="auth-page__wordmark"><span className="app-shell__brand-mark">AS</span> AgentScope</div>
        <div className="auth-page__hero">
          <span className="auth-page__eyebrow">AgentOps Console</span>
          <h1>让每一次 Agent 运行，都成为可行动的质量信号。</h1>
          <p>从实时 Trace、线上抽样到人工 Gold 和离线实验，在一个项目工作台完成定位、评测与发布决策。</p>
        </div>
        <div className="auth-page__signals" aria-label="平台能力">
          <span>实时运行与 Trace</span><span>线上评测与人工回流</span><span>数据集回归与实验对比</span>
        </div>
      </section>
      <section className="auth-page__form-wrap" aria-label="账号访问">
        <Card className="auth-page__form-card">
          <div style={{ marginBottom: 28 }}>
            <Title level={2} className="auth-page__form-title">进入工作台</Title>
            <Text type="secondary"><span>Agent 可观测与评测平台</span> · 登录或创建账号后，选择一个项目开始工作。</Text>
          </div>
          <Tabs centered items={[
            { key: "login", label: "登录", children: loginFormItems },
            { key: "register", label: "注册", children: registerFormItems },
          ]} />
        </Card>
      </section>
    </main>
  );

}
