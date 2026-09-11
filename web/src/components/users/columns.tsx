import type { ReactElement } from "react";

import type { User } from "~/api/queries.ts";
import { GroupNames } from "~/components/access/group-names.tsx";
import { groupsOfUser } from "~/components/access/model.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { Avatar } from "~/components/ui/avatar.tsx";
import { Badge } from "~/components/ui/badge.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { UserMenu } from "~/components/users/menu.tsx";
import { RoleBadge } from "~/components/users/role-badge.tsx";
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
  helper.accessor(
    (user) => `${userLabel(user)} ${user.name} ${user.email} ${providerLabel(user)}`,
    {
      id: "name",
      header: "User",
      enableSorting: true,
      cell: ({ row }) => <NameCell user={row.original} />,
      meta: { className: "w-[30%] min-w-56" },
    },
  ),
  helper.accessor((user) => user.role, {
    id: "role",
    header: "Role",
    enableSorting: true,
    cell: ({ row }) => <RoleBadge role={row.original.role} />,
    meta: { className: "whitespace-nowrap" },
  }),
  helper.accessor((user) => (user.approved ? 1 : 0), {
    id: "status",
    header: "Status",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => (
      <Badge tone={row.original.approved ? "success" : "warning"}>
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
        <GroupNames
          ids={groupsOfUser(groups, row.original).map((group) => group.id)}
          groups={groups}
          max={2}
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

/**
 * The name, and under it the username and email on one line: the username is what the policy refers
 * to, the email is how the operator knows who that is. A separate Email column pushed the role and
 * status off the right edge at 1280px.
 */
function NameCell({ user }: { readonly user: User }): ReactElement {
  const label = userLabel(user);
  const provider = externalProvider(user);
  const parts = [label === user.name ? "" : user.name, user.email].filter((part) => part !== "");
  const title = [label, user.email, provider ?? ""].filter((part) => part !== "").join(", ");

  return (
    <div className="flex max-w-80 items-center gap-2.5" title={title}>
      <Avatar name={label} size="lg" />
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="truncate font-medium text-kumo-default">{label}</span>
        {parts.length === 0 ? null : (
          <span className="truncate text-xs text-kumo-subtle">{parts.join(" · ")}</span>
        )}
      </div>
    </div>
  );
}
