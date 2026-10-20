import { createFileRoute, Outlet } from "@tanstack/react-router";

import { requireSignIn } from "~/auth/guard.ts";

/**
 * Pages that take the whole screen, without the sidebar and the top bar: the Mesh "Add node"
 * wizard's frame, for a flow the operator should finish or leave rather than wander out of. The
 * guard is the app layout's, so a page here is behind the same sign-in and the same sidebar rule as
 * the page it was opened from; each page draws its own focused chrome.
 */
export const Route = createFileRoute("/_focus")({
  beforeLoad: requireSignIn,
  component: Outlet,
});
