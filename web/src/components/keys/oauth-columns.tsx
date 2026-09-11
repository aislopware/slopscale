import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { OAuthClient, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { emptyUsers } from "~/components/keys/api-columns.tsx";
import { issuerHost } from "~/components/keys/federated.ts";
import { KeyActions } from "~/components/keys/key-actions.tsx";
import { useOAuthClientMutations } from "~/components/keys/mutations.ts";
import { OAuthClientDetailsDialog } from "~/components/keys/oauth-details-dialog.tsx";
import { EditOAuthClientDialog } from "~/components/keys/oauth-dialogs.tsx";
import { scopeLabel } from "~/components/keys/scopes.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { Avatar } from "~/components/ui/avatar.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { TagList } from "~/components/ui/tag.tsx";
import { toast } from "~/components/ui/toast.ts";
import { ValueList } from "~/components/ui/value-list.tsx";
import { userLabel } from "~/lib/node.ts";
import { parseTime } from "~/lib/time.ts";

const helper = createAppColumnHelper<OAuthClient>();

export const oauthClientColumns = helper.columns([
  helper.accessor(
    (client) => `${client.clientId} ${client.description} ${client.subject} ${client.issuer}`,
    {
      id: "client",
      header: "Client",
      enableSorting: true,
      cell: ({ row }) => <ClientCell client={row.original} />,
      meta: { className: "min-w-56 align-top" },
    },
  ),
  helper.accessor((client) => (client.keyType === "federated" ? "Federated identity" : "Client"), {
    id: "kind",
    header: "Kind",
    enableSorting: true,
    cell: ({ getValue }) => <span className="text-kumo-default">{getValue()}</span>,
    meta: { className: "hidden min-w-32 align-top whitespace-nowrap sm:table-cell" },
  }),
  // Scopes are the column that gives way first: they wrap onto as many lines as they need, so the
  // tags beside them keep the width one of them asks for.
  helper.accessor((client) => client.scopes.join(" "), {
    id: "scopes",
    header: "Scopes",
    enableSorting: false,
    cell: ({ row }) => <ValueList items={row.original.scopes} label={scopeLabel} />,
    meta: { className: "min-w-32 align-top" },
  }),
  helper.accessor((client) => client.tags.join(" "), {
    id: "tags",
    header: "Tags",
    enableSorting: false,
    cell: ({ row }) => <TagList tags={row.original.tags} size="sm" />,
    meta: { className: "hidden min-w-28 align-top md:table-cell" },
  }),
  helper.accessor((client) => client.userId ?? "", {
    id: "user",
    header: "Created by",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row, table }) => (
      <CreatorCell userId={row.original.userId} users={table.options.meta?.users ?? emptyUsers} />
    ),
    meta: { className: "hidden min-w-28 align-top whitespace-nowrap 2xl:table-cell" },
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
    meta: { className: "hidden align-top whitespace-nowrap 2xl:table-cell" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => (
      <ClientMenu
        client={row.original}
        disabled={table.options.meta?.me === undefined || !can(table.options.meta.me, "oauth_keys")}
        me={table.options.meta?.me}
      />
    ),
    meta: { className: "w-12 text-right", sticky: "right" },
  }),
]);

/**
 * What the row says under the client id. The subject and the issuer host each take a line of their
 * own and truncate on their own: on one line the host took the room the subject needed, which left
 * a federated subject as "repo:acm…" even on a wide screen. The whole value is in the details
 * dialog, and in the title of each line for a pointer that hovers it.
 */
function ClientSubtext({ client }: { readonly client: OAuthClient }): ReactElement | null {
  if (client.keyType === "federated") {
    const host = issuerHost(client.issuer);
    return (
      <div className="flex min-w-0 flex-col text-sm">
        <span className="truncate text-kumo-default" title={client.subject}>
          {client.subject}
        </span>
        {host === "" ? null : (
          <span className="truncate text-kumo-subtle" title={client.issuer}>
            {host}
          </span>
        )}
      </div>
    );
  }
  if (client.description === "") {
    return null;
  }
  return (
    <span className="truncate text-sm text-kumo-subtle" title={client.description}>
      {client.description}
    </span>
  );
}

/**
 * The client id with subject/issuer host for federated rows or description under it. The cell is
 * capped rather than left to grow: a federated subject is a long line, and a column that widens to
 * fit one pushes the tags beside it under the pinned actions. The id opens the details, where the
 * values are whole and copyable, so a truncated line is never the end of the story.
 */
function ClientCell({ client }: { readonly client: OAuthClient }): ReactElement {
  const [showing, setShowing] = useState(false);
  const isFederated = client.keyType === "federated";

  return (
    <div className="flex max-w-72 min-w-0 flex-col gap-0.5">
      <button
        type="button"
        title={`Details for ${client.clientId}`}
        className="-mx-1 max-w-full truncate rounded-sm px-1 text-left font-mono text-[0.9em] text-kumo-default hover:bg-kumo-tint hover:underline focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none"
        onClick={(event) => {
          // The row itself is not a link, but the actions column is; keep the click here.
          event.stopPropagation();
          setShowing(true);
        }}
      >
        {client.clientId}
      </button>
      <ClientSubtext client={client} />
      {isFederated && client.description !== "" ? (
        <span className="truncate text-sm text-kumo-subtle" title={client.description}>
          {client.description}
        </span>
      ) : null}
      <OAuthClientDetailsDialog client={client} open={showing} onOpenChange={setShowing} />
    </div>
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

function ClientMenu({
  client,
  disabled,
  me,
}: {
  readonly client: OAuthClient;
  readonly disabled: boolean;
  readonly me?: Me | undefined;
}): ReactElement {
  const [editing, setEditing] = useState(false);
  const [showing, setShowing] = useState(false);
  const { revoke } = useOAuthClientMutations();
  const isFederated = client.keyType === "federated";
  const resourceType = isFederated ? "identity" : "OAuth client";

  return (
    <>
      <KeyActions
        label={`Actions for ${resourceType} ${client.clientId}`}
        disabled={disabled}
        details={{
          onSelect: () => {
            setShowing(true);
          },
        }}
        edit={{
          onSelect: () => {
            setEditing(true);
          },
        }}
        remove={{
          resourceType,
          resourceName: client.clientId,
          buttonText: isFederated ? "Delete identity" : undefined,
          menuItemText: isFederated ? "Delete identity…" : undefined,
          pending: revoke.isPending,
          error: revoke.isError ? errorMessage(revoke.error) : undefined,
          run: (done) => {
            revoke.mutate(
              { params: { path: { clientId: client.clientId } } },
              {
                onSuccess: () => {
                  toast.success(
                    isFederated ? "Federated identity deleted" : "OAuth client revoked",
                  );
                  done();
                },
              },
            );
          },
        }}
      />
      <EditOAuthClientDialog client={client} me={me} open={editing} onOpenChange={setEditing} />
      <OAuthClientDetailsDialog client={client} open={showing} onOpenChange={setShowing} />
    </>
  );
}
