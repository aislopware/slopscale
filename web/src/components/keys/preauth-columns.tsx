import { Badge } from "@cloudflare/kumo/components/badge";
import type { BadgeVariant } from "@cloudflare/kumo/components/badge";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Group, PreAuthKey } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { groupName } from "~/components/access/model.ts";
import { ExpiryCell, KeyPrefix } from "~/components/keys/cells.tsx";
import { KeyActions } from "~/components/keys/key-actions.tsx";
import { usePreAuthKeyMutations } from "~/components/keys/mutations.ts";
import { preAuthKeyStatus, statusOrder } from "~/components/keys/status.ts";
import type { KeyStatus } from "~/components/keys/status.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { Avatar } from "~/components/ui/avatar.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { toast } from "~/components/ui/toast.ts";
import { userLabel } from "~/lib/node.ts";
import { parseTime } from "~/lib/time.ts";

/** Enough of the secret to tell two keys apart at a glance; the rest is behind the copy button. */
const previewLength = 24;

/** The delete dialog asks the operator to type this back, so it stays shorter than the cell. */
const nameLength = 16;

function preview(authKey: PreAuthKey): string {
  return authKey.key.slice(0, previewLength);
}

function shortName(authKey: PreAuthKey): string {
  return authKey.key.slice(0, nameLength);
}

const helper = createAppColumnHelper<PreAuthKey>();

export const preAuthKeyColumns = helper.columns([
  helper.accessor((authKey) => authKey.key, {
    id: "key",
    header: "Key",
    enableSorting: false,
    cell: ({ row }) => (
      <KeyPrefix
        text={`${preview(row.original)}…`}
        copy={row.original.key}
        label="Copy pre-auth key"
      />
    ),
    meta: { className: "min-w-64" },
  }),
  helper.accessor((authKey) => userLabel(authKey.user), {
    id: "user",
    header: "User",
    enableSorting: true,
    cell: ({ row }) => <UserCell name={userLabel(row.original.user)} />,
    meta: { className: "min-w-36" },
  }),
  // A column of its own for tags was empty on most rows, so they ride along in this cell; the
  // accessor is what the global filter searches, which keeps "search by tag" working.
  helper.accessor((authKey) => authKey.aclTags.join(" "), {
    id: "type",
    header: "Type",
    enableSorting: false,
    cell: ({ row, table }) => (
      <TypeCell authKey={row.original} groups={table.options.meta?.groups ?? []} />
    ),
    meta: { className: "min-w-36" },
  }),
  helper.accessor((authKey) => statusOrder[preAuthKeyStatus(authKey)], {
    id: "status",
    header: "Status",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <StatusCell status={preAuthKeyStatus(row.original)} />,
    meta: { className: "whitespace-nowrap" },
  }),
  helper.accessor((authKey) => parseTime(authKey.expiration)?.getTime() ?? 0, {
    id: "expiration",
    header: "Expires",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <ExpiryCell value={row.original.expiration} />,
    meta: { className: "whitespace-nowrap" },
  }),
  helper.accessor((authKey) => parseTime(authKey.createdAt)?.getTime() ?? 0, {
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
    cell: ({ row, table }) => {
      const { me } = table.options.meta ?? {};

      return me === undefined ? null : <PreAuthKeyMenu authKey={row.original} me={me} />;
    },
    meta: { className: "w-12 text-right", sticky: "right" },
  }),
]);

function UserCell({ name }: { readonly name: string }): ReactElement {
  return (
    <span className="flex min-w-0 items-center gap-2">
      <Avatar name={name} size="sm" />
      <span className="truncate text-kumo-default">{name}</span>
    </span>
  );
}

function TypeCell({
  authKey,
  groups,
}: {
  readonly authKey: PreAuthKey;
  readonly groups: readonly Group[];
}): ReactElement {
  return (
    <div className="flex flex-wrap items-center gap-1">
      <Badge variant="secondary">{authKey.reusable ? "Reusable" : "Single use"}</Badge>
      {authKey.ephemeral ? <Badge variant="secondary">Ephemeral</Badge> : null}
      {authKey.preauthorized ? null : <Badge variant="warning">Needs approval</Badge>}
      {authKey.aclTags.map((tag) => (
        <Badge key={tag} variant="outline" className="font-mono">
          {tag}
        </Badge>
      ))}
      {authKey.groupIds.map((id) => (
        <Badge key={id} variant="outline">
          {groupName(groups, id)}
        </Badge>
      ))}
    </div>
  );
}

const statusLabels: Record<KeyStatus, string> = {
  active: "Active",
  used: "Used",
  expired: "Expired",
};

const statusVariants: Record<KeyStatus, BadgeVariant> = {
  active: "success",
  used: "neutral",
  expired: "error",
};

function StatusCell({ status }: { readonly status: KeyStatus }): ReactElement {
  return (
    <Badge variant={statusVariants[status]} appearance="dot">
      {statusLabels[status]}
    </Badge>
  );
}

function PreAuthKeyMenu({
  authKey,
  me,
}: {
  readonly authKey: PreAuthKey;
  readonly me: Me;
}): ReactElement {
  const { expire, remove } = usePreAuthKeyMutations();

  return (
    <KeyActions
      label={`Actions for key ${shortName(authKey)}`}
      disabled={!can(me, "auth_keys")}
      expire={{
        title: "Expire pre-auth key?",
        description:
          "Machines already registered with it keep working, but the key cannot register any more.",
        pending: expire.isPending,
        error: expire.isError ? errorMessage(expire.error) : undefined,
        run: (done) => {
          expire.mutate(
            { body: { id: authKey.id } },
            {
              onSuccess: () => {
                toast.success("Pre-auth key expired");
                done();
              },
            },
          );
        },
      }}
      remove={{
        resourceType: "pre-auth key",
        resourceName: shortName(authKey),
        pending: remove.isPending,
        error: remove.isError ? errorMessage(remove.error) : undefined,
        run: (done) => {
          remove.mutate(
            { params: { query: { id: authKey.id } } },
            {
              onSuccess: () => {
                toast.success("Pre-auth key deleted");
                done();
              },
            },
          );
        },
      }}
    />
  );
}
