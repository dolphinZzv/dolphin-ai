import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  timeout: 30000,
  retries: 0,
  fullyParallel: false,
  workers: 1,
  use: {
    baseURL: "http://localhost:8080",
    headless: true,
    trace: "off",
    screenshot: "off",
    launchOptions: {
      args: ["--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage"],
    },
  },
});
