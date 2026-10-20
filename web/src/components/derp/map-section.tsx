import { Button } from "@cloudflare/kumo/components/button";
import { Table } from "@cloudflare/kumo/components/table";
import { useDeferredValue, useMemo, useState } from "react";
import type { ReactElement } from "react";

import type { Derp } from "~/api/queries.ts";
import { sourceLabels } from "~/components/derp/model.ts";
import type { RegionSource } from "~/components/derp/model.ts";
import { TableScroll } from "~/components/table/scroll-panel.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { tableClass, tableRowClass } from "~/components/table/styles.ts";
import { TableFooter, TableGroup, TableToolbar } from "~/components/table/toolbar.tsx";
import { FacetChips } from "~/components/ui/facet-chips.tsx";
import type { Facet } from "~/components/ui/facet-chips.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";

type Region = Derp["regions"][number];

type SourceFacet = "all" | RegionSource;

/** Matches a region on its id, code, name or where it came from, so any column can be searched. */
function matches(region: Region, query: string): boolean {
  const text =
    `${region.id} ${region.code} ${region.name} ${sourceLabels[region.source]}`.toLowerCase();

  return text.includes(query);
}

/** "All" and one facet per source the map has regions from, in the order the map merges them. */
function sourceFacets(regions: readonly Region[]): Facet<SourceFacet>[] {
  const counts = new Map<RegionSource, number>();

  for (const region of regions) {
    counts.set(region.source, (counts.get(region.source) ?? 0) + 1);
  }

  return [
    { value: "all", label: regions.length === 1 ? "Region" : "Regions", count: regions.length },
    ...[...counts].map(([source, count]) => ({
      value: source,
      label: sourceLabels[source],
      count,
    })),
  ];
}

/**
 * The merged map every machine receives, as the dashboard's Networking > Routes lays its routes
 * out: facet chips counting the regions per source, a search, and the table of regions.
 */
export function MapSection({ derp }: { readonly derp: Derp }): ReactElement {
  const [search, setSearch] = useState("");
  const [source, setSource] = useState<SourceFacet>("all");
  const query = useDeferredValue(search);
  const facets = useMemo(() => sourceFacets(derp.regions), [derp.regions]);
  const regions = useMemo(() => {
    const text = query.trim().toLowerCase();

    return derp.regions.filter(
      (region) =>
        (source === "all" || region.source === source) && (text === "" || matches(region, text)),
    );
  }, [derp.regions, query, source]);
  const total = derp.regions.length;

  if (total === 0) {
    return (
      <TableScroll>
        {() => (
          <SectionEmpty
            title="No regions"
            description="Machines must connect directly to each other."
          />
        )}
      </TableScroll>
    );
  }

  return (
    <>
      <div className="flex flex-col gap-3">
        <FacetChips label="Regions by source" facets={facets} value={source} onChange={setSource} />
        <TableToolbar>
          <SearchInput value={search} placeholder="Search regions" onValueChange={setSearch} />
        </TableToolbar>
      </div>
      <TableGroup>
        <TableScroll
          below={
            regions.length === 0 ? (
              <SectionEmpty
                title="No regions match"
                contents={
                  <Button
                    variant="secondary"
                    onClick={() => {
                      setSearch("");
                      setSource("all");
                    }}
                  >
                    Clear filters
                  </Button>
                }
              />
            ) : null
          }
        >
          {() => (
            <Table className={tableClass}>
              <Table.Header>
                <Table.Row>
                  <Table.Head className="w-16">ID</Table.Head>
                  <Table.Head>Code</Table.Head>
                  <Table.Head>Name</Table.Head>
                  <Table.Head className="text-right">Relays</Table.Head>
                  <Table.Head className="text-right">From</Table.Head>
                </Table.Row>
              </Table.Header>
              {regions.length === 0 ? null : (
                <Table.Body>
                  {regions.map((region) => (
                    <Table.Row key={region.id} className={tableRowClass}>
                      <Table.Cell className="font-mono tabular-nums">{region.id}</Table.Cell>
                      <Table.Cell className="font-mono">{region.code}</Table.Cell>
                      <Table.Cell>{region.name}</Table.Cell>
                      <Table.Cell className="text-right tabular-nums">{region.nodes}</Table.Cell>
                      <Table.Cell className="text-right">
                        <span className="text-kumo-subtle">{sourceLabels[region.source]}</span>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              )}
            </Table>
          )}
        </TableScroll>
        <TableFooter>
          {regions.length === total
            ? `Showing ${total === 1 ? "1 region" : `${total} regions`}`
            : `Showing ${regions.length} of ${total} regions`}
        </TableFooter>
      </TableGroup>
    </>
  );
}
