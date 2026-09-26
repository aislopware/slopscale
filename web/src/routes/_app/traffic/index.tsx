import { createFileRoute, redirect } from "@tanstack/react-router";

/** Traffic is a branch of the sidebar; its address opens the first page under it. */
export const Route = createFileRoute("/_app/traffic/")({
  beforeLoad: () => {
    throw redirect({ to: "/traffic/overview", replace: true });
  },
});
