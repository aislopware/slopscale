import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { trafficReportersQuery } from "~/api/traffic.ts";
import { can } from "~/auth/me.ts";
import { GatewaysTable, gatewayState } from "~/components/traffic/gateways-table.tsx";
import { InstallSection } from "~/components/traffic/install-section.tsx";
import { Frame } from "~/components/ui/frame.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/traffic/gateways")({
  loader: async ({ context }) => {
    await context.queryClient.query(trafficReportersQuery);
  },
  component: GatewaysPage,
});

function GatewaysPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { data } = useSuspenseQuery(trafficReportersQuery);
  const silent = data.reporters.filter((reporter) => gatewayState(reporter) === "silent").length;

  return (
    <>
      <PageHeader
        title="Gateways"
        description="The exit nodes, subnet routers and app connectors that report what passes through them."
        meta={
          <>
            <span>
              {data.asnRanges === 0
                ? "Network names not loaded yet"
                : `Network names for ${data.asnRanges.toLocaleString()} address ranges`}
            </span>
            {silent === 0 ? null : (
              <>
                <span aria-hidden>·</span>
                <span className="text-kumo-warning">
                  {silent === 1
                    ? "1 gateway stopped reporting"
                    : `${silent} gateways stopped reporting`}
                </span>
              </>
            )}
          </>
        }
      />
      <Frame>
        <GatewaysTable reporters={data.reporters} writable={can(me, "logs:network")} />
      </Frame>
      <InstallSection />
    </>
  );
}
