import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { groupsQuery, networksQuery, nodesQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { NetworkEditor } from "~/components/networks/network-editor.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/networks/new")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(networksQuery),
      context.queryClient.query(groupsQuery),
      context.queryClient.query(nodesQuery),
    ]);
  },
  component: NewNetworkPage,
});

function NewNetworkPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { enforcing } = useSuspenseQuery(networksQuery).data;
  const { groups } = useSuspenseQuery(groupsQuery).data;
  const { nodes } = useSuspenseQuery(nodesQuery).data;

  useBreadcrumb("New network");

  return (
    <FormLayout>
      <PageHeader
        title="New network"
        docs="ref/networks"
        description="Prefixes reached through routing machines, approved on the routers and handed only to the groups you pick."
      />
      <NetworkEditor
        groups={groups}
        nodes={nodes}
        enforcing={enforcing}
        canEdit={can(me, "devices:routes")}
      />
    </FormLayout>
  );
}
