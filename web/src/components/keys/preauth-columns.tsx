import { Badge } from "@cloudflare/kumo/components/badge";
import type { BadgeVariant } from "@cloudflare/kumo/components/badge";
import { ClipboardText } from "@cloudflare/kumo/components/clipboard-text";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { PreAuthKey } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { KeyActions } from "~/components/keys/key-actions.tsx";
import { usePreAuthKeyMutations } from "~/components/keys/mutations.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { toast } from "~/components/ui/toast.ts";
import { userLabel } from "~/lib/node.ts";
import { isPast, parseTime } from "~/lib/time.ts";

/** Enough of the secret to recognise the row; the rest is behind the copy button. */
const previewLength = 10;

type KeyStatus = "used" | "expired" | "active";

const statusOrder = { active: 0, used: 1, expired: 2 } as const;

function keyStatus(authKey: PreAuthKey): KeyStatus {
  if (authKey.used) {
    return "used";
  }

  return isPast(parseTime(authKey.expiration)) ? "expired" : "active";
}

const helper = createAppColumnHelper<PreAuthKey>();

export const preAuthKeyColumns = helper.columns([
  helper.accessor((authKey) => authKey.key, {
    id: "key",
    header: "Key",
    enableSorting: false,
    cell: ({ row }) => <KeyCell authKey={row.original} />,
    meta: { className: "min-w-52" },
  }),
  helper.accessor((authKey) => userLabel(authKey.user), {
    id: "user",
    header: "User",
    enableSorting: true,
  }),
  helper.display({
    id: "type",
    header: "Type",
    cell: ({ row }) => <TypeCell authKey={row.original} />,
    meta: { className: "min-w-40" },
  }),
  helper.accessor((authKey) => authKey.aclTags.join(" "), {
    id: "tags",
    header: "Tags",
    enableSorting: false,
    cell: ({ row }) => <TagsCell tags={row.original.aclTags} />,
    meta: { className: "hidden md:table-cell" },
  }),
  helper.accessor((authKey) => statusOrder[keyStatus(authKey)], {
    id: "status",
    header: "Status",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <StatusCell authKey={row.original} />,
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
    meta: { className: "w-12 text-right" },
  }),
]);

function KeyCell({ authKey }: { readonly authKey: PreAuthKey }): ReactElement {
  return (
    <ClipboardText
      size="sm"
      text={`${authKey.key.slice(0, previewLength)}…`}
      textToCopy={authKey.key}
      tooltip={{ text: "Copy key", copiedText: "Copied" }}
      labels={{ copyAction: "Copy key" }}
    />
  );
}

function TypeCell({ authKey }: { readonly authKey: PreAuthKey }): ReactElement {
  return (
    <div className="flex flex-wrap gap-1">
      <Badge variant="secondary">{authKey.reusable ? "Reusable" : "Single use"}</Badge>
      {authKey.ephemeral ? <Badge variant="secondary">Ephemeral</Badge> : null}
      {authKey.preauthorized ? (
        <Badge variant="success">Pre-authorized</Badge>
      ) : (
        <Badge variant="warning">Needs approval</Badge>
      )}
    </div>
  );
}

function TagsCell({ tags }: { readonly tags: readonly string[] }): ReactElement {
  if (tags.length === 0) {
    return <span className="text-kumo-inactive">—</span>;
  }

  return (
    <div className="flex flex-wrap gap-1">
      {tags.map((tag) => (
        <Badge key={tag} variant="blue" className="font-mono">
          {tag}
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

function StatusCell({ authKey }: { readonly authKey: PreAuthKey }): ReactElement {
  const status = keyStatus(authKey);

  return (
    <div className="flex flex-col items-start gap-1">
      <Badge variant={statusVariants[status]} appearance="dot">
        {statusLabels[status]}
      </Badge>
      <span className="text-sm text-kumo-subtle">
        Expires <RelativeTime value={authKey.expiration} />
      </span>
    </div>
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
  const preview = authKey.key.slice(0, previewLength);

  return (
    <KeyActions
      label={`Actions for key ${preview}`}
      disabled={!can(me, "auth_keys")}
      expire={{
        title: "Expire pre-auth key?",
        description:
          "Machines already registered with it keep working, but the key cannot register any more.",
        confirmLabel: "Expire key",
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
        title: "Delete pre-auth key?",
        description: `Key ${preview}… is removed for good. Machines registered with it keep working.`,
        confirmLabel: "Delete",
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
