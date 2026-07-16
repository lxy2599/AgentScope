import { useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { Layout, Menu, Button, Dropdown, Typography, Space } from "antd";
import {
  ProjectOutlined,
  UserOutlined,
  LogoutOutlined,
} from "@ant-design/icons";
import type { User } from "../../api/types";

const { Header, Sider, Content } = Layout;
const { Text } = Typography;

interface AppShellProps {
  children: React.ReactNode;
  user?: User | null;
  onLogout?: () => void;
}

export function AppShell({ children, user, onLogout }: AppShellProps) {
  const location = useLocation();
  const navigate = useNavigate();
  const [collapsed, setCollapsed] = useState(false);

  const menuItems = [
    {
      key: "/projects",
      icon: <ProjectOutlined />,
      label: "项目",
    },
  ];

  const selectedKey = menuItems
    .filter((item) => location.pathname.startsWith(item.key))
    .sort((a, b) => b.key.length - a.key.length)[0]?.key ?? "/projects";

  const userMenuItems = [
    {
      key: "logout",
      icon: <LogoutOutlined />,
      label: "退出登录",
      onClick: onLogout,
    },
  ];

  return (
    <Layout style={{ minHeight: "100vh" }}>
      <Sider
        collapsible
        collapsed={collapsed}
        onCollapse={setCollapsed}
        theme="light"
        style={{
          borderRight: "1px solid #f0f0f0",
        }}
      >
        <div
          style={{
            height: 64,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            borderBottom: "1px solid #f0f0f0",
          }}
        >
          <Text strong style={{ fontSize: collapsed ? 14 : 18, color: "#1677ff" }}>
            {collapsed ? "AS" : "AgentScope"}
          </Text>
        </div>
        <Menu
          mode="inline"
          selectedKeys={[selectedKey]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
          style={{ borderInlineEnd: "none" }}
        />
      </Sider>
      <Layout>
        <Header
          style={{
            background: "#fff",
            padding: "0 24px",
            display: "flex",
            alignItems: "center",
            justifyContent: "flex-end",
            borderBottom: "1px solid #f0f0f0",
            height: 64,
          }}
        >
          {user && (
            <Dropdown menu={{ items: userMenuItems }} placement="bottomRight">
              <Button type="text" style={{ height: 40 }}>
                <Space>
                  <UserOutlined />
                  <Text>{user.display_name}</Text>
                </Space>
              </Button>
            </Dropdown>
          )}
        </Header>
        <Content style={{ margin: 24, minHeight: 280 }}>
          <div style={{ maxWidth: 1200, margin: "0 auto" }}>{children}</div>
        </Content>
      </Layout>
    </Layout>
  );
}

export function SimpleShell({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        background: "#f5f5f5",
      }}
    >
      {children}
    </div>
  );
}
