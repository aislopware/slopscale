import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { networksQuery, nodesQuery } from "~/api/queries.ts";
import { RoutesTab } from "~/components/networks/routes-tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { textSearchSchema } from "~/lib/search-text.ts";

export const Route = createFileRoute("/_app/routes")({
  validateSearch: textSearchSchema,
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(nodesQuery),
      context.queryClient.query(networksQuery),
    ]);
  },
  component: RoutesPage,
});

function RoutesPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { nodes } = useSuspenseQuery(nodesQuery).data;
  const { networks } = useSuspenseQuery(networksQuery).data;

  const setSearch = (value: string): void => {
    void navigate({
      search: (current) => ({ ...current, q: value === "" ? undefined : value }),
      replace: true,
    });
  };

  const pending = nodes.reduce(
    (sum, node) =>
      sum + node.availableRoutes.filter((route) => !node.approvedRoutes.includes(route)).length,
    0,
  );

  return (
    <>
      <PageHeader
        title="Routes"
        description="Every route any machine advertises, approved or waiting. A route a network owns is approved by the network; the rest are approved here."
        meta={pending === 1 ? "1 route pending" : `${pending} routes pending`}
      />
      <RoutesTab
        me={me}
        nodes={nodes}
        networks={networks}
        search={search.q ?? ""}
        onSearchChange={setSearch}
      />
    </>
  );
}
