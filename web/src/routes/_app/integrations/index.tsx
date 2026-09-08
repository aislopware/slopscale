import { createFileRoute, redirect } from "@tanstack/react-router";

import { can } from "~/auth/me.ts";

/**
 * Integrations is a branch of the sidebar; its address opens the first page under it the caller may
 * read.
 */
export const Route = createFileRoute("/_app/integrations/")({
  beforeLoad: ({ context }) => {
    throw redirect({
      to: can(context.me, "webhooks:read") ? "/integrations/webhooks" : "/integrations/log-streams",
      replace: true,
    });
  },
});
