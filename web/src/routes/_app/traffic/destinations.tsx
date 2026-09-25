import { Button } from "@cloudflare/kumo/components/button";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useCallback } from "react";
import type { ReactElement } from "react";
import { object } from "valibot";

import { nodesQuery } from "~/api/queries.ts";
import { trafficDestinationsQuery, trafficReportersQuery } from "~/api/traffic.ts";
import type { TrafficDestination, TrafficScope } from "~/api/traffic.ts";
import { can } from "~/auth/me.ts";
import { FilterChips } from "~/components/machines/filter-chips.tsx";
import type { FilterChip } from "~/components/machines/filter-chips.tsx";
import { plural } from "~/components/overview/plural.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { GroupingPicker } from "~/components/traffic/destinations-section.tsx";
import { DestinationsTable, groupingLabels } from "~/components/traffic/destinations-table.tsx";
import { windowOf } from "~/components/traffic/range.ts";
import { useSearchDraft } from "~/components/traffic/search-draft.ts";
import {
  destinationChips,
  destinationFilters,
  destinationSearchEntries,
  destinationTabs,
  noDestinationFilters,
  pickDestination,
} from "~/components/traffic/search.ts";
import type { DestinationSearch } from "~/components/traffic/search.ts";
import { WindowHeader } from "~/components/traffic/window-header.tsx";
import { isRefusedWindow, loadWindow } from "~/components/traffic/window-refusal.tsx";
import { WindowToolbar } from "~/components/traffic/window-toolbar.tsx";
import { Frame } from "~/components/ui/frame.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";
import { nodeName } from "~/lib/node.ts";

/** The server's cap on one destinations read. */
const allRows = 1000;

const searchSchema = object(destinationSearchEntries);

function scopeOf(search: DestinationSearch): TrafficScope {
  return { ...windowOf(search), node: search.node };
}

export const Route = createFileRoute("/_app/traffic/destinations")({
  validateSearch: searchSchema,
  loaderDeps: ({ search }) => search,
  loader: async ({ context, deps }) => {
    const scope = scopeOf(deps);
    const filters = destinationFilters(deps, allRows);

    await loadWindow([
      context.queryClient.query(trafficDestinationsQuery(scope, filters)),
      context.queryClient.query(trafficReportersQuery),
    ]);
  },
  component: DestinationsPage,
});

function DestinationsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const destinations = useQuery({
    ...trafficDestinationsQuery(scopeOf(search), destinationFilters(search, allRows)),
    placeholderData: keepPreviousData,
  });
  const reporters = useQuery(trafficReportersQuery);
  const nodes = useQuery({ ...nodesQuery, enabled: can(me, "devices:core:read") });
  const [draft, setDraft] = useSearchDraft(
    search.q,
    useCallback(
      (q: string) => {
        void navigate({ search: (previous) => ({ ...previous, q }), replace: true });
      },
      [navigate],
    ),
  );
  const setSearch = (next: DestinationSearch): void => {
    void navigate({ search: () => next });
  };
  const machineName = (id: string): string => {
    const node = nodes.data?.nodes.find((candidate) => candidate.id === id);

    return node === undefined ? `Machine ${id}` : nodeName(node);
  };

  return (
    <>
      <WindowHeader
        title="Destinations"
        description="Where the machines connect through the gateways. Pick a row to narrow the list to it."
        window={destinations.data}
        reporters={reporters.data?.reporters ?? []}
        gateway={search.gateway}
      />
      <WindowToolbar
        search={search}
        failure={destinations.error}
        reporters={reporters.data?.reporters ?? []}
        onChange={(next) => {
          void navigate({ search: (previous) => ({ ...previous, ...next }) });
        }}
        actions={
          <GroupingPicker
            tabs={destinationTabs}
            value={search.by}
            onChange={(by) => {
              void navigate({ search: (previous) => ({ ...previous, by }), replace: true });
            }}
          />
        }
      >
        <SearchInput
          value={draft}
          placeholder="Search hosts and addresses"
          onValueChange={setDraft}
        />
      </WindowToolbar>
      {isRefusedWindow(destinations.error) ? null : (
        <DestinationsFrame
          search={search}
          rows={destinations.data?.destinations ?? []}
          chips={destinationChips(search, machineName, setSearch)}
          onSearch={setSearch}
        />
      )}
    </>
  );
}

function DestinationsFrame({
  search,
  rows,
  chips,
  onSearch,
}: {
  readonly search: DestinationSearch;
  readonly rows: readonly TrafficDestination[];
  readonly chips: FilterChip[];
  readonly onSearch: (next: DestinationSearch) => void;
}): ReactElement {
  const navigate = useNavigate({ from: Route.fullPath });
  const whole = rows.reduce((sum, row) => sum + row.txBytes + row.rxBytes, 0);
  const clear = (): void => {
    onSearch({ ...search, ...noDestinationFilters });
  };

  return (
    <Frame>
      <FilterChips chips={chips} onClearAll={clear} />
      <DestinationsTable
        rows={rows}
        groupBy={search.by}
        whole={whole}
        oneMachine={search.node !== ""}
        empty={
          chips.length === 0 ? undefined : (
            <SectionEmpty
              title="Nothing matches"
              description="No destination in the window matches these filters."
              contents={
                <Button variant="secondary" onClick={clear}>
                  Clear filters
                </Button>
              }
            />
          )
        }
        footer={
          rows.length === 0 ? undefined : (
            <TableFooter>
              {rows.length >= allRows
                ? `The ${allRows} busiest ${groupingLabels[search.by].toLowerCase()} rows`
                : `Showing ${plural(rows.length, "row")}`}
            </TableFooter>
          )
        }
        onPick={(row) => {
          if (search.by === "node") {
            void navigate({
              to: "/traffic/machines/$nodeId",
              params: { nodeId: row.nodeId },
              search: { ...windowOf(search), by: "host" },
            });

            return;
          }

          onSearch({ ...search, ...pickDestination(row, search.by) });
        }}
      />
    </Frame>
  );
}
