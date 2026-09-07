import { Badge } from "@cloudflare/kumo/components/badge";
import type { ReactElement } from "react";

import type { User } from "~/api/queries.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { UserMenu } from "~/components/users/menu.tsx";
import { userLabel } from "~/lib/node.ts";

const helper = createAppColumnHelper<User>();

/** Users without an identity provider were created here; the API leaves the field empty. */
function providerLabel(user: User): string {
  return user.provider === "" ? "local" : user.provider;
}

function initial(user: User): string {
  return (userLabel(user).trim()[0] ?? "?").toUpperCase();
}

export const columns = helper.columns([
  helper.accessor((user) => `${userLabel(user)} ${user.name}`, {
    id: "name",
    header: "Name",
    enableSorting: true,
    cell: ({ row }) => <NameCell user={row.original} />,
    meta: { className: "w-[30%] min-w-56" },
  }),
  helper.accessor((user) => user.email, {
    id: "email",
    header: "Email",
    enableSorting: true,
    cell: ({ row }) => <EmailCell user={row.original} />,
  }),
  helper.accessor((user) => user.role, {
    id: "role",
    header: "Role",
    enableSorting: true,
    cell: ({ row }) => (
      <Badge variant={row.original.role === "owner" ? "info" : "secondary"}>
        {row.original.role}
      </Badge>
    ),
  }),
  helper.accessor((user) => (user.approved ? 1 : 0), {
    id: "status",
    header: "Status",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <StatusCell user={row.original} />,
  }),
  helper.accessor((user) => providerLabel(user), {
    id: "provider",
    header: "Provider",
    enableSorting: true,
    cell: ({ row }) => <span className="text-kumo-subtle">{providerLabel(row.original)}</span>,
    meta: { className: "hidden md:table-cell" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const { me } = table.options.meta ?? {};

      return me === undefined ? null : <UserMenu user={row.original} me={me} />;
    },
    meta: { className: "w-12 text-right" },
  }),
]);

function NameCell({ user }: { readonly user: User }): ReactElement {
  return (
    <div className="flex items-center gap-3">
      <span
        aria-hidden
        className="flex size-7 shrink-0 items-center justify-center rounded-full bg-kumo-tint text-sm font-medium text-kumo-default"
      >
        {initial(user)}
      </span>
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="truncate font-medium text-kumo-default">{userLabel(user)}</span>
        <span className="truncate text-sm text-kumo-subtle">{user.name}</span>
      </div>
    </div>
  );
}

function EmailCell({ user }: { readonly user: User }): ReactElement {
  if (user.email === "") {
    return <span className="text-kumo-inactive">No email</span>;
  }

  return <span className="text-kumo-subtle">{user.email}</span>;
}

function StatusCell({ user }: { readonly user: User }): ReactElement {
  return (
    <div className="flex flex-col items-start gap-1">
      <Badge variant={user.approved ? "success" : "warning"} appearance="dot">
        {user.approved ? "Active" : "Needs approval"}
      </Badge>
      <span className="text-sm text-kumo-subtle">
        Joined <RelativeTime value={user.createdAt} />
      </span>
    </div>
  );
}
