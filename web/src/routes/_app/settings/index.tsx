import { createFileRoute, redirect } from "@tanstack/react-router";

/** Settings is a branch of the sidebar; its address opens the first page under it. */
export const Route = createFileRoute("/_app/settings/")({
  beforeLoad: () => {
    throw redirect({ to: "/settings/tailnet", replace: true });
  },
});
