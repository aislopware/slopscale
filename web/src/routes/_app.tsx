import { createFileRoute, Outlet } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { requireSignIn } from "~/auth/guard.ts";
import { Shell } from "~/components/layout/shell.tsx";

export const Route = createFileRoute("/_app")({
  beforeLoad: requireSignIn,
  component: AppLayout,
});

function AppLayout(): ReactElement {
  const { me } = Route.useRouteContext();

  return (
    <Shell me={me}>
      <Outlet />
    </Shell>
  );
}
