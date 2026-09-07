import { playwright } from "@vitest/browser-playwright";
import { mergeConfig } from "vitest/config";

import viteConfig from "./vite.config.ts";

// Tests run in a real Chromium through Playwright so component behaviour
// (focus, keyboard, portals) matches what operators see.
export default mergeConfig(viteConfig, {
  test: {
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["./src/test-setup.ts"],
    browser: {
      enabled: true,
      headless: true,
      provider: playwright(),
      // Desktop width: below 768px Kumo's Sidebar becomes a sheet and the
      // navigation is not in the tree until it is opened.
      viewport: { width: 1280, height: 800 },
      instances: [{ browser: "chromium" }],
    },
  },
});
