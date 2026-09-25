import { LinkButton } from "@cloudflare/kumo/components/button";
import { DesktopIcon } from "@phosphor-icons/react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";
import { object } from "valibot";

import {
  trafficDestinationsQuery,
  trafficNamesQuery,
  trafficReportersQuery,
  trafficSummaryQuery,
  trafficSettingsQuery,
} from "~/api/traffic.ts";
import type { DestinationGrouping, TrafficNode, TrafficScope } from "~/api/traffic.ts";
import { plural } from "~/components/overview/plural.ts";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { VolumeCell } from "~/components/traffic/cells.tsx";
import { DestinationsSection, whereGrouping } from "~/components/traffic/destinations-section.tsx";
import { trafficNodeName } from "~/components/traffic/machines-table.tsx";
import { NamesTable } from "~/components/traffic/names-table.tsx";
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
import { loadWindow } from "~/components/traffic/window-refusal.tsx";
import { WindowToolbar } from "~/components/traffic/window-toolbar.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

/** How many destinations and names the machine's page lists; the pages it links to list the rest. */
const destinationRows = 100;
const nameRows = 20;

const searchSchema = object({
  range: destinationSearchEntries.range,
  from: destinationSearchEntries.from,
  to: destinationSearchEntries.to,
  gateway: destinationSearchEntries.gateway,
  by: destinationSearchEntries.by,
});

interface MachineSearch extends TrafficWindowSearch {
  readonly by: DestinationGrouping;
}

function scopeOf(search: TrafficWindowSearch, node: string): TrafficScope {
  return { ...windowOf(search), node };
}

/** A machine's own page has no "by machine" view: it is the machine. */
function destinationsOf(search: MachineSearch): ReturnType<typeof destinationFilters> {
  return destinationFilters(
    { ...search, ...noDestinationFilters, by: whereGrouping(search.by) },
    destinationRows,
  );
}

const nameFilters = { groupBy: "name", q: "", limit: nameRows } as const;

export const Route = createFileRoute("/_app/traffic/machines/$nodeId")({
  validateSearch: searchSchema,
  loaderDeps: ({ search }) => search,
  loader: async ({ context, deps, params }) => {
    const scope = scopeOf(deps, params.nodeId);
    const destinations = destinationsOf(deps);

    await loadWindow([
      context.queryClient.query(trafficSummaryQuery(scope, 1)),
      context.queryClient.query(trafficDestinationsQuery(scope, destinations)),
      context.queryClient.query(trafficNamesQuery(scope, nameFilters)),
      context.queryClient.query(trafficReportersQuery),
      context.queryClient.query(trafficSettingsQuery),
    ]);
  },
  component: MachineTrafficPage,
});

function MachineTrafficPage(): ReactElement {
  const { nodeId } = Route.useParams();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data, error } = useQuery({
    ...trafficSummaryQuery(scopeOf(search, nodeId), 1),
    placeholderData: keepPreviousData,
  });
  const reporters = useQuery(trafficReportersQuery);
  const reporterList = reporters.data?.reporters ?? [];
  const self = data?.nodes.find((node) => node.nodeId === nodeId);
  const name = self === undefined ? `Machine ${nodeId}` : trafficNodeName(self);
  const whole = data === undefined ? 0 : data.total.txBytes + data.total.rxBytes;

  useBreadcrumb(name);

  return (
    <>
      <WindowHeader
        title={name}
        description="What this machine sent through the gateways, and where it went."
        window={data}
        reporters={reporterList}
        gateway={search.gateway}
        actions={
          self?.nodeName === "" ? undefined : (
            <LinkButton href={`/machines/${nodeId}`} variant="secondary" icon={DesktopIcon}>
              Machine
            </LinkButton>
          )
        }
      />
      <WindowToolbar
        search={search}
        failure={error}
        reporters={reporterList}
        onChange={(next) => {
          void navigate({ search: (previous) => ({ ...previous, ...next }) });
        }}
      />
      <TrafficSummaryPanel
        summary={data ?? emptySummary}
        loading={data === undefined && error === null}
        onZoom={(from, to) => {
          void navigate({ search: (previous) => ({ ...previous, range: "custom", from, to }) });
        }}
      />
      <MachineDestinationsSection nodeId={nodeId} search={search} whole={whole} />
      <GatewaysSection gateways={data?.reporters ?? []} whole={whole} />
      <MachineNamesSection nodeId={nodeId} search={search} />
    </>
  );
}

function MachineDestinationsSection({
  nodeId,
  search,
  whole,
}: {
  readonly nodeId: string;
  readonly search: MachineSearch;
  readonly whole: number;
}): ReactElement {
  const navigate = useNavigate({ from: Route.fullPath });
  const groupBy = whereGrouping(search.by);
  const destinations = useQuery({
    ...trafficDestinationsQuery(scopeOf(search, nodeId), destinationsOf(search)),
    placeholderData: keepPreviousData,
  });
  const rows = destinations.data?.destinations ?? [];

  return (
    <DestinationsSection
      title="Destinations"
      description="Where this machine's traffic went. Pick a row to see every machine that reached it."
      rows={rows}
      groupBy={groupBy}
      whole={whole}
      oneMachine
      footer={
        rows.length === 0 ? undefined : (
          <TableFooter
            actions={
              <TextLink
                to="/traffic/destinations"
                search={{ ...windowOf(search), ...noDestinationFilters, by: groupBy, node: nodeId }}
              >
                Open in destinations
              </TextLink>
            }
          >
            {`Showing ${plural(rows.length, "row")}`}
          </TableFooter>
        )
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
            ...pickDestination(row, groupBy),
          },
        });
      }}
    />
  );
}

/** Which gateways carried the machine's traffic; only worth a section when there is a choice. */
function GatewaysSection({
  gateways,
  whole,
}: {
  readonly gateways: readonly TrafficNode[];
  readonly whole: number;
}): ReactElement | null {
  if (gateways.length < 2) {
    return null;
  }

  const widest = Math.max(...gateways.map((row) => row.txBytes + row.rxBytes));

  return (
    <Section title="Gateways" description="Which gateways this machine's traffic went through.">
      <DefinitionList
        items={gateways.map((gateway) => ({
          key: gateway.nodeId,
          label: trafficNodeName(gateway),
          value: (
            <VolumeCell bytes={gateway.txBytes + gateway.rxBytes} widest={widest} whole={whole} />
          ),
        }))}
      />
    </Section>
  );
}

function MachineNamesSection({
  nodeId,
  search,
}: {
  readonly nodeId: string;
  readonly search: MachineSearch;
}): ReactElement {
  const navigate = useNavigate({ from: Route.fullPath });
  const names = useQuery({
    ...trafficNamesQuery(scopeOf(search, nodeId), nameFilters),
    placeholderData: keepPreviousData,
  });
  const settings = useQuery(trafficSettingsQuery);
  const rows = names.data?.names ?? [];

  return (
    <Section
      title="DNS lookups"
      description="The names this machine looked up through the gateways' resolvers, most asked first."
      panel={false}
    >
      <NamesTable
        rows={rows}
        groupBy="name"
        oneMachine
        empty={
          settings.data?.dnsLogging === false ? (
            <SectionEmpty
              title="DNS logging is off"
              description="Turn it on in the traffic settings to see what machines look up."
            />
          ) : undefined
        }
        footer={
          rows.length === 0 ? undefined : (
            <TableFooter
              actions={
                <TextLink
                  to="/traffic/dns"
                  search={{ ...windowOf(search), by: "name", q: "", node: nodeId }}
                >
                  All lookups
                </TextLink>
              }
            >
              {`The ${nameRows} most asked`}
            </TableFooter>
          )
        }
        onPick={(row) => {
          void navigate({
            to: "/traffic/dns",
            search: { ...windowOf(search), by: "node", q: row.name, node: "" },
          });
        }}
      />
    </Section>
  );
}
