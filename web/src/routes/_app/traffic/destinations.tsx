import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useCallback, useState } from "react";
import type { ReactElement } from "react";
import { object } from "valibot";

import { nodesQuery } from "~/api/queries.ts";
import { trafficDestinationsQuery, trafficReportersQuery } from "~/api/traffic.ts";
import type { TrafficDestination, TrafficScope } from "~/api/traffic.ts";
import { can } from "~/auth/me.ts";
import type { FilterChip } from "~/components/machines/filter-chips.tsx";
import { tablePageSize } from "~/components/table/app-table.tsx";
import { ColumnsMenu, LimitSelect, useHiddenColumns } from "~/components/table/log-view.tsx";
import type { HiddenColumns } from "~/components/table/log-view.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableGroup } from "~/components/table/toolbar.tsx";
import {
  DestinationsTable,
  destinationColumnChoices,
} from "~/components/traffic/destinations-table.tsx";
import { NothingMatches } from "~/components/traffic/nothing-matches.tsx";
import { windowOf } from "~/components/traffic/range.ts";
import { useSearchDraft } from "~/components/traffic/search-draft.ts";
import {
  destinationChips,
  destinationFilters,
  destinationSearchEntries,
  destinationTabs,
  noDestinationFilters,
  pickDestination,
  toDestinationGrouping,
} from "~/components/traffic/search.ts";
import type { DestinationSearch } from "~/components/traffic/search.ts";
import { WindowHeader } from "~/components/traffic/window-header.tsx";
import { isRefusedWindow, loadWindow } from "~/components/traffic/window-refusal.tsx";
import {
  GroupBySelect,
  WindowToolbar,
  pickedFilter,
} from "~/components/traffic/window-toolbar.tsx";
import { nodeLabel } from "~/lib/node.ts";

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

    return node === undefined ? `Machine ${id}` : nodeLabel(node);
  };

  const chips = destinationChips(search, machineName, setSearch);
  const [pageSize, setPageSize] = useState(tablePageSize);
  const columns = useHiddenColumns("slopscale-admin.traffic.destinations.hidden-columns");

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
        query={
          <SearchInput
            value={draft}
            placeholder="Search hosts and addresses"
            onValueChange={setDraft}
          />
        }
        filters={chips.map((chip) => pickedFilter(chip))}
        onClearFilters={(window) => {
          setSearch({ ...search, ...window, ...noDestinationFilters, q: search.q });
        }}
        actions={
          <>
            <GroupBySelect
              label="Group destinations by"
              tabs={destinationTabs}
              value={search.by}
              onChange={(value) => {
                void navigate({
                  search: (previous) => ({ ...previous, by: toDestinationGrouping(value) }),
                  replace: true,
                });
              }}
            />
            <LimitSelect value={pageSize} onChange={setPageSize} />
            <ColumnsMenu
              choices={destinationColumnChoices(search.by, search.node !== "")}
              columns={columns}
            />
          </>
        }
      />
      {isRefusedWindow(destinations.error) ? null : (
        <DestinationsFrame
          search={search}
          rows={destinations.data?.destinations ?? []}
          chips={chips}
          onSearch={setSearch}
          pageSize={pageSize}
          columns={columns}
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
  pageSize,
  columns,
}: {
  readonly search: DestinationSearch;
  readonly rows: readonly TrafficDestination[];
  readonly chips: FilterChip[];
  readonly onSearch: (next: DestinationSearch) => void;
  readonly pageSize: number;
  readonly columns: HiddenColumns;
}): ReactElement {
  const navigate = useNavigate({ from: Route.fullPath });
  const whole = rows.reduce((sum, row) => sum + row.txBytes + row.rxBytes, 0);
  const clear = (): void => {
    onSearch({ ...search, ...noDestinationFilters });
  };

  return (
    <TableGroup>
      <DestinationsTable
        rows={rows}
        groupBy={search.by}
        whole={whole}
        of="the rows shown"
        oneMachine={search.node !== ""}
        pageSize={pageSize}
        hidden={columns.hidden}
        empty={
          chips.length === 0 && search.q === "" ? undefined : (
            <NothingMatches
              description="No destination in the window matches these filters."
              onClear={clear}
            />
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
    </TableGroup>
  );
}
