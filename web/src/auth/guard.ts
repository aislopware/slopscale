import type { QueryClient } from "@tanstack/react-query";
import { redirect } from "@tanstack/react-router";

import { ApiError } from "~/api/error.ts";
import { meQuery } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { redirectFor } from "~/components/layout/nav.ts";

/**
 * The sign-in guard of every layout with pages behind it, the shell's and the focused flows': the
 * caller, or a trip to the sign-in page that comes back here. No local check comes first: a sign-in
 * through the identity provider leaves only an HttpOnly cookie, so whether the operator is signed
 * in is the server's answer. Every page is also behind the rule that decides the sidebar, so an
 * address reached by hand cannot open a page the sidebar does not offer.
 */
export async function requireSignIn({
  context,
  location,
}: {
  readonly context: { readonly queryClient: QueryClient };
  readonly location: { readonly href: string; readonly pathname: string };
}): Promise<{ me: Me }> {
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
}
