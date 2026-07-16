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
import { LoginPage } from "../features/auth/LoginPage";
import { apiRequest } from "../api/client";
import type { AuthResponse } from "../api/types";

const qc = new QueryClient();

function useAuth() {
  const [user, setUser] = useState<{ id: string; email: string } | null>(
    () => JSON.parse(sessionStorage.getItem("agentscope_user") ?? "null"),
  );

  const login = useCallback(async (email: string, password: string) => {
    const res = await apiRequest<AuthResponse>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    });
    sessionStorage.setItem("agentscope_user", JSON.stringify(res.user));
    setUser(res.user);
  }, []);

  const register = useCallback(
    async (email: string, password: string, displayName: string) => {
      await apiRequest("/api/auth/register", {
        method: "POST",
        body: JSON.stringify({ email, password, display_name: displayName }),
      });
      await login(email, password);
    },
    [login],
  );

  const logout = useCallback(() => {
    sessionStorage.removeItem("agentscope_user");
    setUser(null);
  }, []);

  return { user, login, register, logout };
}

function HomePage() {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold text-slate-900">Welcome to AgentScope</h1>
      <p className="text-slate-600">Select a section from the sidebar.</p>
    </div>
  );
}

function AppRoutes() {
  const { user, login, register, logout: _logout } = useAuth();

  if (!user) {
    return <LoginPage onLogin={login} onRegister={register} />;
  }

  return (
    <AppShell>
      <Routes>
        <Route path="/" element={<HomePage />} />
        <Route path="/projects" element={<ProjectsPage />} />
        <Route path="/projects/:projectId" element={<ProjectHomePage />} />
        <Route path="/runs" element={<div>Runs</div>} />
        <Route path="/runs/:runId" element={<div>Run Detail</div>} />
        <Route path="/datasets" element={<div>Datasets</div>} />
        <Route path="/experiments" element={<div>Experiments</div>} />
        <Route path="/experiments/:experimentId" element={<div>Experiment Detail</div>} />
        <Route path="*" element={<Navigate to="/" replace />} />
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
