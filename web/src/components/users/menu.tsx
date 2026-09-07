import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import {
  CheckIcon,
  DevicesIcon,
  DotsThreeIcon,
  PencilSimpleIcon,
  ShieldCheckIcon,
  TrashIcon,
} from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import type { User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { DeleteUserDialog, RenameUserDialog, RoleDialog } from "~/components/users/dialogs.tsx";
import { useUserMutations } from "~/components/users/mutations.ts";

const actionsIconSize = 18;

type Dialog = "rename" | "role" | "delete";

type Mutations = ReturnType<typeof useUserMutations>;

export interface UserMenuProps {
  readonly user: User;
  readonly me: Me;
}

/** Every action on one user, gated by the caller's scopes. */
export function UserMenu({ user, me }: UserMenuProps): ReactElement {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const mutations = useUserMutations();
  const close = (open: boolean): void => {
    if (!open) {
      setDialog(null);
    }
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenu.Trigger
          render={
            <Button
              variant="ghost"
              shape="square"
              size="sm"
              icon={<DotsThreeIcon size={actionsIconSize} weight="bold" />}
              aria-label={`Actions for ${user.name}`}
            />
          }
        />
        <DropdownMenu.Content align="end">
          <UserMenuItems user={user} me={me} mutations={mutations} onOpen={setDialog} />
        </DropdownMenu.Content>
      </DropdownMenu>
      <RenameUserDialog
        user={user}
        open={dialog === "rename"}
        mutations={mutations}
        onOpenChange={close}
      />
      <RoleDialog user={user} open={dialog === "role"} mutations={mutations} onOpenChange={close} />
      <DeleteUserDialog
        user={user}
        open={dialog === "delete"}
        mutations={mutations}
        onOpenChange={close}
      />
    </>
  );
}

function UserMenuItems({
  user,
  me,
  mutations,
  onOpen,
}: {
  readonly user: User;
  readonly me: Me;
  readonly mutations: Mutations;
  readonly onOpen: (dialog: Dialog) => void;
}): ReactElement {
  const writable = can(me, "users");
  // The server refuses both of these on the caller's own account, so do not offer them.
  const own = me.user !== undefined && me.user.id === user.id;

  return (
    <>
      {user.approved ? null : (
        <DropdownMenu.Item
          icon={CheckIcon}
          disabled={!writable}
          onClick={() => {
            mutations.approve.mutate({
              params: { path: { id: user.id } },
              body: { approved: true },
            });
          }}
        >
          Approve
        </DropdownMenu.Item>
      )}
      <DropdownMenu.Item
        icon={PencilSimpleIcon}
        disabled={!writable}
        onClick={() => {
          onOpen("rename");
        }}
      >
        Rename…
      </DropdownMenu.Item>
      <DropdownMenu.Item
        icon={ShieldCheckIcon}
        disabled={!writable || own}
        onClick={() => {
          onOpen("role");
        }}
      >
        Change role…
      </DropdownMenu.Item>
      {can(me, "devices:core:read") ? (
        <DropdownMenu.Item
          // A rendered item drops the item's own icon and children, so the link carries both.
          render={
            <Link to="/machines" search={{ user: user.id }}>
              <DevicesIcon className="mr-2 size-4" />
              View machines
            </Link>
          }
        />
      ) : null}
      <DropdownMenu.Separator />
      <DropdownMenu.Item
        icon={TrashIcon}
        variant="danger"
        disabled={!writable || own}
        onClick={() => {
          onOpen("delete");
        }}
      >
        Delete…
      </DropdownMenu.Item>
    </>
  );
}
