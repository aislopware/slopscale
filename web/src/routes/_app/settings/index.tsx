import { createFileRoute, redirect } from "@tanstack/react-router";

import { can } from "~/auth/me.ts";

/**
 * Settings is a branch of the sidebar; its address opens the first page under it the caller may
 * read. A member has only their sessions there.
 */
export const Route = createFileRoute("/_app/settings/")({
  beforeLoad: ({ context }) => {
    throw redirect({
      to: can(context.me, "feature_settings:read") ? "/settings/tailnet" : "/settings/sessions",
      replace: true,
    });
  },
});
