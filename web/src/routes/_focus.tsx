import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";

import { ApiError } from "~/api/error.ts";
import type { Me } from "~/auth/me.ts";
import { meQuery } from "~/auth/me.ts";
import { redirectFor } from "~/components/layout/nav.ts";

/**
 * Pages that take the whole screen, without the sidebar and the top bar: the Mesh "Add node"
 * wizard's frame, for a flow the operator should finish or leave rather than wander out of. The
 * guard is the app layout's, so a page here is behind the same sign-in and the same sidebar rule as
 * the page it was opened from; each page draws its own focused chrome.
 */
export const Route = createFileRoute("/_focus")({
  beforeLoad: async ({ context, location }): Promise<{ me: Me }> => {
    let me: Me;

    try {
      me = await context.queryClient.query({ ...meQuery, staleTime: "static" });
    } catch (error) {
      if (error instanceof ApiError && error.unauthorized) {
        throw redirect({ to: "/login", search: { redirect: location.href } });
      }

      throw error;
    }

    const elsewhere = redirectFor(me, location.pathname);

    if (elsewhere !== null) {
      throw redirect({ to: elsewhere, replace: true });
    }

    return { me };
  },
  component: Outlet,
});
