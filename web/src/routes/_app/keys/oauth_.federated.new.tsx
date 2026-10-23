import { createFileRoute, redirect } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { can } from "~/auth/me.ts";
import { CreateFederatedIdentityPage } from "~/components/keys/federated-page.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/keys/oauth_/federated/new")({
  // A reader who may not create identities has nothing to do here; the list says why its button is off.
  beforeLoad: ({ context }) => {
    if (!can(context.me, "oauth_keys")) {
      throw redirect({ to: "/keys/oauth", replace: true });
    }
  },
  component: NewFederatedIdentityPage,
});

function NewFederatedIdentityPage(): ReactElement {
  const { me } = Route.useRouteContext();

  useBreadcrumb("Create federated identity");

  return <CreateFederatedIdentityPage me={me} />;
}
