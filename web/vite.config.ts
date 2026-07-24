/// <reference types="vitest" />
import react from "@vitejs/plugin-react";
import { defineConfig, loadEnv } from "vite";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const apiTarget = env.AGENTSCOPE_API_URL || "http://127.0.0.1:8080";

  return {
    plugins: [react()],
    server: {
      port: 5173,
      proxy: {
        "/api": apiTarget,
        "/runner": apiTarget,
        "/v1": apiTarget,
      },
    },
    test: {
      environment: "jsdom",
      setupFiles: "./src/test/setup.ts",
      globals: true,
      // Ant Design rendering can exceed Vitest's 5s default on a cold CI worker.
      testTimeout: 15_000,
      exclude: ["e2e/**", "node_modules/**", "dist/**"],
    },
  };
});
