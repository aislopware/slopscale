import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import type { NodeDerpLatency, NodeNetInfo } from "~/api/schema.gen.ts";
import {
  homeLatency,
  linkLabel,
  msLabel,
  natLabel,
  natTone,
  otherLatencies,
  portMapLabel,
  yesNo,
} from "~/components/derp/latency-model.ts";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";
import { Section } from "~/components/ui/section.tsx";
import { Status } from "~/components/ui/status.tsx";

/**
 * How the machine reaches the rest of the tailnet: the relay region it homes on, and what its own
 * network report says about the path out. The client sends the report, so it is what the machine
 * believes rather than something the server measured.
 */
export function ConnectivitySection({ node }: { readonly node: Node }): ReactElement {
  const { netInfo } = node;

  if (netInfo === undefined) {
    return (
      <Section title="Connectivity">
        <p className="px-5 py-4 text-kumo-subtle">
          No network report yet; the client sends one when it connects.
        </p>
      </Section>
    );
  }

  const others = otherLatencies(netInfo);

  return (
    <Section title="Connectivity">
      <DefinitionList items={facts(netInfo)} />
      {others.length === 0 ? null : <OtherRegions regions={others} />}
    </Section>
  );
}

function facts(netInfo: NodeNetInfo): Definition[] {
  return [
    { key: "home", label: "Home relay", value: <HomeRelay netInfo={netInfo} /> },
    { key: "link", label: "Link", value: linkLabel(netInfo.linkType) },
    {
      key: "nat",
      label: "NAT",
      value: (
        <Status tone={natTone(netInfo.mappingVariesByDestIp)}>
          {natLabel(netInfo.mappingVariesByDestIp)}
        </Status>
      ),
    },
    { key: "udp", label: "UDP out", value: yesNo(netInfo.workingUdp) },
    { key: "ipv6", label: "IPv6", value: yesNo(netInfo.workingIpv6) },
    { key: "portmap", label: "Port mapping", value: portMapLabel(netInfo) },
  ];
}

/** The region the client picked, with its own measurement of it. */
function HomeRelay({ netInfo }: { readonly netInfo: NodeNetInfo }): ReactElement {
  const home = homeLatency(netInfo);

  if (home === undefined) {
    return (
      <span className="text-kumo-subtle">
        {netInfo.preferredDerpName === "" ? "Unknown" : netInfo.preferredDerpName}
      </span>
    );
  }

  return (
    <span className="flex min-w-0 items-baseline gap-2">
      <span className="font-mono text-[0.9em]">{home.code}</span>
      <span className="truncate">{home.name}</span>
      <span className="shrink-0 text-kumo-subtle tabular-nums">{msLabel(home.ms)}</span>
    </span>
  );
}

/** Every other region the client measured, nearest first, so a better home is easy to spot. */
function OtherRegions({ regions }: { readonly regions: readonly NodeDerpLatency[] }): ReactElement {
  return (
    <div className="border-t border-kumo-hairline px-5 py-3">
      <p className="pb-1.5 text-kumo-subtle">Other regions</p>
      <ul className="flex flex-col gap-1">
        {regions.map((region) => (
          <li key={region.regionId} className="flex items-baseline justify-between gap-4">
            <span className="flex min-w-0 items-baseline gap-2">
              <span className="font-mono text-[0.9em]">{region.code}</span>
              <span className="truncate text-kumo-subtle">{region.name}</span>
            </span>
            <span className="shrink-0 text-kumo-subtle tabular-nums">{msLabel(region.ms)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
