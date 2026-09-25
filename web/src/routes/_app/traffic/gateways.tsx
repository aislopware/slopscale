import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { trafficReportersQuery } from "~/api/traffic.ts";
import type { TrafficReporter } from "~/api/traffic.ts";
import { can } from "~/auth/me.ts";
import { GatewaysTable, gatewayState } from "~/components/traffic/gateways-table.tsx";
import type { GatewayState } from "~/components/traffic/gateways-table.tsx";
import { InstallSection } from "~/components/traffic/install-section.tsx";
import { ResolverNotices } from "~/components/traffic/resolver-notices.tsx";
import { Frame } from "~/components/ui/frame.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/traffic/gateways")({
  loader: async ({ context }) => {
    await context.queryClient.query(trafficReportersQuery);
  },
  component: GatewaysPage,
});

function countIn(reporters: readonly TrafficReporter[], state: GatewayState): number {
  return reporters.filter((reporter) => gatewayState(reporter) === state).length;
}

/** The gateways worth a look, counted in the header: refused ones first, then silent ones. */
function Trouble({
  reporters,
}: {
  readonly reporters: readonly TrafficReporter[];
}): ReactElement | null {
  const refused = countIn(reporters, "refused");
  const silent = countIn(reporters, "silent");
  const parts = [
    refused === 0 ? "" : `${refused} ${refused === 1 ? "gateway" : "gateways"} refused`,
    silent === 0 ? "" : `${silent} stopped reporting`,
  ].filter((part) => part !== "");

  return parts.length === 0 ? null : (
    <>
      <span aria-hidden>·</span>
      <span className={refused === 0 ? "text-kumo-warning" : "text-kumo-danger"}>
        {parts.join(", ")}
      </span>
    </>
  );
}

function GatewaysPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { data } = useSuspenseQuery(trafficReportersQuery);
  const writable = can(me, "logs:network");

  return (
    <>
      <PageHeader
        title="Gateways"
        description="The tagged exit nodes, subnet routers and app connectors that report what passes through them."
        meta={
          <>
            <span>
              {data.asnRanges === 0
                ? "Network names not loaded yet"
                : `Network names for ${data.asnRanges.toLocaleString()} address ranges`}
            </span>
            <Trouble reporters={data.reporters} />
          </>
        }
      />
      <ResolverNotices reporters={data} />
      <Frame>
        <GatewaysTable
          reporters={data.reporters}
          writable={writable}
          canApprove={writable && can(me, "dns")}
        />
      </Frame>
      <InstallSection />
    </>
  );
}
