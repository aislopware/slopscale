import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { trafficReportersQuery } from "~/api/traffic.ts";
import type { TrafficReporter } from "~/api/traffic.ts";
import { can } from "~/auth/me.ts";
import { plural } from "~/components/overview/plural.ts";
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

/**
 * The gateways in a line for the header, worth-a-look ones in their tone: refused ones first, then
 * those not reporting, then those reporting with a collector down.
 */
function Tally({ reporters }: { readonly reporters: readonly TrafficReporter[] }): ReactElement {
  const refused = countIn(reporters, "refused");
  const silent = countIn(reporters, "silent");
  const degraded = countIn(reporters, "degraded");
  const trouble = [
    refused === 0 ? "" : `${refused} refused`,
    silent === 0 ? "" : `${silent} not reporting`,
    degraded === 0 ? "" : `${degraded} degraded`,
  ].filter((part) => part !== "");

  return (
    <>
      <span>{plural(reporters.length, "gateway")}</span>
      {trouble.length === 0 ? null : (
        <>
          <span aria-hidden>·</span>
          <span className={refused === 0 ? "text-kumo-warning" : "text-kumo-danger"}>
            {trouble.join(", ")}
          </span>
        </>
      )}
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
        {...(data.reporters.length === 0 ? {} : { meta: <Tally reporters={data.reporters} /> })}
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
