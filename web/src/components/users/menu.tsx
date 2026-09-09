import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import {
  CheckIcon,
  DevicesIcon,
  DotsThreeIcon,
  IdentificationCardIcon,
  PencilSimpleIcon,
  ShieldCheckIcon,
  SignOutIcon,
  TrashIcon,
  UsersThreeIcon,
} from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { groupsQuery } from "~/api/queries.ts";
import type { User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { MembershipDialog } from "~/components/access/membership-dialog.tsx";
import { groupsOfUser } from "~/components/access/model.ts";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import {
  DeleteUserDialog,
  EndSessionsDialog,
  RenameUserDialog,
  RoleDialog,
} from "~/components/users/dialogs.tsx";
import { useUserMutations } from "~/components/users/mutations.ts";
import { EditProfileDialog } from "~/components/users/profile-dialog.tsx";

const actionsIconSize = 18;

type Dialog = "rename" | "profile" | "role" | "groups" | "sessions" | "delete";

type Mutations = ReturnType<typeof useUserMutations>;

export interface UserMenuProps {
  readonly user: User;
  readonly me: Me;
}

/** Every action on one user, gated by the caller's scopes. */
export function UserMenu({ user, me }: UserMenuProps): ReactElement {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const mutations = useUserMutations();
  const access = useAccessMutations();
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file") });
  const groupList = groups.data?.groups ?? [];
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
          <UserMenuItems
            user={user}
            me={me}
            mutations={mutations}
            canEditGroups={can(me, "policy_file") && groups.data !== undefined}
            onOpen={setDialog}
          />
        </DropdownMenu.Content>
      </DropdownMenu>
      <RenameUserDialog
        user={user}
        open={dialog === "rename"}
        mutations={mutations}
        onOpenChange={close}
      />
      <EditProfileDialog
        user={user}
        open={dialog === "profile"}
        mutations={mutations}
        onOpenChange={close}
      />
      <RoleDialog user={user} open={dialog === "role"} mutations={mutations} onOpenChange={close} />
      <MembershipDialog
        title="Edit groups"
        description="Every machine this user owns is in these groups, including ones registered later."
        member={{ userId: user.id }}
        groups={groupList}
        current={groupsOfUser(groupList, user).map((group) => group.id)}
        open={dialog === "groups"}
        onOpenChange={close}
        mutations={access}
      />
      <EndSessionsDialog
        user={user}
        open={dialog === "sessions"}
        mutations={mutations}
        onOpenChange={close}
        me={me}
      />
      <DeleteUserDialog
        user={user}
        open={dialog === "delete"}
        mutations={mutations}
        onOpenChange={close}
      />
    </>
  );
}

export const cannotChangeUsers = "Your credentials cannot change users";

/** Why an item is off limits, in the order the server refuses it. */
function itemReason(writable: boolean, own: boolean, ownReason: string): string | undefined {
  if (!writable) {
    return cannotChangeUsers;
  }

  return own ? ownReason : undefined;
}

function UserMenuItems({
  user,
  me,
  mutations,
  canEditGroups,
  onOpen,
}: {
  readonly user: User;
  readonly me: Me;
  readonly mutations: Mutations;
  readonly canEditGroups: boolean;
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
        icon={IdentificationCardIcon}
        disabled={!writable}
        onClick={() => {
          onOpen("profile");
        }}
      >
        Edit profile…
      </DropdownMenu.Item>
      <DisabledReason reason={itemReason(writable, own, "Only someone else can change your role")}>
        <DropdownMenu.Item
          icon={ShieldCheckIcon}
          disabled={!writable || own}
          onClick={() => {
            onOpen("role");
          }}
        >
          Change role…
        </DropdownMenu.Item>
      </DisabledReason>
      {canEditGroups ? (
        <DropdownMenu.Item
          icon={UsersThreeIcon}
          onClick={() => {
            onOpen("groups");
          }}
        >
          Edit groups…
        </DropdownMenu.Item>
      ) : null}
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
      {/* Ending your own sessions is allowed: it is how you drop a browser you left signed in. */}
      <DisabledReason reason={writable ? undefined : cannotChangeUsers}>
        <DropdownMenu.Item
          icon={SignOutIcon}
          disabled={!writable}
          onClick={() => {
            onOpen("sessions");
          }}
        >
          Sign out everywhere…
        </DropdownMenu.Item>
      </DisabledReason>
      <DisabledReason reason={itemReason(writable, own, "You cannot delete your own account")}>
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
      </DisabledReason>
    </>
  );
}
