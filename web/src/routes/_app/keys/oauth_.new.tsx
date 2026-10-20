import { createFileRoute, redirect } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { can } from "~/auth/me.ts";
import { CreateOAuthClientPage } from "~/components/keys/oauth-page.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/keys/oauth_/new")({
  // A reader who may not create clients has nothing to do here; the list says why its button is off.
  beforeLoad: ({ context }) => {
    if (!can(context.me, "oauth_keys")) {
      throw redirect({ to: "/keys/oauth", replace: true });
    }
  },
  component: NewOAuthClientPage,
});

function NewOAuthClientPage(): ReactElement {
  const { me } = Route.useRouteContext();

  useBreadcrumb("Create");

  return <CreateOAuthClientPage me={me} />;
}
