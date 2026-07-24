/// <reference types="vitest" />
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": "http://127.0.0.1:8080",
      "/runner": "http://127.0.0.1:8080",
      "/v1": "http://127.0.0.1:8080",
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: "./src/test/setup.ts",
    globals: true,
    // Ant Design table/card rendering can exceed Vitest's 5s default on a
    // cold or resource-constrained CI worker even when the assertion is
    // synchronous. Keep a bounded timeout that still catches hangs.
    testTimeout: 15_000,
  },
});
