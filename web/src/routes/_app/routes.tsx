import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { networksQuery, nodesQuery } from "~/api/queries.ts";
import { RoutesTab } from "~/components/networks/routes-tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { pendingRouteCount } from "~/lib/node.ts";
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

  const pending = nodes.reduce((sum, node) => sum + pendingRouteCount(node), 0);

  return (
    <>
      <PageHeader
        title="Routes"
        description="Every route any machine advertises, approved or waiting. Routes a network owns are approved by that network."
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
