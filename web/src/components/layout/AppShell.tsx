import { useEffect, useState, useMemo } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import {
  Layout,
  Menu,
  Button,
  Dropdown,
  Typography,
  Space,
  Drawer,
  Grid,
} from "antd";
import {
  ProjectOutlined,
  UserOutlined,
  LogoutOutlined,
  DashboardOutlined,
  AuditOutlined,
  ExperimentOutlined,
  MenuOutlined,
} from "@ant-design/icons";
import type { User } from "../../api/types";

const { Header, Sider, Content } = Layout;
const { Text } = Typography;

/** UUID v4 regex — captures valid project IDs only. */
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Extract a UUID projectId from the pathname, or return null. */
function getProjectUuid(pathname: string): string | null {
  const match = pathname.match(/^\/projects\/([^/]+)/);
  if (!match) return null;
  return UUID_RE.test(match[1]) ? match[1] : null;
}

interface AppShellProps {
  children: React.ReactNode;
  user?: User | null;
  onLogout?: () => void;
}

export function AppShell({ children, user, onLogout }: AppShellProps) {
  const location = useLocation();
  const navigate = useNavigate();
  const screens = Grid.useBreakpoint();
  const isMobile = screens.md === false;
  const [collapsed, setCollapsed] = useState(false);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);

  // Only show "采集健康" when we are inside a real project with a UUID.
  const projectUuid = getProjectUuid(location.pathname);

  const menuItems = useMemo(() => {
    const items = [
      {
        key: "/projects",
        icon: <ProjectOutlined />,
        label: "项目",
      },
    ];
    if (projectUuid) {
      items.push({
        key: `/projects/${projectUuid}/ingest`,
        icon: <DashboardOutlined />,
        label: "采集健康",
      });
      items.push({
        key: `/projects/${projectUuid}/rubrics`,
        icon: <AuditOutlined />,
        label: "Rubric 模板",
      });
      items.push({
        key: `/projects/${projectUuid}/eval-calibrations`,
        icon: <ExperimentOutlined />,
        label: "Judge 校准",
      });
    }
    return items;
  }, [projectUuid]);

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

  useEffect(() => {
    setMobileMenuOpen(false);
  }, [location.pathname]);

  const navigation = (
    <Menu
      mode="inline"
      selectedKeys={[selectedKey]}
      items={menuItems}
      onClick={({ key }) => navigate(key)}
      style={{ borderInlineEnd: "none" }}
    />
  );

  return (
    <Layout style={{ minHeight: "100vh", minWidth: 0 }}>
      {!isMobile && (
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
          {navigation}
        </Sider>
      )}
      <Drawer
        title="AgentScope"
        placement="left"
        size={280}
        open={isMobile && mobileMenuOpen}
        onClose={() => setMobileMenuOpen(false)}
        styles={{ body: { padding: 0 } }}
      >
        {navigation}
      </Drawer>
      <Layout style={{ minWidth: 0 }}>
        <Header
          style={{
            background: "#fff",
            padding: isMobile ? "0 12px" : "0 24px",
            display: "flex",
            alignItems: "center",
            justifyContent: isMobile ? "space-between" : "flex-end",
            borderBottom: "1px solid #f0f0f0",
            height: 64,
          }}
        >
          {isMobile && (
            <Space size="small">
              <Button
                type="text"
                aria-label="打开导航"
                icon={<MenuOutlined />}
                onClick={() => setMobileMenuOpen(true)}
              />
              <Text strong style={{ color: "#1677ff" }}>AgentScope</Text>
            </Space>
          )}
          {user && (
            <Dropdown
              menu={{ items: userMenuItems }}
              placement="bottomRight"
              trigger={["click"]}
            >
              <Button type="text" style={{ height: 40, maxWidth: isMobile ? 168 : undefined }}>
                <Space>
                  <UserOutlined />
                  <Text ellipsis>{user.display_name}</Text>
                </Space>
              </Button>
            </Dropdown>
          )}
        </Header>
        <Content style={{ margin: isMobile ? 12 : 24, minHeight: 280, minWidth: 0 }}>
          <div style={{ width: "100%", maxWidth: 1200, minWidth: 0, margin: "0 auto" }}>
            {children}
          </div>
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
