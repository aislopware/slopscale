import { fileURLToPath } from "node:url";

import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The console is served by headscale under /admin/, so every asset URL is
// rooted there. In development, API calls and the sign-in flow are proxied
// to a local server; start it with -server-url set to this origin so the
// identity provider sends the browser back here (see cmd/dev).
const backend = process.env["HEADSCALE_URL"] ?? "http://127.0.0.1:8080";

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
    include: ["@cloudflare/kumo", "@cloudflare/kumo/components/*", "@cloudflare/kumo/utils"],
  },
  server: {
    proxy: {
      "/api": { target: backend, changeOrigin: true },
      "/oidc": { target: backend, changeOrigin: true },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
    target: "es2024",
  },
});
