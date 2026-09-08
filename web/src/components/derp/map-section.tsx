import { Badge } from "@cloudflare/kumo/components/badge";
import { Table } from "@cloudflare/kumo/components/table";
import type { ReactElement } from "react";

import type { Derp } from "~/api/queries.ts";
import { sourceLabels } from "~/components/derp/model.ts";
import type { RegionSource } from "~/components/derp/model.ts";
import { TableScroll } from "~/components/table/scroll-panel.tsx";
import { frameTableClass, frameTableRowClass } from "~/components/ui/frame.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";

const sourceVariants: Record<RegionSource, "secondary" | "info" | "success" | "neutral"> = {
  tailscale: "secondary",
  url: "secondary",
  file: "neutral",
  custom: "success",
  embedded: "info",
  config: "neutral",
};

/** The merged map every machine receives, one row per region. */
export function MapSection({ derp }: { readonly derp: Derp }): ReactElement {
  return (
    <Section
      title="Map machines receive"
      description="Every region after merging the sources, the relays you run and the embedded relay. A machine measures its latency to each region and keeps the closest one as home."
      bodyClassName="p-0"
    >
      {derp.regions.length === 0 ? (
        <SectionRow>
          <p className="text-kumo-subtle">No regions. Machines must connect directly.</p>
        </SectionRow>
      ) : (
        <TableScroll>
          {() => (
            <Table className={frameTableClass}>
              <Table.Header variant="compact" sticky>
                <Table.Row>
                  <Table.Head className="w-16">Id</Table.Head>
                  <Table.Head>Code</Table.Head>
                  <Table.Head>Name</Table.Head>
                  <Table.Head className="text-right">Relays</Table.Head>
                  <Table.Head className="text-right">From</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {derp.regions.map((region) => (
                  <Table.Row key={region.id} className={frameTableRowClass}>
                    <Table.Cell className="font-mono tabular-nums">{region.id}</Table.Cell>
                    <Table.Cell className="font-mono">{region.code}</Table.Cell>
                    <Table.Cell className="text-kumo-subtle">{region.name}</Table.Cell>
                    <Table.Cell className="text-right tabular-nums">{region.nodes}</Table.Cell>
                    <Table.Cell className="text-right">
                      <Badge variant={sourceVariants[region.source]}>
                        {sourceLabels[region.source]}
                      </Badge>
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
