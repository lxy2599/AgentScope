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
import type { MenuProps } from "antd";
import {
  ProjectOutlined,
  HomeOutlined,
  RobotOutlined,
  FundOutlined,
  SettingOutlined,
  DownOutlined,
  UnorderedListOutlined,
  CheckOutlined,
  UserOutlined,
  LogoutOutlined,
  DashboardOutlined,
  AuditOutlined,
  ExperimentOutlined,
  FilterOutlined,
  FormOutlined,
  MenuOutlined,
} from "@ant-design/icons";
import { useProjects } from "../../api/hooks";
import type { User } from "../../api/types";

const { Header, Sider, Content } = Layout;
const { Text } = Typography;
const LAST_PROJECT_ID_STORAGE_KEY = "agentscope_last_project_id";

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
  const [lastProjectID, setLastProjectID] = useState<string | null>(
    () => sessionStorage.getItem(LAST_PROJECT_ID_STORAGE_KEY),
  );
  const { data: projects = [] } = useProjects();

  // Project-scoped menu entries are only valid for UUID project routes.
  const projectUuid = getProjectUuid(location.pathname);
  const currentProject = projects.find((project) => project.id === projectUuid);
  const lastProject = projects.find((project) => project.id === lastProjectID);

  useEffect(() => {
    if (!projectUuid) return;
    sessionStorage.setItem(LAST_PROJECT_ID_STORAGE_KEY, projectUuid);
    setLastProjectID(projectUuid);
  }, [projectUuid]);

  const menuItems = useMemo<MenuProps["items"]>(() => {
    const items: NonNullable<MenuProps["items"]> = [
      {
        key: "/projects",
        icon: <ProjectOutlined />,
        label: "项目列表",
      },
    ];
    if (!projectUuid) {
      if (lastProject) {
        items.push({
          type: "group",
          label: "最近项目",
          children: [
            {
              key: `/projects/${lastProject.id}`,
              icon: <HomeOutlined />,
              label: `返回工作台：${lastProject.name}`,
            },
          ],
        });
      }
      return items;
    }

    const projectPath = `/projects/${projectUuid}`;
    items.push(
      {
        type: "group",
        label: "当前项目",
        children: [
          {
            key: projectPath,
            icon: <HomeOutlined />,
            label: "项目概览",
          },
        ],
      },
      {
        type: "group",
        label: "在线观测",
        children: [
          {
            key: `${projectPath}/agents`,
            icon: <RobotOutlined />,
            label: "Agent",
          },
          {
            key: `${projectPath}/runs`,
            icon: <FundOutlined />,
            label: "运行追踪",
          },
          {
            key: `${projectPath}/ingest`,
            icon: <DashboardOutlined />,
            label: "采集健康",
          },
        ],
      },
      {
        type: "group",
        label: "在线评测",
        children: [
          {
            key: `${projectPath}/sampling-rules`,
            icon: <FilterOutlined />,
            label: "采样规则",
          },
          {
            key: `${projectPath}/annotation-queues`,
            icon: <FormOutlined />,
            label: "标注队列",
          },
        ],
      },
      {
        type: "group",
        label: "离线评测",
        children: [
          {
            key: `${projectPath}/metric-config`,
            icon: <SettingOutlined />,
            label: "评测配置",
          },
          {
            key: `${projectPath}/rubrics`,
            icon: <AuditOutlined />,
            label: "Rubric 模板",
          },
          {
            key: `${projectPath}/eval-calibrations`,
            icon: <ExperimentOutlined />,
            label: "Judge 校准",
          },
        ],
      },
    );
    return items;
  }, [lastProject, projectUuid]);

  const projectSwitcherItems = useMemo<MenuProps["items"]>(
    () => [
      ...projects.map((project) => ({
        key: project.id,
        icon: project.id === projectUuid ? <CheckOutlined /> : <ProjectOutlined />,
        label: project.name,
      })),
      ...(projects.length > 0 ? [{ type: "divider" as const }] : []),
      {
        key: "__all_projects__",
        icon: <UnorderedListOutlined />,
        label: "查看全部项目",
      },
    ],
    [projects, projectUuid],
  );

  const handleProjectSwitch: MenuProps["onClick"] = ({ key }) => {
    if (key === "__all_projects__") {
      navigate("/projects");
      return;
    }
    navigate(`/projects/${key}`);
  };

  const routeKeys = useMemo(() => {
    if (!projectUuid) return ["/projects"];
    const projectPath = `/projects/${projectUuid}`;
    return [
      "/projects",
      projectPath,
      `${projectPath}/agents`,
      `${projectPath}/runs`,
      `${projectPath}/ingest`,
      `${projectPath}/sampling-rules`,
      `${projectPath}/annotation-queues`,
      `${projectPath}/metric-config`,
      `${projectPath}/rubrics`,
      `${projectPath}/eval-calibrations`,
    ];
  }, [projectUuid]);

  const selectedKey = routeKeys
    .filter((key) => location.pathname.startsWith(key))
    .sort((a, b) => b.length - a.length)[0] ?? "/projects";

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
            justifyContent: "space-between",
            borderBottom: "1px solid #f0f0f0",
            height: 64,
            gap: 12,
          }}
        >
          <Space size="small" style={{ minWidth: 0 }}>
            {isMobile && (
              <Button
                type="text"
                aria-label="打开导航"
                icon={<MenuOutlined />}
                onClick={() => setMobileMenuOpen(true)}
              />
            )}
            {isMobile && <Text strong style={{ color: "#1677ff" }}>AgentScope</Text>}
            {projectUuid && (
              <Dropdown
                menu={{ items: projectSwitcherItems, onClick: handleProjectSwitch }}
                placement="bottomLeft"
                trigger={["click"]}
              >
                <Button
                  type="text"
                  aria-label="切换项目"
                  style={{ maxWidth: isMobile ? 168 : 320, minWidth: 0 }}
                >
                  <Space size={6} style={{ maxWidth: "100%" }}>
                    <ProjectOutlined />
                    <Text ellipsis strong>{currentProject?.name ?? "当前项目"}</Text>
                    <DownOutlined style={{ fontSize: 10 }} />
                  </Space>
                </Button>
              </Dropdown>
            )}
          </Space>
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
