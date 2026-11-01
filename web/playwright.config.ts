import { defineConfig, devices } from "@playwright/test";

// Browser tests against a real sentrix-server in demo mode: the embedded
// dashboard, real sessions and cookies, and simulated hosts shipping
// through the real ingest path. e2e/server.ts starts and stops it.
export default defineConfig({
  testDir: "e2e",
  testMatch: "*.e2e.ts",
  globalSetup: "./e2e/server.ts",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: !!process.env.CI,
  timeout: 60_000,
  expect: { timeout: 30_000 },
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: process.env.SENTRIX_E2E_URL ?? "http://127.0.0.1:18090",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
