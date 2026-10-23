import { LinkButton } from "@cloudflare/kumo/components/button";
import { PlusIcon, UserPlusIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";

export interface QuickActionsProps {
  readonly me: Me;
}

/**
 * The two things an operator comes here to add: a user and a machine. Adding a machine is the one
 * the page is for, so it is the only filled button, rightmost in the group; the other is secondary,
 * as the dashboard pairs its header actions. Each one is hidden rather than disabled when the
 * caller has no scope for it, so the row never offers a dead end.
 */
export function QuickActions({ me }: QuickActionsProps): ReactElement {
  return (
    <>
      {can(me, "users") ? (
        <LinkButton href="/users/new" variant="secondary" icon={UserPlusIcon}>
          Add user
        </LinkButton>
      ) : null}
      {can(me, "auth_keys") ? (
        <LinkButton href="/machines/add" variant="primary" icon={PlusIcon}>
          Add machine
        </LinkButton>
      ) : null}
    </>
  );
}
