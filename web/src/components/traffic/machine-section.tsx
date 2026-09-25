import { useQuery } from "@tanstack/react-query";
import type { ReactElement } from "react";

import { trafficSummaryQuery } from "~/api/traffic.ts";
import { formatBytes, formatCount } from "~/components/traffic/format.ts";
import { defaultTrafficRange } from "~/components/traffic/range.ts";
import type { TrafficWindowSearch } from "~/components/traffic/range.ts";
import { TextLink } from "~/components/traffic/window-header.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import { Section } from "~/components/ui/section.tsx";

const lastDay: TrafficWindowSearch = { range: defaultTrafficRange, from: "", to: "", gateway: "" };

/**
 * The machine's last day through the gateways, on its own page, with the way to the rest: what it
 * sent and received, and the page that says where it went.
 */
export function MachineTrafficSection({ nodeId }: { readonly nodeId: string }): ReactElement {
  const summary = useQuery(trafficSummaryQuery({ ...lastDay, node: nodeId }, 1));
  const total = summary.data?.total;

  return (
    <Section
      title="Traffic"
      description="The last 24 hours through the gateways."
      actions={
        <TextLink
          to="/traffic/machines/$nodeId"
          params={{ nodeId }}
          search={{ ...lastDay, by: "host" }}
        >
          Details
        </TextLink>
      }
    >
      {total === undefined || total.txBytes + total.rxBytes === 0 ? (
        <p className="px-5 py-4 text-kumo-subtle">
          {summary.isPending ? "Loading…" : "Nothing through a gateway in the last 24 hours."}
        </p>
      ) : (
        <DefinitionList
          items={[
            { key: "up", label: "Upload", value: formatBytes(total.txBytes) },
            { key: "down", label: "Download", value: formatBytes(total.rxBytes) },
            { key: "conns", label: "Connections", value: formatCount(total.conns) },
          ]}
        />
      )}
    </Section>
  );
}
