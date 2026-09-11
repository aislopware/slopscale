import { defineConfig } from "blume";

export default defineConfig({
  title: "slopscale",
  description:
    "An open source, self-hosted implementation of the Tailscale control server, with a built-in admin console.",
  logo: { image: "/mark.svg", text: "slopscale" },
  content: { root: "content" },
  github: { owner: "aislopware", repo: "slopscale", dir: "docs" },
  deployment: { site: "https://aislopware.github.io", base: "/slopscale" },
  theme: { accent: "orange", radius: "md", mode: "system" },
  seo: {
    og: { logo: "/mark.svg", palette: { accent: "#f6821f" } },
    software: { license: "BSD-3-Clause", operatingSystem: "Linux", price: 0 },
  },
  navigation: { sidebar: { display: "group" } },
  toc: true,
  lastModified: true,
  redirects: [
    { from: "/acls", to: "/ref/policy" },
    { from: "/ref/acls", to: "/ref/policy" },
    { from: "/android-client", to: "/usage/connect/android" },
    { from: "/apple-client", to: "/usage/connect/apple" },
    { from: "/dns-records", to: "/ref/dns" },
    { from: "/exit-node", to: "/ref/routes" },
    { from: "/faq", to: "/about/faq" },
    { from: "/iOS-client", to: "/usage/connect/apple#ios" },
    { from: "/oidc", to: "/ref/oidc" },
    { from: "/ref/exit-node", to: "/ref/routes" },
    { from: "/ref/remote-cli", to: "/ref/api#remote-control" },
    { from: "/remote-cli", to: "/ref/api#remote-control" },
    { from: "/reverse-proxy", to: "/ref/integration/reverse-proxy" },
    { from: "/tls", to: "/ref/tls" },
    { from: "/windows-client", to: "/usage/connect/windows" },
  ],
});
