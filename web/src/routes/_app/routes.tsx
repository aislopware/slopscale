import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";
import { object, optional, pipe, transform, unknown } from "valibot";
import type { InferOutput } from "valibot";

import { networksQuery, nodesQuery } from "~/api/queries.ts";
import {
  routeFiltersFromSearch,
  searchFromRouteFilters,
} from "~/components/networks/routes-model.ts";
import type { RouteFilterState } from "~/components/networks/routes-model.ts";
import { RoutesTab } from "~/components/networks/routes-tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { pendingRouteCount } from "~/lib/node.ts";
import { optionalText } from "~/lib/search-text.ts";

/** Anything but `true` reads as off, so a chip is only ever on because a URL says so. */
function toFlag(value: unknown): true | undefined {
  return value === true ? true : undefined;
}

/**
 * The text search plus one parameter per chip, every one optional and absent while it is off. The
 * apps page links here with `pending=true` and, for one waiting connector, its name as `q`, so the
 * plain search keeps working on its own and the chips narrow it further.
 */
const optionalFlag = optional(pipe(unknown(), transform(toFlag)));

const searchSchema = object({
  q: optionalText,
  pending: optionalFlag,
  exit: optionalFlag,
  learned: optionalFlag,
  networks: optionalFlag,
});

type RoutesSearch = InferOutput<typeof searchSchema>;

/**
 * The whole search, built from what is set: an empty box and a chip that is off leave no parameter,
 * so `/routes` is the URL of the unfiltered page and the apps page's `/routes?q=…` link stays the
 * URL of a search for one connector.
 */
function toSearch(query: string, filters: RouteFilterState): RoutesSearch {
  return { ...(query === "" ? {} : { q: query }), ...searchFromRouteFilters(filters) };
}

export const Route = createFileRoute("/_app/routes")({
  validateSearch: searchSchema,
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

  const query = search.q ?? "";
  const filters = routeFiltersFromSearch(search);

  const setSearch = (value: string): void => {
    void navigate({ search: () => toSearch(value, filters), replace: true });
  };

  const setFilters = (next: RouteFilterState): void => {
    void navigate({ search: () => toSearch(query, next), replace: true });
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
        search={query}
        filters={filters}
        onSearchChange={setSearch}
        onFiltersChange={setFilters}
      />
    </>
  );
}
