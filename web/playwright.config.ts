import { defineConfig } from "@playwright/test";

const localNoProxy = ["127.0.0.1", "localhost"];
const configuredNoProxy = (process.env.NO_PROXY || process.env.no_proxy || "")
  .split(",")
  .map((entry) => entry.trim())
  .filter(Boolean);
const noProxy = [...new Set([...configuredNoProxy, ...localNoProxy])].join(",");
process.env.NO_PROXY = noProxy;
process.env.no_proxy = noProxy;

const webPort = process.env.PHASE4_WEB_PORT || "15173";
const baseURL = process.env.PHASE4_WEB_URL || `http://127.0.0.1:${webPort}`;
const apiURL = process.env.AGENTSCOPE_API_URL || "http://127.0.0.1:8080";

export default defineConfig({
  testDir: "./e2e",
  outputDir: "./test-results/phase4",
  timeout: 90_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  reporter: [["list"]],
  use: {
    baseURL,
    screenshot: { mode: "only-on-failure", fullPage: true },
    trace: "retain-on-failure",
  },
  projects: [
    {
      name: "desktop-chromium",
      use: {
        browserName: "chromium",
        viewport: { width: 1440, height: 900 },
      },
    },
    {
      name: "mobile-chromium",
      use: {
        browserName: "chromium",
        viewport: { width: 390, height: 844 },
        isMobile: true,
        hasTouch: true,
      },
    },
  ],
  webServer: {
    command: `npm run dev -- --host 127.0.0.1 --port ${webPort}`,
    url: baseURL,
    reuseExistingServer: false,
    timeout: 60_000,
    stdout: "pipe",
    stderr: "pipe",
    env: {
      AGENTSCOPE_API_URL: apiURL,
      NO_PROXY: noProxy,
      no_proxy: noProxy,
    },
  },
});
