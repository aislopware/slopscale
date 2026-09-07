import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { FileTextIcon, PlusIcon, UserPlusIcon } from "@phosphor-icons/react";
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
 * The three things an operator comes here to do. Each one is hidden rather than disabled when the
 * caller has no scope for it, so the row never offers a dead end.
 */
export function QuickActions({ me, onAddMachine }: QuickActionsProps): ReactElement {
  const [userOpen, setUserOpen] = useState(false);
  const userMutations = useUserMutations();

  return (
    <>
      {can(me, "auth_keys") ? (
        <Button variant="secondary" size="sm" icon={PlusIcon} onClick={onAddMachine}>
          Add machine
        </Button>
      ) : null}
      {can(me, "users") ? (
        <>
          <Button
            variant="secondary"
            size="sm"
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
      {can(me, "policy_file") ? (
        <LinkButton href="/policy" variant="secondary" size="sm" icon={FileTextIcon}>
          Edit policy
        </LinkButton>
      ) : null}
    </>
  );
}
