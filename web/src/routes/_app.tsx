import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { ApiError } from "~/api/error.ts";
import type { Me } from "~/auth/me.ts";
import { meQuery } from "~/auth/me.ts";
import { Shell } from "~/components/layout/shell.tsx";

export const Route = createFileRoute("/_app")({
  // No local check first: a sign-in through the identity provider leaves only an HttpOnly cookie,
  // so whether the operator is signed in is the server's answer.
  beforeLoad: async ({ context, location }): Promise<{ me: Me }> => {
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
