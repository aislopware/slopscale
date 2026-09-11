import type { ReactElement } from "react";

import type { Invite, User } from "~/api/queries.ts";
import { GroupNames } from "~/components/access/group-names.tsx";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { Badge } from "~/components/ui/badge.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import type { Tone } from "~/components/ui/status.tsx";
import { InviteMenu } from "~/components/users/invite-menu.tsx";
import { inviteState } from "~/components/users/invites.ts";
import type { InviteState } from "~/components/users/invites.ts";
import { RoleBadge } from "~/components/users/role-badge.tsx";
import { userLabel } from "~/lib/node.ts";

const helper = createAppColumnHelper<Invite>();

const stateLabels: Record<InviteState, string> = {
  pending: "Waiting",
  expired: "Expired",
  accepted: "Accepted",
};

const stateTones: Record<InviteState, Tone> = {
  pending: "info",
  expired: "warning",
  accepted: "success",
};

/** Who sent the invitation; the id is all the API carries, so an unknown sender stays a dash. */
function senderLabel(users: readonly User[] | undefined, id: string): string | null {
  if (id === "" || id === "0") {
    return null;
  }

  const user = users?.find((candidate) => candidate.id === id);

  return user === undefined ? `#${id}` : userLabel(user);
}

export const inviteColumns = helper.columns([
  helper.accessor((invite) => invite.email, {
    id: "email",
    header: "Email",
    enableSorting: true,
    // An address has no spaces to break at, and `truncate` would set the column to the width of the
    // longest one, pushing the last columns out of the panel. It wraps inside the word instead.
    cell: ({ row }) => (
      <span className="font-medium wrap-anywhere text-kumo-default">{row.original.email}</span>
    ),
    meta: { className: "w-[28%] min-w-52 align-top" },
  }),
  helper.accessor((invite) => invite.role, {
    id: "role",
    header: "Role",
    enableSorting: true,
    cell: ({ row }) => <RoleBadge role={row.original.role} />,
    meta: { className: "whitespace-nowrap" },
  }),
  helper.display({
    id: "groups",
    header: "Groups",
    cell: ({ row, table }) => {
      const groups = table.options.meta?.groups;

      return groups === undefined ? null : (
        <GroupNames ids={row.original.groupIds} groups={groups} max={2} />
      );
    },
    meta: { className: "hidden lg:table-cell" },
  }),
  helper.accessor((invite) => invite.expiresAt, {
    id: "expires",
    header: "Expires",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => (
      <span className="text-kumo-subtle">
        <RelativeTime value={row.original.expiresAt} />
      </span>
    ),
    meta: { className: "hidden whitespace-nowrap md:table-cell" },
  }),
  helper.display({
    id: "invitedBy",
    header: "Invited by",
    cell: ({ row, table }) => (
      <SenderCell invite={row.original} users={table.options.meta?.users} />
    ),
    meta: { className: "hidden whitespace-nowrap lg:table-cell" },
  }),
  helper.accessor((invite) => inviteState(invite), {
    id: "status",
    header: "Status",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => {
      const state = inviteState(row.original);

      return <Badge tone={stateTones[state]}>{stateLabels[state]}</Badge>;
    },
    meta: { className: "whitespace-nowrap" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const { me } = table.options.meta ?? {};

      return me === undefined ? null : <InviteMenu invite={row.original} me={me} />;
    },
    meta: { className: "w-12 text-right", sticky: "right" },
  }),
]);

function SenderCell({
  invite,
  users,
}: {
  readonly invite: Invite;
  readonly users: readonly User[] | undefined;
}): ReactElement {
  const label = senderLabel(users, invite.createdBy ?? "");

  return <span className="text-kumo-subtle">{label ?? "—"}</span>;
}
