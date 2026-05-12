import { test, expect } from "@playwright/test";

function uid(): string {
  return Math.random().toString(36).slice(2, 10);
}

const GRAPHQL = "http://localhost:8080/graphql";

async function registerAgent(): Promise<{ token: string; name: string }> {
  const name = `test-${uid()}`;
  const extId = `ext-${uid()}`;
  const secret = "test-secret";

  const regRes = await fetch(GRAPHQL, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      operationName: "registerAgent",
      query: `mutation registerAgent($name: String!, $kind: AgentKind!, $externalID: String!, $secret: String!, $capabilities: [String!]) {
        registerAgent(name: $name, kind: $kind, externalID: $externalID, secret: $secret, capabilities: $capabilities) {
          agent { id name }
          token
        }
      }`,
      variables: { name, kind: "human", externalID: extId, secret, capabilities: [] },
    }),
  });
  const reg = await regRes.json();
  if (reg.errors) throw new Error(`Register failed: ${reg.errors[0].message}`);
  return { token: reg.data.registerAgent.token, name };
}

async function loginViaApi(token: string, page: import("@playwright/test").Page) {
  await page.goto("/");
  await page.evaluate((t) => localStorage.setItem("token", t), token);
}

test.describe("main workflow", () => {
  let token: string;

  test.beforeAll(async () => {
    const agent = await registerAgent();
    token = agent.token;
  });

  test("login and see dashboard", async ({ page }) => {
    await loginViaApi(token, page);
    await page.waitForURL("/");
    await expect(page.locator("h1").first()).toBeVisible();
  });

  test("create a project from dashboard", async ({ page }) => {
    await loginViaApi(token, page);
    await page.waitForURL("/");

    // Click first "创建项目" button
    await page.getByRole("button", { name: /创建项目/ }).first().click();

    // Fill project name (no htmlFor, use placeholder)
    await page.getByPlaceholder("输入项目名称").fill("E2E Test Project");
    await page.getByPlaceholder("项目描述").fill("Created by Playwright");

    // Click submit button (the one with just "创建" text)
    await page.getByRole("button", { name: /^创建$/ }).click();

    // Wait for success toast or project card
    await expect(page.getByText("E2E Test Project")).toBeVisible({ timeout: 5000 });
  });

  test("create an issue in a project", async ({ page }) => {
    await loginViaApi(token, page);
    await page.waitForURL("/");

    // Click on project card
    await page.getByText("E2E Test Project").first().click();
    await page.waitForURL(/\/projects\/\d+/);

    // Click "创建 Issue" button
    await page.getByRole("button", { name: /创建 Issue/ }).click();

    // Fill title using the Label htmlFor
    await page.getByLabel("标题").fill("E2E test issue");

    // Select priority from dropdown
    await page.getByLabel("优先级").click();
    await page.getByRole("option", { name: "中" }).click();

    // Submit
    await page.getByRole("button", { name: /^创建$/ }).click();

    // Wait for issue to appear on board
    await expect(page.getByText("E2E test issue")).toBeVisible({ timeout: 5000 });
  });

  test("view issue and add comment", async ({ page }) => {
    await loginViaApi(token, page);
    await page.waitForURL("/");

    // Navigate to project
    await page.getByText("E2E Test Project").first().click();
    await page.waitForURL(/\/projects\/\d+/);

    // Click on issue
    await page.getByText("E2E test issue").first().click();
    await page.waitForURL(/\/issues\/\d+/);

    // Verify issue detail
    await expect(page.getByText("E2E test issue")).toBeVisible();
    await expect(page.getByText("待处理")).toBeVisible();

    // Add a comment
    const textarea = page.locator("textarea").first();
    await textarea.fill("Comment from E2E test");

    // Send
    await page.getByRole("button", { name: /发送/ }).click();

    // Wait for comment to appear
    await expect(page.getByText("Comment from E2E test")).toBeVisible({ timeout: 5000 });
  });

  test("sidebar navigation", async ({ page }) => {
    await loginViaApi(token, page);
    await page.waitForURL("/");

    // Click "Agent" link in sidebar
    await page.getByRole("link", { name: "Agent" }).first().click();
    await page.waitForURL("/agents");
    await expect(page.locator("h1")).toContainText("Agent");

    // Back to dashboard
    await page.getByRole("link", { name: "首页" }).first().click();
    await page.waitForURL("/");
  });
});
