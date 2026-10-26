import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { derpLatencyQuery } from "~/api/queries.ts";
import { LatencySection } from "~/components/derp/latency-section.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/relays/latency")({
  loader: async ({ context }) => {
    await context.queryClient.query(derpLatencyQuery);
  },
  component: LatencyPage,
});

function LatencyPage(): ReactElement {
  const report = useSuspenseQuery(derpLatencyQuery).data;

  return (
    <>
      <PageHeader
        title="Latency"
        description="What the machines measure to each relay region, and which of them sit farthest from the region they home on."
      />
      <LatencySection report={report} />
    </>
  );
}
