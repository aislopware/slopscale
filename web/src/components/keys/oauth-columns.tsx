import { Badge } from "@cloudflare/kumo/components/badge";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { OAuthClient, User } from "~/api/queries.ts";
import { emptyUsers } from "~/components/keys/api-columns.tsx";
import { KeyPrefix } from "~/components/keys/cells.tsx";
import { KeyActions } from "~/components/keys/key-actions.tsx";
import { useOAuthClientMutations } from "~/components/keys/mutations.ts";
import { scopeLabel } from "~/components/keys/scopes.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { Avatar } from "~/components/ui/avatar.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { toast } from "~/components/ui/toast.ts";
import { userLabel } from "~/lib/node.ts";
import { parseTime } from "~/lib/time.ts";

const helper = createAppColumnHelper<OAuthClient>();

export const oauthClientColumns = helper.columns([
  helper.accessor((client) => `${client.clientId} ${client.description}`, {
    id: "client",
    header: "Client",
    enableSorting: true,
    cell: ({ row }) => <ClientCell client={row.original} />,
    meta: { className: "min-w-36" },
  }),
  helper.accessor((client) => client.scopes.join(" "), {
    id: "scopes",
    header: "Scopes",
    enableSorting: false,
    cell: ({ row }) => <Chips values={row.original.scopes} />,
    meta: { className: "hidden min-w-40 md:table-cell" },
  }),
  helper.accessor((client) => client.tags.join(" "), {
    id: "tags",
    header: "Tags",
    enableSorting: false,
    cell: ({ row }) =>
      row.original.tags.length === 0 ? (
        <span className="text-kumo-subtle">None</span>
      ) : (
        <Chips values={row.original.tags} mono />
      ),
    meta: { className: "hidden lg:table-cell" },
  }),
  helper.accessor((client) => client.userId ?? "", {
    id: "user",
    header: "Created by",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row, table }) => (
      <CreatorCell userId={row.original.userId} users={table.options.meta?.users ?? emptyUsers} />
    ),
    meta: { className: "min-w-40" },
  }),
  helper.accessor((client) => parseTime(client.createdAt)?.getTime() ?? 0, {
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
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row }) => <ClientMenu client={row.original} />,
    meta: { className: "w-12 text-right" },
  }),
]);

/** The client id with the description under it. The secret is never shown again. */
function ClientCell({ client }: { readonly client: OAuthClient }): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <KeyPrefix text={client.clientId} copy={client.clientId} label="Copy client id" />
      {client.description === "" ? null : (
        <span className="truncate text-xs text-kumo-subtle">{client.description}</span>
      )}
    </div>
  );
}

/** Scopes by their console names; tags as the policy spells them. */
function Chips({
  values,
  mono = false,
}: {
  readonly values: readonly string[];
  readonly mono?: boolean;
}): ReactElement {
  return (
    <span className="flex flex-wrap gap-1">
      {values.map((value) =>
        mono ? (
          <Badge key={value} variant="outline" className="font-mono">
            {value}
          </Badge>
        ) : (
          <Badge key={value} variant="secondary">
            {scopeLabel(value)}
          </Badge>
        ),
      )}
    </span>
  );
}

function CreatorCell({
  userId,
  users,
}: {
  readonly userId: string | null;
  readonly users: readonly User[];
}): ReactElement {
  if (userId === null || userId === "") {
    return <span className="text-kumo-subtle">CLI</span>;
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

function ClientMenu({ client }: { readonly client: OAuthClient }): ReactElement {
  const { revoke } = useOAuthClientMutations();

  return (
    <KeyActions
      label={`Actions for OAuth client ${client.clientId}`}
      remove={{
        resourceType: "OAuth client",
        resourceName: client.clientId,
        pending: revoke.isPending,
        error: revoke.isError ? errorMessage(revoke.error) : undefined,
        run: (done) => {
          revoke.mutate(
            { params: { path: { clientId: client.clientId } } },
            {
              onSuccess: () => {
                toast.success("OAuth client revoked");
                done();
              },
            },
          );
        },
      }}
    />
  );
}
