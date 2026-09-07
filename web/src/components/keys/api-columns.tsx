import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { ApiKey, User } from "~/api/queries.ts";
import { ExpiryCell, KeyPrefix } from "~/components/keys/cells.tsx";
import { KeyActions } from "~/components/keys/key-actions.tsx";
import { useApiKeyMutations } from "~/components/keys/mutations.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { Avatar } from "~/components/ui/avatar.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { toast } from "~/components/ui/toast.ts";
import { userLabel } from "~/lib/node.ts";
import { parseTime } from "~/lib/time.ts";

export const emptyUsers: readonly User[] = [];

const helper = createAppColumnHelper<ApiKey>();

export const apiKeyColumns = helper.columns([
  helper.accessor((apiKey) => apiKey.prefix, {
    id: "prefix",
    header: "Prefix",
    enableSorting: true,
    cell: ({ row }) => (
      <KeyPrefix text={row.original.prefix} copy={row.original.prefix} label="Copy prefix" />
    ),
    meta: { className: "min-w-36" },
  }),
  helper.accessor((apiKey) => apiKey.userId ?? "", {
    id: "user",
    header: "User",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row, table }) => (
      <UserCell userId={row.original.userId} users={table.options.meta?.users ?? emptyUsers} />
    ),
    meta: { className: "min-w-40" },
  }),
  helper.accessor((apiKey) => parseTime(apiKey.expiration)?.getTime() ?? 0, {
    id: "expiration",
    header: "Expires",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <ExpiryCell value={row.original.expiration} />,
    meta: { className: "whitespace-nowrap" },
  }),
  helper.accessor((apiKey) => parseTime(apiKey.lastSeen)?.getTime() ?? 0, {
    id: "lastSeen",
    header: "Last used",
    enableSorting: true,
    enableGlobalFilter: false,
    sortDescFirst: true,
    cell: ({ row }) => (
      <span className="text-kumo-subtle">
        <RelativeTime value={row.original.lastSeen} never="Never used" />
      </span>
    ),
    meta: { className: "hidden md:table-cell whitespace-nowrap" },
  }),
  helper.accessor((apiKey) => parseTime(apiKey.createdAt)?.getTime() ?? 0, {
    id: "created",
    header: "Created",
    enableSorting: true,
    enableGlobalFilter: false,
    sortDescFirst: true,
    cell: ({ row }) => (
      <span className="text-kumo-subtle">
        <RelativeTime value={row.original.createdAt} />
      </span>
    ),
    meta: { className: "hidden lg:table-cell whitespace-nowrap" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row }) => <ApiKeyMenu apiKey={row.original} />,
    meta: { className: "w-12 text-right" },
  }),
]);

function UserCell({
  userId,
  users,
}: {
  readonly userId: string | null;
  readonly users: readonly User[];
}): ReactElement {
  if (userId === null || userId === "") {
    return <span className="text-kumo-subtle">All access</span>;
  }

  const user = users.find((candidate) => candidate.id === userId);
  const name = user === undefined ? `User ${userId}` : userLabel(user);

  return (
    <span className="flex min-w-0 items-center gap-2">
      <Avatar name={name} size="sm" />
      <span className="truncate text-kumo-default">{name}</span>
    </span>
  );
}

function ApiKeyMenu({ apiKey }: { readonly apiKey: ApiKey }): ReactElement {
  const { expire, remove } = useApiKeyMutations();

  return (
    <KeyActions
      label={`Actions for API key ${apiKey.prefix}`}
      expire={{
        title: "Expire API key?",
        description: `Anything still using ${apiKey.prefix} stops being able to call the API.`,
        pending: expire.isPending,
        error: expire.isError ? errorMessage(expire.error) : undefined,
        run: (done) => {
          expire.mutate(
            { body: { prefix: apiKey.prefix } },
            {
              onSuccess: () => {
                toast.success("API key expired");
                done();
              },
            },
          );
        },
      }}
      remove={{
        resourceType: "API key",
        resourceName: apiKey.prefix,
        pending: remove.isPending,
        error: remove.isError ? errorMessage(remove.error) : undefined,
        run: (done) => {
          remove.mutate(
            { params: { path: { prefix: apiKey.prefix } } },
            {
              onSuccess: () => {
                toast.success("API key deleted");
                done();
              },
            },
          );
        },
      }}
    />
  );
}
