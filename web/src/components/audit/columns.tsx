import { Badge } from "@cloudflare/kumo/components/badge";
import type { BadgeVariant } from "@cloudflare/kumo/components/badge";
import type { ReactElement, ReactNode } from "react";

import type { AuditEvent } from "~/api/queries.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";

const helper = createAppColumnHelper<AuditEvent>();

/** How the API names each kind of actor, in the console's words. */
const actorKinds: Record<string, string> = {
  api_key: "api key",
  local: "local socket",
  oauth: "oauth token",
  session: "session",
  system: "system",
};

const clientError = 400;
const serverError = 500;

/** At most this many detail fields per row; the rest are counted. */
const maxDetailFields = 3;
/** Longer detail values are cut, so one long field cannot push the row open. */
const maxDetailLength = 40;

export const columns = helper.columns([
  helper.display({
    id: "time",
    header: "Time",
    cell: ({ row }) => <RelativeTime value={row.original.createdAt} />,
    meta: { className: "w-36 whitespace-nowrap" },
  }),
  helper.display({
    id: "actor",
    header: "Actor",
    cell: ({ row }) => <ActorCell event={row.original} />,
    meta: { className: "w-48" },
  }),
  helper.display({
    id: "action",
    header: "Action",
    cell: ({ row }) => <code className="font-mono text-[0.9em]">{row.original.action}</code>,
    meta: { className: "w-56" },
  }),
  helper.display({
    id: "target",
    header: "Target",
    cell: ({ row }) => <TargetCell event={row.original} />,
    meta: { className: "w-48" },
  }),
  helper.display({
    id: "result",
    header: "Result",
    cell: ({ row }) => (
      <Badge variant={outcomeVariant(row.original.outcome)}>{row.original.outcome}</Badge>
    ),
    meta: { className: "w-24" },
  }),
  helper.display({
    id: "detail",
    header: "Detail",
    cell: ({ row }) => <DetailCell detail={row.original.detail} />,
    meta: { className: "hidden lg:table-cell" },
  }),
]);

function outcomeVariant(outcome: number): BadgeVariant {
  if (outcome >= serverError) {
    return "error";
  }

  return outcome >= clientError ? "warning" : "success";
}

function ActorCell({ event }: { readonly event: AuditEvent }): ReactElement {
  const kind = actorKinds[event.actorKind] ?? event.actorKind;

  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="truncate text-kumo-default">
        {event.actorName === "" ? "unknown" : event.actorName}
      </span>
      <span className="truncate text-sm text-kumo-subtle">{kind}</span>
    </div>
  );
}

function TargetCell({ event }: { readonly event: AuditEvent }): ReactNode {
  if (event.targetKind === "" && event.targetId === "" && event.targetName === "") {
    return null;
  }

  const name = event.targetName === "" ? event.targetId : event.targetName;

  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="truncate text-kumo-default">{name}</span>
      <span className="truncate text-sm text-kumo-subtle">{event.targetKind}</span>
    </div>
  );
}

function DetailCell({ detail }: { readonly detail: Record<string, unknown> }): ReactNode {
  const fields = Object.entries(detail);

  if (fields.length === 0) {
    return null;
  }

  const shown = fields.slice(0, maxDetailFields);
  const hidden = fields.length - shown.length;

  return (
    <div className="flex flex-wrap items-center gap-1">
      {shown.map(([key, value]) => (
        <span
          key={key}
          className="rounded-sm bg-kumo-tint px-1.5 py-0.5 font-mono text-[0.9em] text-kumo-subtle"
        >
          {key}={detailValue(value)}
        </span>
      ))}
      {hidden === 0 ? null : <span className="text-sm text-kumo-subtle">+{hidden} more</span>}
    </div>
  );
}

/** A detail field as one short string; anything that is not a primitive is shown as JSON. */
function detailValue(value: unknown): string {
  const text = primitive(value) ?? JSON.stringify(value) ?? String(value);

  return text.length > maxDetailLength ? `${text.slice(0, maxDetailLength)}…` : text;
}

function primitive(value: unknown): string | undefined {
  if (typeof value === "string") {
    return value;
  }

  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }

  return undefined;
}
