import { createFileRoute, redirect } from "@tanstack/react-router";

/** Access controls is a branch of the sidebar; its address opens the first page under it. */
export const Route = createFileRoute("/_app/policy/")({
  beforeLoad: () => {
    throw redirect({ to: "/policy/rules", replace: true });
  },
});
