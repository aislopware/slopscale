import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { AreaLayout } from "~/components/layout/sub-nav.tsx";

/** DNS: its pages under the sub-navigation bar that links them. */
export const Route = createFileRoute("/_app/dns")({
  component: DnsLayout,
});

function DnsLayout(): ReactElement {
  return <AreaLayout area="/dns" />;
}
