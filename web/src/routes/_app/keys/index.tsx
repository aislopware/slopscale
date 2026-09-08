import { createFileRoute, redirect } from "@tanstack/react-router";

import { can } from "~/auth/me.ts";

/** Keys is a branch of the sidebar; its address opens the first page under it the caller may read. */
export const Route = createFileRoute("/_app/keys/")({
  beforeLoad: ({ context }) => {
    throw redirect({
      to: can(context.me, "auth_keys:read") ? "/keys/pre-auth" : "/keys/api",
      replace: true,
    });
  },
});
