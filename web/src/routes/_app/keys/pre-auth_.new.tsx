import { createFileRoute, redirect } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { can } from "~/auth/me.ts";
import { CreatePreAuthKeyPage } from "~/components/keys/preauth-page.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/keys/pre-auth_/new")({
  // A reader who may not create keys has nothing to do here; the list says why its button is off.
  beforeLoad: ({ context }) => {
    if (!can(context.me, "auth_keys")) {
      throw redirect({ to: "/keys/pre-auth", replace: true });
    }
  },
  component: NewPreAuthKeyPage,
});

function NewPreAuthKeyPage(): ReactElement {
  const { me } = Route.useRouteContext();

  useBreadcrumb("Create");

  return <CreatePreAuthKeyPage me={me} />;
}
