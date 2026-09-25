import { Button } from "@cloudflare/kumo/components/button";
import { Select } from "@cloudflare/kumo/components/select";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useCallback } from "react";
import type { ReactElement, ReactNode } from "react";
import { object } from "valibot";

import { nodesQuery } from "~/api/queries.ts";
import type { Node } from "~/api/queries.ts";
import { trafficNamesQuery, trafficReportersQuery, trafficSettingsQuery } from "~/api/traffic.ts";
import type { TrafficName, TrafficScope } from "~/api/traffic.ts";
import { can } from "~/auth/me.ts";
import { FilterChips } from "~/components/machines/filter-chips.tsx";
import type { FilterChip } from "~/components/machines/filter-chips.tsx";
import { plural } from "~/components/overview/plural.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { NamesTable } from "~/components/traffic/names-table.tsx";
import { windowOf } from "~/components/traffic/range.ts";
import { useSearchDraft } from "~/components/traffic/search-draft.ts";
import { nameSearchEntries, pickName, toNameGrouping } from "~/components/traffic/search.ts";
import type { NameSearch } from "~/components/traffic/search.ts";
import { TextLink, WindowHeader } from "~/components/traffic/window-header.tsx";
import { loadWindow } from "~/components/traffic/window-refusal.tsx";
import { WindowToolbar } from "~/components/traffic/window-toolbar.tsx";
import { Frame } from "~/components/ui/frame.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";
import { nodeName } from "~/lib/node.ts";

/** The server's cap on one read. */
const allRows = 1000;

const searchSchema = object(nameSearchEntries);

function scopeOf(search: NameSearch): TrafficScope {
  return { ...windowOf(search), node: search.node };
}

function filtersOf(search: NameSearch): { groupBy: NameSearch["by"]; q: string; limit: number } {
  return { groupBy: search.by, q: search.q, limit: allRows };
}

export const Route = createFileRoute("/_app/traffic/dns")({
  validateSearch: searchSchema,
  loaderDeps: ({ search }) => search,
  loader: async ({ context, deps }) => {
    const scope = scopeOf(deps);
    const filters = filtersOf(deps);

    await loadWindow([
      context.queryClient.query(trafficNamesQuery(scope, filters)),
      context.queryClient.query(trafficReportersQuery),
      context.queryClient.query(trafficSettingsQuery),
    ]);
  },
  component: DnsPage,
});

const groupTabs = [
  { value: "name", label: "Names" },
  { value: "node", label: "Machines" },
];

function machineItems(nodes: readonly Node[]): { value: string; label: string }[] {
  return [
    { value: "", label: "All machines" },
    ...nodes
      .map((node) => ({ value: node.id, label: nodeName(node) }))
      .toSorted((left, right) => left.label.localeCompare(right.label)),
  ];
}

function DnsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const names = useQuery({
    ...trafficNamesQuery(scopeOf(search), filtersOf(search)),
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
  const setSearch = (next: NameSearch): void => {
    void navigate({ search: () => next });
  };
  const chips: FilterChip[] = [];

  // The machine picker says which machine it holds only when the caller may list machines; a
  // machine that came from a link is spelled out as a chip either way.
  if (search.node !== "" && nodes.data === undefined) {
    chips.push({
      name: "Machine",
      value: `Machine ${search.node}`,
      onRemove: () => {
        setSearch({ ...search, node: "" });
      },
    });
  }

  return (
    <>
      <WindowHeader
        title="DNS lookups"
        description="The names machines look up through the gateways' resolvers."
        window={names.data}
        reporters={reporters.data?.reporters ?? []}
        gateway={search.gateway}
      />
      <WindowToolbar
        search={search}
        failure={names.error}
        reporters={reporters.data?.reporters ?? []}
        onChange={(next) => {
          void navigate({ search: (previous) => ({ ...previous, ...next }) });
        }}
        actions={
          <Tabs
            variant="segmented"
            aria-label="Group lookups by"
            tabs={groupTabs}
            value={search.by}
            onValueChange={(value) => {
              void navigate({
                search: (previous) => ({ ...previous, by: toNameGrouping(value) }),
                replace: true,
              });
            }}
          />
        }
      >
        <SearchInput value={draft} placeholder="Search names" onValueChange={setDraft} />
        {nodes.data === undefined ? null : (
          <Select
            aria-label="Machine"
            className="w-44"
            value={search.node}
            items={machineItems(nodes.data.nodes)}
            onValueChange={(value) => {
              setSearch({ ...search, node: value ?? "" });
            }}
          />
        )}
      </WindowToolbar>
      <NamesFrame
        search={search}
        rows={names.data?.names ?? []}
        chips={chips}
        onSearch={setSearch}
      />
    </>
  );
}

function NamesFrame({
  search,
  rows,
  chips,
  onSearch,
}: {
  readonly search: NameSearch;
  readonly rows: readonly TrafficName[];
  readonly chips: FilterChip[];
  readonly onSearch: (next: NameSearch) => void;
}): ReactElement {
  const settings = useQuery(trafficSettingsQuery);
  const clear = (): void => {
    onSearch({ ...search, node: "", q: "" });
  };
  let empty: ReactNode = undefined;

  if (settings.data?.dnsLogging === false) {
    empty = (
      <SectionEmpty
        title="DNS logging is off"
        description="With it on, every gateway runs a resolver and the machines send their lookups to it, so this page can list what each machine looks up."
        contents={<TextLink to="/traffic/settings">Traffic settings</TextLink>}
      />
    );
  } else if (search.q !== "" || search.node !== "") {
    empty = (
      <SectionEmpty
        title="Nothing matches"
        description="No lookup in the window matches these filters."
        contents={
          <Button variant="secondary" onClick={clear}>
            Clear filters
          </Button>
        }
      />
    );
  }

  return (
    <Frame>
      <FilterChips chips={chips} onClearAll={clear} />
      <NamesTable
        rows={rows}
        groupBy={search.by}
        empty={empty}
        oneMachine={search.node !== ""}
        footer={
          rows.length === 0 ? undefined : (
            <TableFooter>
              {rows.length >= allRows
                ? `The ${allRows} most asked`
                : `Showing ${plural(rows.length, search.by === "node" ? "machine" : "name")}`}
            </TableFooter>
          )
        }
        onPick={(row) => {
          onSearch({ ...search, ...pickName(row, search.by) });
        }}
      />
    </Frame>
  );
}
