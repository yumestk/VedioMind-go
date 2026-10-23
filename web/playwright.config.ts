import { defineConfig } from "@playwright/test";
const production = process.env.E2E_PRODUCTION === "1";
const apiServer = {
  command:
    "E2E_SERVER=1 GIN_MODE=release go test ../internal/app -run ^TestBrowserServer$ -count=1 -v -timeout=20m",
  url: "http://127.0.0.1:18080/api/health/ready",
  timeout: 60000,
  reuseExistingServer: false,
};
const viteServer = {
  command:
    "DEV_API_TARGET=http://127.0.0.1:18080 npm run dev -- --port 15173 --strictPort",
  url: "http://127.0.0.1:15173",
  timeout: 30000,
  reuseExistingServer: false,
};
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  timeout: 45000,
  expect: { timeout: 15000 },
  use: {
    baseURL: production ? "http://127.0.0.1:18080" : "http://127.0.0.1:15173",
    trace: "retain-on-failure",
  },
  webServer: production ? [apiServer] : [apiServer, viteServer],
});
