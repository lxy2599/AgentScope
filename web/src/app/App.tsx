import { lazy, Suspense, useCallback, useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  BrowserRouter,
  Navigate,
  Route,
  Routes,
  useParams,
} from "react-router-dom";
import { AppShell } from "../components/layout/AppShell";
import { LoginPage } from "../features/auth/LoginPage";
import { apiRequest } from "../api/client";
import type { User } from "../api/types";

const ProjectsPage = lazy(() =>
  import("../features/projects/ProjectsPage").then((module) => ({
    default: module.ProjectsPage,
  })),
);
const ProjectHomePage = lazy(() =>
  import("../features/projects/ProjectHomePage").then((module) => ({
    default: module.ProjectHomePage,
  })),
);
const DatasetsPage = lazy(() =>
  import("../features/datasets/DatasetsPage").then((module) => ({ default: module.DatasetsPage })),
);
const ExperimentsPage = lazy(() =>
  import("../features/experiments/ExperimentsPage").then((module) => ({ default: module.ExperimentsPage })),
);
const ProjectSettingsPage = lazy(() =>
  import("../features/projects/ProjectSettingsPage").then((module) => ({ default: module.ProjectSettingsPage })),
);
const RunsPage = lazy(() =>
  import("../features/runs/RunsPage").then((module) => ({
    default: module.RunsPage,
  })),
);
const RunDetailPage = lazy(() =>
  import("../features/runs/RunDetailPage").then((module) => ({
    default: module.RunDetailPage,
  })),
);
const ExperimentDetailPage = lazy(() =>
  import("../features/experiments/ExperimentDetailPage").then((module) => ({
    default: module.ExperimentDetailPage,
  })),
);
const ExperimentComparePage = lazy(() =>
  import("../features/experiments/ExperimentComparePage").then((module) => ({
    default: module.ExperimentComparePage,
  })),
);
const SamplingRulesPage = lazy(() =>
  import("../features/evals/SamplingRulesPage").then((module) => ({
    default: module.SamplingRulesPage,
  })),
);
const AnnotationQueuesPage = lazy(() =>
  import("../features/evals/AnnotationQueuesPage").then((module) => ({
    default: module.AnnotationQueuesPage,
  })),
);
const MetricConfigPage = lazy(() =>
  import("../features/evals/MetricConfigPage").then((module) => ({
    default: module.MetricConfigPage,
  })),
);
const RubricTemplatesPage = lazy(() =>
  import("../features/evals/RubricTemplatesPage").then((module) => ({
    default: module.RubricTemplatesPage,
  })),
);
const EvalCalibrationPage = lazy(() =>
  import("../features/evals/EvalCalibrationPage").then((module) => ({
    default: module.EvalCalibrationPage,
  })),
);
const AgentsPage = lazy(() =>
  import("../features/agents/AgentsPage").then((module) => ({
    default: module.AgentsPage,
  })),
);
const AgentDetailPage = lazy(() =>
  import("../features/agents/AgentDetailPage").then((module) => ({
    default: module.AgentDetailPage,
  })),
);
const ActiveRunsPage = lazy(
  () => import("../features/runs/ActiveRunsPage"),
);
const RunRuntimeDetailPage = lazy(
  () => import("../features/runs/RunRuntimeDetailPage"),
);
const IngestDashboard = lazy(
  () => import("../features/ingest/IngestDashboard"),
);

const qc = new QueryClient();

/** UUID v4 regex — only valid UUIDs may act as project IDs. */
export const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * Route guard that redirects to /projects when :projectId is not a valid UUID.
 * This prevents "ingest" or other non-UUID values from reaching
 * project-scoped APIs and triggering 403 responses.
 */
export function ValidProjectGuard({ children }: { children: React.ReactNode }) {
  const { projectId } = useParams<{ projectId: string }>();
  if (!projectId || !UUID_RE.test(projectId)) {
    return <Navigate to="/projects" replace />;
  }
  return <>{children}</>;
}

function RouteLoading() {
  return (
    <div
      role="status"
      aria-label="页面加载中"
      aria-live="polite"
      style={{
        color: "#666",
        padding: 48,
        textAlign: "center",
      }}
    >
      页面加载中…
    </div>
  );
}

function useAuth() {
  const [user, setUser] = useState<User | null>(
    () => JSON.parse(sessionStorage.getItem("agentscope_user") ?? "null"),
  );

  const login = useCallback(async (email: string, password: string) => {
    const user = await apiRequest<User>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    });
    sessionStorage.setItem("agentscope_user", JSON.stringify(user));
    setUser(user);
  }, []);

  const register = useCallback(
    async (email: string, password: string, displayName: string) => {
      const user = await apiRequest<User>("/api/auth/register", {
        method: "POST",
        body: JSON.stringify({ email, password, display_name: displayName }),
      });
      sessionStorage.setItem("agentscope_user", JSON.stringify(user));
      setUser(user);
    },
    [],
  );

  const logout = useCallback(() => {
    sessionStorage.removeItem("agentscope_user");
    setUser(null);
  }, []);

  return { user, login, register, logout };
}

function AppRoutes() {
  const { user, login, register, logout } = useAuth();

  if (!user) {
    return <LoginPage onLogin={login} onRegister={register} />;
  }

  return (
    <AppShell user={user} onLogout={logout}>
      <Suspense fallback={<RouteLoading />}>
        <Routes>
          <Route path="/" element={<Navigate to="/projects" replace />} />
          <Route path="/projects" element={<ProjectsPage />} />
          <Route path="/projects/:projectId" element={<ValidProjectGuard><ProjectHomePage /></ValidProjectGuard>} />
          <Route path="/projects/:projectId/datasets" element={<ValidProjectGuard><DatasetsPage /></ValidProjectGuard>} />
          <Route path="/projects/:projectId/experiments" element={<ValidProjectGuard><ExperimentsPage /></ValidProjectGuard>} />
          <Route path="/projects/:projectId/settings" element={<ValidProjectGuard><ProjectSettingsPage /></ValidProjectGuard>} />
          <Route path="/projects/:projectId/runs" element={<ValidProjectGuard><RunsPage /></ValidProjectGuard>} />
          <Route path="/projects/:projectId/agents" element={<ValidProjectGuard><AgentsPage /></ValidProjectGuard>} />
          <Route path="/projects/:projectId/agents/:agentId" element={<ValidProjectGuard><AgentDetailPage /></ValidProjectGuard>} />
          <Route path="/projects/:projectId/active-runs" element={<ValidProjectGuard><ActiveRunsPage /></ValidProjectGuard>} />
          <Route path="/projects/:projectId/runs/:runId/runtime" element={<ValidProjectGuard><RunRuntimeDetailPage /></ValidProjectGuard>} />
          <Route path="/runs/:runId" element={<RunDetailPage />} />
          <Route path="/experiments/:experimentId" element={<ExperimentDetailPage />} />
          <Route path="/comparisons/:comparisonId" element={<ExperimentComparePage />} />
          <Route path="/projects/:projectId/sampling-rules" element={<ValidProjectGuard><SamplingRulesPage /></ValidProjectGuard>} />
          <Route path="/projects/:projectId/annotation-queues" element={<ValidProjectGuard><AnnotationQueuesPage /></ValidProjectGuard>} />
          <Route path="/projects/:projectId/metric-config" element={<ValidProjectGuard><MetricConfigPage /></ValidProjectGuard>} />
          <Route path="/projects/:projectId/rubrics" element={<ValidProjectGuard><RubricTemplatesPage /></ValidProjectGuard>} />
          <Route path="/projects/:projectId/eval-calibrations" element={<ValidProjectGuard><EvalCalibrationPage /></ValidProjectGuard>} />
          <Route path="/projects/:projectId/ingest" element={<ValidProjectGuard><IngestDashboard /></ValidProjectGuard>} />
          <Route path="*" element={<Navigate to="/projects" replace />} />
        </Routes>
      </Suspense>
    </AppShell>
  );
}

export function App() {
  return (
    <QueryClientProvider client={qc}>
      <BrowserRouter>
        <AppRoutes />
      </BrowserRouter>
    </QueryClientProvider>
  );
}
