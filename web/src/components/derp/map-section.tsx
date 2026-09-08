import { Badge } from "@cloudflare/kumo/components/badge";
import type { ReactElement } from "react";

import type { Derp } from "~/api/queries.ts";
import { sourceLabels } from "~/components/derp/model.ts";
import type { RegionSource } from "~/components/derp/model.ts";
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
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-xs text-kumo-subtle">
              <th scope="col" className="px-5 py-2 font-medium">
                Id
              </th>
              <th scope="col" className="px-2 py-2 font-medium">
                Code
              </th>
              <th scope="col" className="px-2 py-2 font-medium">
                Name
              </th>
              <th scope="col" className="px-2 py-2 text-right font-medium">
                Relays
              </th>
              <th scope="col" className="px-5 py-2 text-right font-medium">
                From
              </th>
            </tr>
          </thead>
          <tbody>
            {derp.regions.map((region) => (
              <tr key={region.id} className="border-t border-kumo-hairline">
                <td className="px-5 py-2 font-mono tabular-nums">{region.id}</td>
                <td className="px-2 py-2 font-mono">{region.code}</td>
                <td className="px-2 py-2 text-kumo-subtle">{region.name}</td>
                <td className="px-2 py-2 text-right tabular-nums">{region.nodes}</td>
                <td className="px-5 py-2 text-right">
                  <Badge variant={sourceVariants[region.source]}>
                    {sourceLabels[region.source]}
                  </Badge>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Section>
  );
}
