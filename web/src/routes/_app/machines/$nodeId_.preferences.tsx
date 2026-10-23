import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import { can } from "~/auth/me.ts";
import { MachinePreferences } from "~/components/machines/preferences-page.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";
import { nodeName } from "~/lib/node.ts";

export const Route = createFileRoute("/_app/machines/$nodeId_/preferences")({
  loader: async ({ context, params }) => {
    await context.queryClient.query(
      api.queryOptions("get", "/api/v1/node/{nodeId}", {
        params: { path: { nodeId: params.nodeId } },
      }),
    );
  },
  component: PreferencesPage,
});

function PreferencesPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { nodeId } = Route.useParams();
  const { node } = useSuspenseQuery(
    api.queryOptions("get", "/api/v1/node/{nodeId}", { params: { path: { nodeId } } }),
  ).data;
  const name = nodeName(node);

  useBreadcrumb("Preferences", { label: name, to: `/machines/${node.id}` });

  return (
    <FormLayout>
      <PageHeader
        title={name}
        kicker="Preferences"
        docs="ref/device-management"
        description="These go straight to the machine's own client, which applies them at once and may refuse one it does not allow. Only what you change is sent."
      />
      <MachinePreferences node={node} canEdit={can(me, "devices:core")} />
    </FormLayout>
  );
}
