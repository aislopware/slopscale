import { createFileRoute, redirect } from "@tanstack/react-router";

/**
 * Keys is a branch of the sidebar; its address opens the first page under it. A caller who may not
 * read pre-auth keys is moved on from there by the app layout's guard.
 */
export const Route = createFileRoute("/_app/keys/")({
  beforeLoad: () => {
    throw redirect({ to: "/keys/pre-auth", replace: true });
  },
});
