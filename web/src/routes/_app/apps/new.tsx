import { createFileRoute, redirect } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { appsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { AppEditor } from "~/components/apps/app-editor.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/apps/new")({
  // A reader who may not write apps has nothing to do here; the list says why its button is off.
  beforeLoad: ({ context }) => {
    if (!can(context.me, "policy_file")) {
      throw redirect({ to: "/apps", replace: true });
    }
  },
  // The connector field offers the tags the other apps name.
  loader: async ({ context }) => {
    await context.queryClient.query(appsQuery);
  },
  component: NewAppPage,
});

function NewAppPage(): ReactElement {
  const { me } = Route.useRouteContext();

  useBreadcrumb("New app");

  return (
    <FormLayout>
      <PageHeader
        title="New app"
        description="Domains reached through app connectors: the connectors resolve them and route every address they learn."
      />
      <AppEditor me={me} canEdit />
    </FormLayout>
  );
}
