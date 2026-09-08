import { Badge } from "@cloudflare/kumo/components/badge";
import type { ReactElement } from "react";

import type { User } from "~/api/queries.ts";
import { GroupChips } from "~/components/access/group-chips.tsx";
import { groupsOfUser } from "~/components/access/model.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { Avatar } from "~/components/ui/avatar.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { UserMenu } from "~/components/users/menu.tsx";
import { roleName, roleVariant } from "~/components/users/roles.ts";
import { userLabel } from "~/lib/node.ts";

const helper = createAppColumnHelper<User>();

const providerNames: Record<string, string> = { oidc: "OpenID Connect", local: "Local" };

/** Users without an identity provider were created here; the API leaves the field empty. */
export function providerLabel(user: User): string {
  return providerNames[user.provider] ?? (user.provider === "" ? "Local" : user.provider);
}

/** Local is the default, so only an external provider earns the second line under the email. */
function externalProvider(user: User): string | null {
  return user.provider === "" || user.provider === "local" ? null : providerLabel(user);
}

export const columns = helper.columns([
  helper.accessor((user) => `${userLabel(user)} ${user.name}`, {
    id: "name",
    header: "User",
    enableSorting: true,
    cell: ({ row }) => <NameCell user={row.original} />,
    meta: { className: "w-[26%] min-w-52" },
  }),
  helper.accessor((user) => `${user.email} ${providerLabel(user)}`, {
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
      <Badge variant={roleVariant(row.original.role)}>{roleName(row.original.role)}</Badge>
    ),
    meta: { className: "whitespace-nowrap" },
  }),
  helper.accessor((user) => (user.approved ? 1 : 0), {
    id: "status",
    header: "Status",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => (
      <Badge variant={row.original.approved ? "success" : "warning"} appearance="dot">
        {row.original.approved ? "Approved" : "Needs approval"}
      </Badge>
    ),
    meta: { className: "whitespace-nowrap" },
  }),
  helper.display({
    id: "groups",
    header: "Groups",
    cell: ({ row, table }) => {
      const groups = table.options.meta?.groups;

      return groups === undefined ? null : (
        <GroupChips
          ids={groupsOfUser(groups, row.original).map((group) => group.id)}
          groups={groups}
        />
      );
    },
    meta: { className: "hidden lg:table-cell" },
  }),
  helper.accessor((user) => user.createdAt, {
    id: "joined",
    header: "Joined",
    enableSorting: true,
    enableGlobalFilter: false,
    sortDescFirst: true,
    cell: ({ row }) => (
      <span className="text-kumo-subtle">
        <RelativeTime value={row.original.createdAt} />
      </span>
    ),
    meta: { className: "hidden md:table-cell whitespace-nowrap" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const { me } = table.options.meta ?? {};

      return me === undefined ? null : <UserMenu user={row.original} me={me} />;
    },
    meta: { className: "w-12 text-right", sticky: "right" },
  }),
]);

function NameCell({ user }: { readonly user: User }): ReactElement {
  const label = userLabel(user);

  return (
    <div className="flex items-center gap-2.5">
      <Avatar name={label} size="lg" />
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="truncate font-medium text-kumo-default">{label}</span>
        {label === user.name ? null : (
          <span className="truncate text-xs text-kumo-subtle">{user.name}</span>
        )}
      </div>
    </div>
  );
}

function EmailCell({ user }: { readonly user: User }): ReactElement {
  const provider = externalProvider(user);

  if (user.email === "" && provider === null) {
    return <span className="text-kumo-subtle">—</span>;
  }

  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      {user.email === "" ? (
        <span className="text-kumo-subtle">—</span>
      ) : (
        <span className="truncate text-kumo-subtle">{user.email}</span>
      )}
      {provider === null ? null : (
        <span className="truncate text-xs text-kumo-subtle">{provider}</span>
      )}
    </div>
  );
}
