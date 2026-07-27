import { useEffect, useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { Button, Drawer, Dropdown, Grid, Layout, Menu, Space, Typography } from "antd";
import type { MenuProps } from "antd";
import {
  AuditOutlined,
  CheckOutlined,
  DashboardOutlined,
  DatabaseOutlined,
  DownOutlined,
  ExperimentOutlined,
  FilterOutlined,
  FormOutlined,
  FundOutlined,
  HomeOutlined,
  MenuOutlined,
  ProjectOutlined,
  RobotOutlined,
  SettingOutlined,
  UnorderedListOutlined,
  UserOutlined,
  LogoutOutlined,
} from "@ant-design/icons";
import { useProjects } from "../../api/hooks";
import type { User } from "../../api/types";

const { Content, Header, Sider } = Layout;
const { Text } = Typography;
const LAST_PROJECT_ID_STORAGE_KEY = "agentscope_last_project_id";
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function getProjectUuid(pathname: string): string | null {
  const match = pathname.match(/^\/projects\/([^/]+)/);
  return match && UUID_RE.test(match[1]) ? match[1] : null;
}

interface AppShellProps {
  children: React.ReactNode;
  user?: User | null;
  onLogout?: () => void;
}

/** Project-aware console shell; every route keeps the user in a visible decision context. */
export function AppShell({ children, user, onLogout }: AppShellProps) {
  const location = useLocation();
  const navigate = useNavigate();
  const screens = Grid.useBreakpoint();
  const isMobile = screens.md === false;
  const [collapsed, setCollapsed] = useState(false);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const [lastProjectId, setLastProjectId] = useState<string | null>(() => sessionStorage.getItem(LAST_PROJECT_ID_STORAGE_KEY));
  const { data: projects = [] } = useProjects();
  const projectUuid = getProjectUuid(location.pathname);
  const currentProject = projects.find((project) => project.id === projectUuid);
  const lastProject = projects.find((project) => project.id === lastProjectId);

  useEffect(() => {
    if (!projectUuid) return;
    sessionStorage.setItem(LAST_PROJECT_ID_STORAGE_KEY, projectUuid);
    setLastProjectId(projectUuid);
  }, [projectUuid]);

  useEffect(() => setMobileMenuOpen(false), [location.pathname]);

  const menuItems = useMemo<MenuProps["items"]>(() => {
    const items: NonNullable<MenuProps["items"]> = [{ key: "/projects", icon: <ProjectOutlined />, label: "项目列表" }];
    if (!projectUuid) {
      if (lastProject) items.push({
        type: "group", label: "最近项目", children: [{ key: `/projects/${lastProject.id}`, icon: <HomeOutlined />, label: `返回工作台：${lastProject.name}` }],
      });
      return items;
    }
    const base = `/projects/${projectUuid}`;
    items.push(
      { type: "group", label: "当前项目", children: [{ key: base, icon: <HomeOutlined />, label: "项目概览" }] },
      { type: "group", label: "在线观测", children: [
        { key: `${base}/active-runs`, icon: <DashboardOutlined />, label: "实时运行" },
        { key: `${base}/agents`, icon: <RobotOutlined />, label: "Agent" },
        { key: `${base}/runs`, icon: <FundOutlined />, label: "运行追踪" },
        { key: `${base}/ingest`, icon: <DashboardOutlined />, label: "采集健康" },
      ] },
      { type: "group", label: "在线评测", children: [
        { key: `${base}/sampling-rules`, icon: <FilterOutlined />, label: "采样规则" },
        { key: `${base}/annotation-queues`, icon: <FormOutlined />, label: "标注队列" },
      ] },
      { type: "group", label: "离线评测", children: [
        { key: `${base}/datasets`, icon: <DatabaseOutlined />, label: "数据集" },
        { key: `${base}/experiments`, icon: <ExperimentOutlined />, label: "实验" },
        { key: `${base}/metric-config`, icon: <SettingOutlined />, label: "评测配置" },
        { key: `${base}/rubrics`, icon: <AuditOutlined />, label: "Rubric 模板" },
        { key: `${base}/eval-calibrations`, icon: <ExperimentOutlined />, label: "Judge 校准" },
      ] },
      { type: "group", label: "项目设置", children: [
        { key: `${base}/settings`, icon: <SettingOutlined />, label: "接入与配额" },
      ] },
    );
    return items;
  }, [lastProject, projectUuid]);

  const routeKeys = useMemo(() => {
    if (!projectUuid) return ["/projects"];
    const base = `/projects/${projectUuid}`;
    return ["/projects", `${base}/active-runs`, `${base}/agents`, `${base}/runs`, `${base}/datasets`, `${base}/experiments`, `${base}/ingest`, `${base}/sampling-rules`, `${base}/annotation-queues`, `${base}/metric-config`, `${base}/rubrics`, `${base}/eval-calibrations`, `${base}/settings`, base];
  }, [projectUuid]);
  const selectedKey = routeKeys.filter((key) => location.pathname.startsWith(key)).sort((a, b) => b.length - a.length)[0] ?? "/projects";

  const projectSwitcherItems = useMemo<MenuProps["items"]>(() => [
    ...projects.map((project) => ({ key: project.id, icon: project.id === projectUuid ? <CheckOutlined /> : <ProjectOutlined />, label: project.name })),
    ...(projects.length ? [{ type: "divider" as const }] : []),
    { key: "__all_projects__", icon: <UnorderedListOutlined />, label: "查看全部项目" },
  ], [projects, projectUuid]);

  const navigation = <Menu className="app-shell__navigation" mode="inline" selectedKeys={[selectedKey]} items={menuItems} onClick={({ key }) => navigate(key)} />;
  const userMenuItems: MenuProps["items"] = [{ key: "logout", icon: <LogoutOutlined />, label: "退出登录", onClick: onLogout }];

  return (
    <Layout className="app-shell">
      {!isMobile && (
        <Sider className="app-shell__sider" collapsible collapsed={collapsed} onCollapse={setCollapsed} theme="light" width={248}>
          <div className="app-shell__brand">
            <span className="app-shell__brand-mark">AS</span>
            {!collapsed && <span className="app-shell__brand-name">AgentScope</span>}
          </div>
          {navigation}
        </Sider>
      )}
      <Drawer title="AgentScope" placement="left" size={288} open={isMobile && mobileMenuOpen} onClose={() => setMobileMenuOpen(false)} styles={{ body: { padding: 0 } }}>
        {navigation}
      </Drawer>
      <Layout>
        <Header className="app-shell__header">
          <Space size="small" style={{ minWidth: 0 }}>
            {isMobile && <Button type="text" aria-label="打开导航" icon={<MenuOutlined />} onClick={() => setMobileMenuOpen(true)} />}
            {isMobile && <span className="app-shell__brand-mark">AS</span>}
            {projectUuid && (
              <Dropdown
                menu={{ items: projectSwitcherItems, onClick: ({ key }) => navigate(key === "__all_projects__" ? "/projects" : `/projects/${key}`) }}
                placement="bottomLeft"
                trigger={["click"]}
              >
                <Button className="app-shell__project-switcher" type="text" aria-label="切换项目">
                  <Space size={6}><ProjectOutlined /><Text ellipsis>{currentProject?.name ?? "当前项目"}</Text><DownOutlined style={{ fontSize: 10 }} /></Space>
                </Button>
              </Dropdown>
            )}
          </Space>
          {user && <Dropdown menu={{ items: userMenuItems }} placement="bottomRight" trigger={["click"]}><Button type="text"><Space><UserOutlined /><Text ellipsis>{user.display_name}</Text></Space></Button></Dropdown>}
        </Header>
        <Content className="app-shell__content"><div className="app-shell__content-inner">{children}</div></Content>
      </Layout>
    </Layout>
  );
}

export function SimpleShell({ children }: { children: React.ReactNode }) {
  return <div className="app-shell">{children}</div>;
}
