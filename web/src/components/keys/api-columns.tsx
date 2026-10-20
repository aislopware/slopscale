import { cn } from "@cloudflare/kumo/utils";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { ApiKey, User } from "~/api/queries.ts";
import { RotateApiKeyDialog } from "~/components/keys/api-dialogs.tsx";
import { ExpiryCell, KeyPrefix } from "~/components/keys/cells.tsx";
import { KeyActions } from "~/components/keys/key-actions.tsx";
import { useApiKeyMutations } from "~/components/keys/mutations.ts";
import { scopeLabel } from "~/components/keys/scopes.ts";
import { apiKeyStatus } from "~/components/keys/status.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { cellSecondaryClass, narrowOnlyClass } from "~/components/table/styles.ts";
import { Avatar } from "~/components/ui/avatar.tsx";
import { ConfirmSummary } from "~/components/ui/confirm-dialog.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { toast } from "~/components/ui/toast.ts";
import { ValueList } from "~/components/ui/value-list.tsx";
import { userLabel } from "~/lib/node.ts";
import { parseTime } from "~/lib/time.ts";

export const emptyUsers: readonly User[] = [];

const helper = createAppColumnHelper<ApiKey>();

export const apiKeyColumns = helper.columns([
  helper.accessor((apiKey) => `${apiKey.prefix} ${apiKey.description}`, {
    id: "prefix",
    header: "Key",
    enableSorting: true,
    cell: ({ row, table }) => (
      <KeyCell apiKey={row.original} users={table.options.meta?.users ?? emptyUsers} />
    ),
    meta: { className: "min-w-36" },
  }),
  helper.accessor((apiKey) => apiKey.scopes.join(" "), {
    id: "scopes",
    header: "Scopes",
    enableSorting: false,
    cell: ({ row }) => <ScopesCell scopes={row.original.scopes} />,
    meta: { className: "hidden min-w-40 whitespace-nowrap md:table-cell" },
  }),
  helper.accessor((apiKey) => apiKey.userId ?? "", {
    id: "user",
    header: "User",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row, table }) => (
      <UserCell userId={row.original.userId} users={table.options.meta?.users ?? emptyUsers} />
    ),
    meta: { className: "min-w-40", hideBelow: "sm" },
  }),
  helper.accessor((apiKey) => parseTime(apiKey.expiration)?.getTime() ?? 0, {
    id: "expiration",
    header: "Expires",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <ExpiryCell value={row.original.expiration} />,
    meta: { className: "whitespace-nowrap", hideBelow: "sm" },
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
    meta: { className: "w-12 text-right", sticky: "right" },
  }),
]);

/**
 * The masked prefix with the description under it. On a phone the User and Expires columns are
 * hidden, so the key says whose it is and when it runs out on a line of its own.
 */
function KeyCell({
  apiKey,
  users,
}: {
  readonly apiKey: ApiKey;
  readonly users: readonly User[];
}): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <KeyPrefix text={apiKey.prefix} copy={apiKey.prefix} label="Copy prefix" />
      {apiKey.description === "" ? null : (
        <span className={cellSecondaryClass}>{apiKey.description}</span>
      )}
      <span className={cn("flex min-w-0 items-center gap-2 pt-0.5 text-sm", narrowOnlyClass)}>
        <span className="min-w-0 truncate text-kumo-default">
          {ownerName(apiKey.userId, users) ?? "All access"}
        </span>
        <NarrowExpiry apiKey={apiKey} />
      </span>
    </div>
  );
}

/** "Expires" before a time still to come; an expired key's badge says it alone. */
function NarrowExpiry({ apiKey }: { readonly apiKey: ApiKey }): ReactElement {
  if (apiKey.expiration === null) {
    return <span className="text-kumo-subtle">Never expires</span>;
  }

  if (apiKeyStatus(apiKey) === "expired") {
    return <ExpiryCell value={apiKey.expiration} />;
  }

  return (
    <span className="flex items-center gap-1 text-kumo-subtle">
      Expires <ExpiryCell value={apiKey.expiration} />
    </span>
  );
}

/** Whose key it is, by name; null for a key without a user, which acts with all access. */
function ownerName(userId: string | null, users: readonly User[]): string | null {
  if (userId === null || userId === "") {
    return null;
  }

  const user = users.find((candidate) => candidate.id === userId);

  return user === undefined ? `User ${userId}` : userLabel(user);
}

/** The scopes by their console names; a key without any acts with its owner's whole role. */
function ScopesCell({ scopes }: { readonly scopes: readonly string[] }): ReactElement {
  return <ValueList items={scopes} label={scopeLabel} empty="Whole role" />;
}

function UserCell({
  userId,
  users,
}: {
  readonly userId: string | null;
  readonly users: readonly User[];
}): ReactElement {
  const name = ownerName(userId, users);

  if (name === null || userId === null) {
    return <span className="text-kumo-subtle">All access</span>;
  }

  return (
    <span className="flex min-w-0 items-center gap-2">
      <Avatar name={name} id={userId} size="sm" />
      <span className="truncate text-kumo-default">{name}</span>
    </span>
  );
}

function ApiKeyMenu({ apiKey }: { readonly apiKey: ApiKey }): ReactElement {
  const { expire, remove } = useApiKeyMutations();
  const [rotating, setRotating] = useState(false);
  // The server refuses a rotation on an expired key with a 409, so the menu says so first.
  const expired = apiKeyStatus(apiKey) === "expired";

  return (
    <>
      <RotateApiKeyDialog prefix={apiKey.prefix} open={rotating} onOpenChange={setRotating} />
      <KeyActions
        label={`Actions for API key ${apiKey.prefix}`}
        rotate={{
          reason: expired ? "Expired keys cannot be rotated" : undefined,
          onSelect: () => {
            setRotating(true);
          },
        }}
        expire={{
          title: "Expire API key?",
          description: `Anything using ${apiKey.prefix} can no longer call the v1 API.`,
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
          summary: (
            <ConfirmSummary name={apiKey.prefix} mono>
              {apiKey.description === "" ? undefined : <span>{apiKey.description}</span>}
            </ConfirmSummary>
          ),
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
    </>
  );
}
