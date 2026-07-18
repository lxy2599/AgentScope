import { useCallback, useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  BrowserRouter,
  Navigate,
  Route,
  Routes,
} from "react-router-dom";
import { AppShell } from "../components/layout/AppShell";
import { ProjectsPage } from "../features/projects/ProjectsPage";
import { ProjectHomePage } from "../features/projects/ProjectHomePage";
import { RunsPage } from "../features/runs/RunsPage";
import { RunDetailPage } from "../features/runs/RunDetailPage";
import { ExperimentDetailPage } from "../features/experiments/ExperimentDetailPage";
import { AgentsPage } from "../features/agents/AgentsPage";
import { AgentDetailPage } from "../features/agents/AgentDetailPage";
import { LoginPage } from "../features/auth/LoginPage";
import { apiRequest } from "../api/client";
import type { User } from "../api/types";

const qc = new QueryClient();

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
      <Routes>
        <Route path="/" element={<Navigate to="/projects" replace />} />
        <Route path="/projects" element={<ProjectsPage />} />
        <Route path="/projects/:projectId" element={<ProjectHomePage />} />
        <Route path="/projects/:projectId/runs" element={<RunsPage />} />
        <Route path="/projects/:projectId/agents" element={<AgentsPage />} />
        <Route path="/projects/:projectId/agents/:agentId" element={<AgentDetailPage />} />
        <Route path="/runs/:runId" element={<RunDetailPage />} />
        <Route path="/experiments/:experimentId" element={<ExperimentDetailPage />} />
        <Route path="*" element={<Navigate to="/projects" replace />} />
      </Routes>
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
