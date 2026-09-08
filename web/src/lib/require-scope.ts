import { redirect } from "@tanstack/react-router";

import type { Me, Scope } from "~/auth/me.ts";
import { can } from "~/auth/me.ts";

/**
 * A `beforeLoad` for a page that is nothing without one scope: a caller without it is sent to the
 * overview instead of a page whose every query fails. The sidebar hides the page as well; this
 * covers the URL typed or followed by hand.
 */
export function requireScope(scope: Scope): (opts: { context: { me: Me } }) => void {
  return ({ context }) => {
    if (!can(context.me, scope)) {
      // oxlint-disable-next-line typescript/only-throw-error -- a redirect is what beforeLoad throws
      throw redirect({ to: "/" });
    }
  };
}
