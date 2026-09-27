import { createFileRoute, redirect } from "@tanstack/react-router";

/** Traffic settings are a branch of the sidebar; its address opens the first page under it. */
export const Route = createFileRoute("/_app/settings/traffic/")({
  beforeLoad: () => {
    throw redirect({ to: "/settings/traffic/gateways", replace: true });
  },
});
