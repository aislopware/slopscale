import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { ApiError } from "~/api/error.ts";
import type { Me } from "~/auth/me.ts";
import { meQuery } from "~/auth/me.ts";
import { session } from "~/auth/session.ts";
import { Shell } from "~/components/layout/shell.tsx";

export const Route = createFileRoute("/_app")({
  beforeLoad: async ({ context, location }): Promise<{ me: Me }> => {
    if (session.get() === null) {
      throw redirect({ to: "/login", search: { redirect: location.href } });
    }

    try {
      const me = await context.queryClient.query({ ...meQuery, staleTime: "static" });

      return { me };
    } catch (error) {
      if (error instanceof ApiError && error.unauthorized) {
        throw redirect({ to: "/login", search: { redirect: location.href } });
      }

      throw error;
    }
  },
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
