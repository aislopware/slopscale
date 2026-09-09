import { defineConfig, devices } from "@playwright/test";

// The e2e suite runs the real server: cmd/dev builds slopscale with the
// console embedded, starts it with an in-process mock identity provider,
// and the browser signs in through that provider like it would through
// Google. Build the console first (bun run build) so the binary embeds it;
// bun run e2e does both.
const port = 18_080;
const baseURL = `http://127.0.0.1:${port}`;
const serverStartTimeoutMs = 240_000;

export default defineConfig({
  testDir: "e2e",
  // One server and one identity: the tests share state and run in order.
  fullyParallel: false,
  workers: 1,
  forbidOnly: process.env["CI"] !== undefined,
  reporter: "list",
  use: {
    baseURL,
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: `go run ./cmd/dev -port ${port}`,
    cwd: "..",
    url: `${baseURL}/health`,
    reuseExistingServer: process.env["CI"] === undefined,
    timeout: serverStartTimeoutMs,
    stdout: "ignore",
    stderr: "ignore",
  },
});
