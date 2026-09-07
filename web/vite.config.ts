import { fileURLToPath } from "node:url";

import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The console is served by headscale under /admin/, so every asset URL is
// rooted there. In development, API calls are proxied to a local server.
export default defineConfig({
  base: "/admin/",
  plugins: [tanstackRouter({ target: "react", autoCodeSplitting: true }), react(), tailwindcss()],
  resolve: {
    alias: { "~": fileURLToPath(new URL("src", import.meta.url)) },
  },
  // Kumo is imported per component; pre-bundling every subpath up front keeps
  // the dev server from re-optimising (and briefly serving two React copies)
  // the first time a page pulls in a new one.
  optimizeDeps: {
    include: ["@cloudflare/kumo/components/*", "@cloudflare/kumo/utils"],
  },
  server: {
    proxy: {
      "/api": {
        target: process.env["HEADSCALE_URL"] ?? "http://127.0.0.1:8080",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
    target: "es2024",
  },
});
