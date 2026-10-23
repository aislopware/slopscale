import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { groupsQuery, networksQuery, nodesQuery } from "~/api/queries.ts";
import { NetworksTab } from "~/components/networks/networks-tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { textSearchSchema } from "~/lib/search-text.ts";

export const Route = createFileRoute("/_app/networks/")({
  validateSearch: textSearchSchema,
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(networksQuery),
      context.queryClient.query(groupsQuery),
      context.queryClient.query(nodesQuery),
    ]);
  },
  component: NetworksPage,
});

function NetworksPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { networks, enforcing } = useSuspenseQuery(networksQuery).data;
  const { groups } = useSuspenseQuery(groupsQuery).data;
  const { nodes } = useSuspenseQuery(nodesQuery).data;
  const enabled = networks.filter((network) => network.enabled).length;

  const setSearch = (value: string): void => {
    void navigate({
      search: (current) => ({ ...current, q: value === "" ? undefined : value }),
      replace: true,
    });
  };

  return (
    <>
      <PageHeader
        title="Networks"
        docs="ref/networks"
        description="Subnets and exit nodes reached through routing machines. A network hands its routes only to the groups you pick."
        meta={enabled === 1 ? "1 network enabled" : `${enabled} networks enabled`}
      />
      <NetworksTab
        me={me}
        networks={networks}
        enforcing={enforcing}
        groups={groups}
        nodes={nodes}
        search={search.q ?? ""}
        onSearchChange={setSearch}
      />
    </>
  );
}
