import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { PlusIcon, ShieldCheckIcon, UserPlusIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { CreateUserDialog } from "~/components/users/dialogs.tsx";
import { useUserMutations } from "~/components/users/mutations.ts";

export interface QuickActionsProps {
  readonly me: Me;
  readonly onAddMachine: () => void;
}

/**
 * The three things an operator comes here to do: Access controls, Add user, and Add machine. Adding
 * a machine is the one the page is for, so it is the only filled button, rightmost in the group;
 * the rest are secondary. Each one is hidden rather than disabled when the caller has no scope for
 * it, so the row never offers a dead end.
 */
export function QuickActions({ me, onAddMachine }: QuickActionsProps): ReactElement {
  const [userOpen, setUserOpen] = useState(false);
  const userMutations = useUserMutations();

  return (
    <>
      {can(me, "policy_file") ? (
        <LinkButton href="/policy/rules" variant="secondary" icon={ShieldCheckIcon}>
          Access controls
        </LinkButton>
      ) : null}
      {can(me, "users") ? (
        <>
          <Button
            variant="secondary"
            icon={UserPlusIcon}
            onClick={() => {
              setUserOpen(true);
            }}
          >
            Add user
          </Button>
          <CreateUserDialog open={userOpen} onOpenChange={setUserOpen} mutations={userMutations} />
        </>
      ) : null}
      {can(me, "auth_keys") ? (
        <Button variant="primary" icon={PlusIcon} onClick={onAddMachine}>
          Add machine
        </Button>
      ) : null}
    </>
  );
}
