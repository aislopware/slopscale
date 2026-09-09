import { createFileRoute } from "@tanstack/react-router";

import { RouteNotFound } from "~/components/ui/error-page.tsx";

/**
 * Every path the console does not know, matched inside the app layout so the "not found" page keeps
 * the sidebar and the operator can simply click elsewhere.
 */
export const Route = createFileRoute("/_app/$")({
  component: RouteNotFound,
});
