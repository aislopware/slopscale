import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { groupsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { InvitePage } from "~/components/users/invite-page.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/users_/invite")({
  loader: async ({ context }) => {
    if (can(context.me, "policy_file:read")) {
      await context.queryClient.query(groupsQuery);
    }
  },
  component: InviteUserPage,
});

function InviteUserPage(): ReactElement {
  const { me } = Route.useRouteContext();

  useBreadcrumb("Invite a user");

  return <InvitePage me={me} />;
}
