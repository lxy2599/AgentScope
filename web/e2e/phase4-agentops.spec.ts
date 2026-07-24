import fs from "node:fs";
import path from "node:path";
import { expect, test, type Page, type TestInfo } from "@playwright/test";

interface Phase4Result {
  email: string;
  password: string;
  project_id: string;
  run_id: string;
  candidate_experiment_id: string;
  calibration_id: string;
  comparison_id: string;
}

function readResult(): Phase4Result {
  const resultPath = process.env.PHASE4_RESULT_FILE
    || path.resolve("../.external/phase4-agentops-result.json");
  return JSON.parse(fs.readFileSync(resultPath, "utf-8")) as Phase4Result;
}

async function assertNoDocumentOverflow(page: Page): Promise<void> {
  const dimensions = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
  }));
  expect(
    dimensions.scrollWidth,
    `document overflows by ${dimensions.scrollWidth - dimensions.clientWidth}px`,
  ).toBeLessThanOrEqual(dimensions.clientWidth + 2);
}

async function capture(
  page: Page,
  testInfo: TestInfo,
  name: string,
): Promise<void> {
  await page.screenshot({
    path: testInfo.outputPath(`${name}.png`),
    fullPage: true,
  });
}

test("单一账号展示完整 Phase 4 AgentOps 闭环", async ({ page }, testInfo) => {
  const result = readResult();
  const failedResponses: string[] = [];
  page.on("response", (response) => {
    const url = response.url();
    const optionalSnapshotMissing =
      response.status() === 404
      && /\/api\/runs\/[^/]+\/snapshot(?:\?|$)/.test(url);
    if (
      response.status() >= 400
      && !optionalSnapshotMissing
      && (url.includes("/api/") || url.includes("/runner/") || url.includes("/v1/"))
    ) {
      failedResponses.push(`${response.status()} ${url}`);
    }
  });

  await page.goto("/");
  await page.getByLabel("邮箱").fill(result.email);
  await page.getByLabel("密码").fill(result.password);
  await page.locator("form").getByRole("button", { name: /登\s*录/ }).click();
  await expect(page).toHaveURL(/\/projects$/);

  await page.goto(`/projects/${result.project_id}/agents`);
  await expect(page.getByText("openhands-software-agent")).toBeVisible();
  await expect(page.getByLabel("实时状态：实时")).toBeVisible();
  await assertNoDocumentOverflow(page);
  await capture(page, testInfo, "01-agents");

  await page.goto(`/runs/${result.run_id}`);
  await expect(page.getByText(/Agent 时间线/)).toBeVisible();
  await expect(page.getByText("terminal", { exact: true }).first()).toBeVisible();
  await page.getByRole("tab", { name: "运行配置" }).click();
  await expect(page.getByText("runtime-contract-v2", { exact: true })).toBeVisible();
  await assertNoDocumentOverflow(page);
  await capture(page, testInfo, "02-openhands-run");

  await page.goto(`/experiments/${result.candidate_experiment_id}`);
  await expect(page.getByRole("heading", { name: "实验报告" })).toBeVisible();
  await expect(page.getByText("评测结果明细")).toBeVisible();
  await page.getByRole("button", { name: "Judge" }).first().click();
  await expect(page.getByText("Judge 投票与聚合审计")).toBeVisible();
  await expect(page.getByText(/有效票/).first()).toBeVisible();
  await page.getByRole("button", { name: "Close" }).click();
  await assertNoDocumentOverflow(page);
  await capture(page, testInfo, "03-multi-judge");

  await page.goto(`/projects/${result.project_id}/eval-calibrations`);
  await expect(page.getByRole("heading", { name: /Judge 校准/ })).toBeVisible();
  await expect(page.getByText(result.calibration_id.slice(0, 8), { exact: false })).toBeVisible();
  await assertNoDocumentOverflow(page);

  await page.goto(`/comparisons/${result.comparison_id}`);
  await expect(page.getByRole("heading", { name: /实验对比报告/ })).toBeVisible();
  await expect(page.getByText("统计显著性")).toBeVisible();
  await expect(page.getByText("部分运行没有不可变快照，无法比较运行配置")).toBeVisible();
  await assertNoDocumentOverflow(page);
  await capture(page, testInfo, "04-comparison");

  await page.goto(`/projects/${result.project_id}/ingest`);
  await expect(page.getByRole("heading", { name: /采集健康/ })).toBeVisible();
  await expect(page.getByText("SDK 导出背压")).toBeVisible();
  await expect(page.getByText("运行异常")).toBeVisible();
  await expect(page.getByText("项目配额")).toBeVisible();
  await assertNoDocumentOverflow(page);
  await capture(page, testInfo, "05-ingest-governance");

  expect(failedResponses, failedResponses.join("\n")).toEqual([]);
});
