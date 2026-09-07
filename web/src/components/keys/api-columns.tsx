import { Badge } from "@cloudflare/kumo/components/badge";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { ApiKey, User } from "~/api/queries.ts";
import { KeyActions } from "~/components/keys/key-actions.tsx";
import { useApiKeyMutations } from "~/components/keys/mutations.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { toast } from "~/components/ui/toast.ts";
import { userLabel } from "~/lib/node.ts";
import { isPast, parseTime } from "~/lib/time.ts";

export const emptyUsers: readonly User[] = [];

const helper = createAppColumnHelper<ApiKey>();

export const apiKeyColumns = helper.columns([
  helper.accessor((apiKey) => apiKey.prefix, {
    id: "prefix",
    header: "Prefix",
    enableSorting: true,
    cell: ({ row }) => (
      <span className="rounded-sm bg-kumo-tint px-1 py-0.5 font-mono text-[0.9em] text-kumo-default">
        {row.original.prefix}
      </span>
    ),
    meta: { className: "min-w-32" },
  }),
  helper.accessor((apiKey) => apiKey.userId ?? "", {
    id: "user",
    header: "User",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row, table }) => (
      <UserCell userId={row.original.userId} users={table.options.meta?.users ?? emptyUsers} />
    ),
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
    meta: { className: "whitespace-nowrap" },
  }),
  helper.accessor((apiKey) => parseTime(apiKey.expiration)?.getTime() ?? 0, {
    id: "expiration",
    header: "Expires",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <ExpiresCell value={row.original.expiration} />,
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
        <RelativeTime value={row.original.lastSeen} />
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
    return <span className="text-kumo-inactive">—</span>;
  }

  const user = users.find((candidate) => candidate.id === userId);

  return (
    <span className="text-kumo-default">
      {user === undefined ? `User ${userId}` : userLabel(user)}
    </span>
  );
}

function ExpiresCell({ value }: { readonly value: string | null }): ReactElement {
  if (isPast(parseTime(value))) {
    return (
      <Badge variant="error" appearance="dot">
        Expired
      </Badge>
    );
  }

  return (
    <span className="text-kumo-subtle">
      <RelativeTime value={value} never="Never" />
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
        confirmLabel: "Expire key",
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
        title: "Delete API key?",
        description: `Key ${apiKey.prefix} is removed for good and cannot be restored.`,
        confirmLabel: "Delete",
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
