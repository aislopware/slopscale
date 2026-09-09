import { Button } from "@cloudflare/kumo/components/button";
import { Table } from "@cloudflare/kumo/components/table";
import { useDeferredValue, useMemo, useState } from "react";
import type { ReactElement } from "react";

import type { Derp } from "~/api/queries.ts";
import { sourceLabels } from "~/components/derp/model.ts";
import { TableScroll } from "~/components/table/scroll-panel.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { frameTableClass, frameTableRowClass } from "~/components/ui/frame.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";

type Region = Derp["regions"][number];

/** Matches a region on its id, code, name or where it came from, so any column can be searched. */
function matches(region: Region, query: string): boolean {
  const text =
    `${region.id} ${region.code} ${region.name} ${sourceLabels[region.source]}`.toLowerCase();

  return text.includes(query);
}

/** The merged map every machine receives, one row per region. */
export function MapSection({ derp }: { readonly derp: Derp }): ReactElement {
  const [search, setSearch] = useState("");
  const query = useDeferredValue(search);
  const regions = useMemo(() => {
    const text = query.trim().toLowerCase();

    return text === "" ? derp.regions : derp.regions.filter((region) => matches(region, text));
  }, [derp.regions, query]);

  return (
    <Section
      title="The map machines receive"
      description="Every region after merging the sources, the relays you run and the embedded relay."
      bodyClassName="p-0"
      panel={regions.length === 0}
      {...(derp.regions.length === 0
        ? {}
        : {
            actions: (
              <SearchInput
                className="max-w-56"
                value={search}
                placeholder="Search regions"
                onValueChange={setSearch}
              />
            ),
          })}
    >
      {derp.regions.length === 0 ? (
        <SectionEmpty
          title="No regions"
          description="Machines must connect directly to each other."
        />
      ) : null}
      {derp.regions.length > 0 && regions.length === 0 ? (
        <SectionEmpty
          title="No regions match"
          contents={
            <Button
              variant="secondary"
              onClick={() => {
                setSearch("");
              }}
            >
              Clear search
            </Button>
          }
        />
      ) : null}
      {regions.length === 0 ? null : (
        <TableScroll>
          {() => (
            <Table className={frameTableClass}>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head className="w-16">ID</Table.Head>
                  <Table.Head>Code</Table.Head>
                  <Table.Head>Name</Table.Head>
                  <Table.Head className="text-right">Relays</Table.Head>
                  <Table.Head className="text-right">From</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {regions.map((region) => (
                  <Table.Row key={region.id} className={frameTableRowClass}>
                    <Table.Cell className="font-mono tabular-nums">{region.id}</Table.Cell>
                    <Table.Cell className="font-mono">{region.code}</Table.Cell>
                    <Table.Cell className="text-kumo-subtle">{region.name}</Table.Cell>
                    <Table.Cell className="text-right tabular-nums">{region.nodes}</Table.Cell>
                    <Table.Cell className="text-right">
                      <span className="text-kumo-subtle">{sourceLabels[region.source]}</span>
                    </Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
          )}
        </TableScroll>
      )}
    </Section>
  );
}
