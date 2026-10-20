import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { AreaLayout } from "~/components/layout/sub-nav.tsx";

/** Relays: its pages under the sub-navigation bar that links them. */
export const Route = createFileRoute("/_app/relays")({
  component: RelaysLayout,
});

function RelaysLayout(): ReactElement {
  return <AreaLayout area="/relays" />;
}
