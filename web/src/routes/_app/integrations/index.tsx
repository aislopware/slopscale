import { createFileRoute, redirect } from "@tanstack/react-router";

import type { Me } from "~/auth/me.ts";
import { can } from "~/auth/me.ts";

function firstIntegrationPath(
  me: Me,
): "/integrations/webhooks" | "/integrations/log-streams" | "/integrations/posture" {
  if (can(me, "webhooks:read")) {
    return "/integrations/webhooks";
  }

  if (can(me, "logs:configuration:read")) {
    return "/integrations/log-streams";
  }

  return "/integrations/posture";
}

/**
 * Integrations is a branch of the sidebar; its address opens the first page under it the caller may
 * read.
 */
export const Route = createFileRoute("/_app/integrations/")({
  beforeLoad: ({ context }) => {
    throw redirect({
      to: firstIntegrationPath(context.me),
      replace: true,
    });
  },
});
