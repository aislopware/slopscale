import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";
import { object } from "valibot";

import {
  trafficDestinationsQuery,
  trafficReportersQuery,
  trafficSummaryQuery,
} from "~/api/traffic.ts";
import type { DestinationGrouping, TrafficNode, TrafficScope } from "~/api/traffic.ts";
import { plural } from "~/components/overview/plural.ts";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { DestinationsSection } from "~/components/traffic/destinations-section.tsx";
import { MachinesTable } from "~/components/traffic/machines-table.tsx";
import { windowOf } from "~/components/traffic/range.ts";
import type { TrafficWindowSearch } from "~/components/traffic/range.ts";
import {
  destinationFilters,
  destinationSearchEntries,
  noDestinationFilters,
  pickDestination,
} from "~/components/traffic/search.ts";
import { TrafficSummaryPanel, emptySummary } from "~/components/traffic/summary.tsx";
import { TextLink, WindowHeader } from "~/components/traffic/window-header.tsx";
import { isRefusedWindow, loadWindow } from "~/components/traffic/window-refusal.tsx";
import { WindowToolbar } from "~/components/traffic/window-toolbar.tsx";
import { Callout } from "~/components/ui/callout.tsx";
import { Section } from "~/components/ui/section.tsx";

/** How many machines and destinations the overview lists; the pages under it list the rest. */
const topRows = 10;

const searchSchema = object({
  range: destinationSearchEntries.range,
  from: destinationSearchEntries.from,
  to: destinationSearchEntries.to,
  gateway: destinationSearchEntries.gateway,
  by: destinationSearchEntries.by,
});

interface OverviewSearch extends TrafficWindowSearch {
  readonly by: DestinationGrouping;
}

function scopeOf(search: OverviewSearch): TrafficScope {
  return { ...windowOf(search), node: "" };
}

function topFilters(search: OverviewSearch): ReturnType<typeof destinationFilters> {
  return destinationFilters({ ...search, ...noDestinationFilters }, topRows);
}

export const Route = createFileRoute("/_app/traffic/overview")({
  validateSearch: searchSchema,
  loaderDeps: ({ search }) => search,
  loader: async ({ context, deps }) => {
    const scope = scopeOf(deps);
    const filters = topFilters(deps);

    await loadWindow([
      context.queryClient.query(trafficSummaryQuery(scope, topRows)),
      context.queryClient.query(trafficDestinationsQuery(scope, filters)),
      context.queryClient.query(trafficReportersQuery),
    ]);
  },
  component: OverviewPage,
});

function OverviewPage(): ReactElement {
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data, error } = useQuery({
    ...trafficSummaryQuery(scopeOf(search), topRows),
    placeholderData: keepPreviousData,
  });
  const reporters = useQuery(trafficReportersQuery);
  const reporterList = reporters.data?.reporters ?? [];
  const whole = data === undefined ? 0 : data.total.txBytes + data.total.rxBytes;

  return (
    <>
      <WindowHeader
        title="Traffic"
        description="What the machines send through the gateways, and where it goes."
        window={data}
        reporters={reporterList}
        carried={data?.reporters}
        gateway={search.gateway}
      />
      {reporters.data !== undefined && reporterList.length === 0 ? (
        <Callout
          title="No gateway reports yet"
          description={
            <>
              Traffic shows up once the agent runs on a tagged exit node, subnet router or app
              connector. <TextLink to="/traffic/gateways">Set up a gateway</TextLink>.
            </>
          }
        />
      ) : null}
      <WindowToolbar
        search={search}
        failure={error}
        reporters={reporterList}
        onChange={(next) => {
          void navigate({ search: (previous) => ({ ...previous, ...next }) });
        }}
      />
      {isRefusedWindow(error) ? null : (
        <>
          <TrafficSummaryPanel
            summary={data ?? emptySummary}
            loading={data === undefined && error === null}
            onZoom={(from, to) => {
              void navigate({ search: (previous) => ({ ...previous, range: "custom", from, to }) });
            }}
          />
          <TopMachinesSection nodes={data?.nodes ?? []} whole={whole} search={search} />
          <TopDestinationsSection search={search} whole={whole} />
        </>
      )}
    </>
  );
}

function TopMachinesSection({
  nodes,
  whole,
  search,
}: {
  readonly nodes: readonly TrafficNode[];
  readonly whole: number;
  readonly search: OverviewSearch;
}): ReactElement {
  return (
    <Section
      title="Top machines"
      description="The machines that sent and received the most through the gateways."
      panel={false}
    >
      <MachinesTable
        nodes={nodes}
        whole={whole}
        search={search}
        footer={
          <TableFooter
            actions={
              <TextLink to="/traffic/machines" search={windowOf(search)}>
                All machines
              </TextLink>
            }
          >
            {nodes.length >= topRows
              ? `The ${topRows} busiest`
              : `Showing ${plural(nodes.length, "machine")}`}
          </TableFooter>
        }
      />
    </Section>
  );
}

function TopDestinationsSection({
  search,
  whole,
}: {
  readonly search: OverviewSearch;
  readonly whole: number;
}): ReactElement {
  const navigate = useNavigate({ from: Route.fullPath });
  const destinations = useQuery({
    ...trafficDestinationsQuery(scopeOf(search), topFilters(search)),
    placeholderData: keepPreviousData,
  });
  const rows = destinations.data?.destinations ?? [];

  return (
    <DestinationsSection
      title="Top destinations"
      description="Where that traffic went. Pick a row to see which machines reached it."
      rows={rows}
      groupBy={search.by}
      whole={whole}
      footer={
        <TableFooter
          actions={
            <TextLink
              to="/traffic/destinations"
              search={{ ...windowOf(search), ...noDestinationFilters, by: search.by }}
            >
              All destinations
            </TextLink>
          }
        >
          {rows.length >= topRows
            ? `The ${topRows} busiest`
            : `Showing ${plural(rows.length, "row")}`}
        </TableFooter>
      }
      onGroupChange={(by) => {
        void navigate({ search: (previous) => ({ ...previous, by }), replace: true });
      }}
      onPick={(row) => {
        void navigate({
          to: "/traffic/destinations",
          search: {
            ...windowOf(search),
            ...noDestinationFilters,
            by: "node",
            ...pickDestination(row, search.by),
          },
        });
      }}
    />
  );
}
