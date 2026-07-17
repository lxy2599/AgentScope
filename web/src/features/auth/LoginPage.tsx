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
    <div
      style={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        background: "linear-gradient(135deg, #667eea 0%, #764ba2 100%)",
        padding: 24,
      }}
    >
      <Card
        style={{
          width: 440,
          borderRadius: 12,
          boxShadow: "0 8px 32px rgba(0,0,0,0.15)",
        }}
        styles={{ body: { padding: "40px 40px 32px" } }}
      >
        <div style={{ textAlign: "center", marginBottom: 32 }}>
          <Title level={2} style={{ marginBottom: 8, color: "#1677ff" }}>
            AgentScope
          </Title>
          <Text type="secondary" style={{ fontSize: 15 }}>
            Agent 可观测与评测平台
          </Text>
        </div>

        <Tabs
          centered
          items={[
            {
              key: "login",
              label: "登录",
              children: loginFormItems,
            },
            {
              key: "register",
              label: "注册",
              children: registerFormItems,
            },
          ]}
        />
      </Card>
    </div>
  );
}
