import type { ReactElement, ReactNode } from "react";

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
import { Badge } from "~/components/ui/badge.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import type { Tone } from "~/components/ui/status.tsx";
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
    cell: ({ row }) => <KeyCell authKey={row.original} />,
    meta: { className: "min-w-56" },
  }),
  helper.accessor((authKey) => userLabel(authKey.user), {
    id: "user",
    header: "User",
    enableSorting: true,
    cell: ({ row }) => <UserCell name={userLabel(row.original.user)} />,
    meta: { className: "hidden min-w-36 sm:table-cell" },
  }),
  // A column of its own for tags was empty on most rows, so they ride along in this cell; the
  // accessor is what the global filter searches, which keeps "search by tag" working.
  helper.accessor((authKey) => authKey.aclTags.join(" "), {
    id: "type",
    header: "Options",
    enableSorting: false,
    cell: ({ row, table }) => (
      <TypeCell authKey={row.original} groups={table.options.meta?.groups ?? []} />
    ),
    meta: { className: "hidden min-w-36 sm:table-cell" },
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
    cell: ({ row }) => <ExpiresCell authKey={row.original} />,
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
    meta: { className: "hidden 2xl:table-cell whitespace-nowrap" },
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

/** On a phone the User and Options columns are hidden, so the key carries their words below it. */
function KeyCell({ authKey }: { readonly authKey: PreAuthKey }): ReactElement {
  const words = [userLabel(authKey.user), ...traits(authKey), ...authKey.aclTags];

  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <KeyPrefix text={`${preview(authKey)}…`} copy={authKey.key} label="Copy pre-auth key" />
      <span className="truncate text-xs text-kumo-subtle sm:hidden" title={words.join(" · ")}>
        {words.join(" · ")}
      </span>
    </div>
  );
}

function UserCell({ name }: { readonly name: string }): ReactElement {
  return (
    <span className="flex max-w-64 min-w-0 items-center gap-2" title={name}>
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
    <div className="flex min-w-0 flex-col items-start gap-1">
      <span className="text-kumo-default">{traits(authKey).join(" · ")}</span>
      {authKey.aclTags.length === 0 ? null : (
        <Labelled label="Tags">
          <span className="font-mono text-[0.9em]">{authKey.aclTags.join(", ")}</span>
        </Labelled>
      )}
      {authKey.groupIds.length === 0 ? null : (
        <Labelled label="Groups">
          {authKey.groupIds.map((id) => groupName(groups, id)).join(", ")}
        </Labelled>
      )}
    </div>
  );
}

/** One line of the Options cell: what the values are, then the values themselves. */
function Labelled({
  label,
  children,
}: {
  readonly label: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <span className="min-w-0 text-sm [overflow-wrap:anywhere] text-kumo-default">
      <span className="text-kumo-subtle">{label} </span>
      {children}
    </span>
  );
}

/** The Status column already says Expired, so this one says when, in muted text. */
function ExpiresCell({ authKey }: { readonly authKey: PreAuthKey }): ReactElement {
  if (preAuthKeyStatus(authKey) !== "expired") {
    return <ExpiryCell value={authKey.expiration} />;
  }

  return (
    <span className="text-kumo-subtle">
      <RelativeTime value={authKey.expiration} />
    </span>
  );
}

/** What the key does to a machine that registers with it, as words: "Reusable · Ephemeral". */
function traits(authKey: PreAuthKey): string[] {
  const words = [authKey.reusable ? "Reusable" : "Single use"];

  if (authKey.ephemeral) {
    words.push("Ephemeral");
  }

  if (!authKey.preauthorized) {
    words.push("Needs approval");
  }

  return words;
}

const statusLabels: Record<KeyStatus, string> = {
  active: "Active",
  used: "Used",
  expired: "Expired",
};

const statusTones: Record<KeyStatus, Tone> = {
  active: "success",
  used: "neutral",
  expired: "danger",
};

function StatusCell({ status }: { readonly status: KeyStatus }): ReactElement {
  return <Badge tone={statusTones[status]}>{statusLabels[status]}</Badge>;
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
          "Machines already registered keep working, but the key cannot register new ones.",
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
