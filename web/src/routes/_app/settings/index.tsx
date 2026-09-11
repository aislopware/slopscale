import { createFileRoute, redirect } from "@tanstack/react-router";

/** Settings has no page of its own; its address opens the tailnet settings. */
export const Route = createFileRoute("/_app/settings/")({
  beforeLoad: () => {
    throw redirect({ to: "/settings/tailnet", replace: true });
  },
});
