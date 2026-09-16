import { createFileRoute, redirect } from "@tanstack/react-router";

/**
 * Integrations is a branch of the sidebar; its address opens the first page under it. A caller who
 * may not read webhooks is moved on from there by the app layout's guard.
 */
export const Route = createFileRoute("/_app/integrations/")({
  beforeLoad: () => {
    throw redirect({ to: "/integrations/webhooks", replace: true });
  },
});
