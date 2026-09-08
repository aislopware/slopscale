import { createFileRoute, redirect } from "@tanstack/react-router";

/** Relays is a branch of the sidebar; its address opens the first page under it. */
export const Route = createFileRoute("/_app/relays/")({
  beforeLoad: () => {
    throw redirect({ to: "/relays/map", replace: true });
  },
});
