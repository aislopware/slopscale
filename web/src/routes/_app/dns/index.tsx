import { createFileRoute, redirect } from "@tanstack/react-router";

/** DNS is a branch of the sidebar; its address opens the first page under it. */
export const Route = createFileRoute("/_app/dns/")({
  beforeLoad: () => {
    throw redirect({ to: "/dns/nameservers", replace: true });
  },
});
