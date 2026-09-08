import { Badge } from "@cloudflare/kumo/components/badge";
import type { ReactElement } from "react";

import { RequestMenu } from "~/components/access/request-menu.tsx";
import { phaseLabels } from "~/components/access/request-model.ts";
import type { RequestPhase, RequestRow } from "~/components/access/request-model.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { formatDuration } from "~/lib/time.ts";

const helper = createAppColumnHelper<RequestRow>();

const phaseVariants: Record<RequestPhase, "warning" | "success" | "neutral" | "error"> = {
  pending: "warning",
  active: "success",
  expired: "neutral",
  denied: "error",
  cancelled: "neutral",
};

export const requestColumns = helper.columns([
  helper.accessor((request) => `${request.userName} ${request.nodeLabel}`, {
    id: "who",
    header: "Requester",
    enableSorting: true,
    cell: ({ row }) => (
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="truncate font-medium text-kumo-default">{row.original.userName}</span>
        <span className="truncate text-xs text-kumo-subtle">{row.original.nodeLabel}</span>
      </div>
    ),
    meta: { className: "w-[22%] min-w-40" },
  }),
  helper.accessor((request) => `${request.groupLabel} ${request.reason}`, {
    id: "group",
    header: "Group",
    enableSorting: true,
    cell: ({ row }) => (
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="truncate text-kumo-default">{row.original.groupLabel}</span>
        {row.original.reason === "" ? null : (
          <span className="truncate text-xs text-kumo-subtle" title={row.original.reason}>
            {row.original.reason}
          </span>
        )}
      </div>
    ),
    meta: { className: "min-w-40" },
  }),
  helper.accessor((request) => request.durationSeconds, {
    id: "duration",
    header: "For",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => (
      <span className="whitespace-nowrap text-kumo-subtle">
        {formatDuration(row.original.durationSeconds)}
      </span>
    ),
    meta: { className: "hidden whitespace-nowrap md:table-cell" },
  }),
  helper.accessor((request) => request.phase, {
    id: "status",
    header: "Status",
    enableSorting: true,
    cell: ({ row }) => <StatusCell request={row.original} />,
    meta: { className: "min-w-32" },
  }),
  helper.accessor((request) => request.createdAt, {
    id: "created",
    header: "Asked",
    enableSorting: true,
    enableGlobalFilter: false,
    sortDescFirst: true,
    cell: ({ row }) => (
      <span className="text-kumo-subtle">
        <RelativeTime value={row.original.createdAt} />
      </span>
    ),
    meta: { className: "hidden whitespace-nowrap lg:table-cell" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const { me, canDecide } = table.options.meta ?? {};

      return me === undefined ? null : (
        <RequestMenu
          request={row.original}
          canDecide={canDecide === true}
          own={me.user?.id === row.original.userId}
        />
      );
    },
    meta: { className: "w-12 text-right", sticky: "right" },
  }),
]);

/** The phase, and under it who decided, what they said, or when the access ends. */
function StatusCell({ request }: { readonly request: RequestRow }): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <Badge variant={phaseVariants[request.phase]} className="w-fit">
        {phaseLabels[request.phase]}
      </Badge>
      <Detail request={request} />
    </div>
  );
}

function Detail({ request }: { readonly request: RequestRow }): ReactElement | null {
  const by = request.decidedBy === "" ? "" : ` by ${request.decidedBy}`;

  if (request.phase === "active" || request.phase === "expired") {
    return (
      <span className="truncate text-xs text-kumo-subtle">
        {request.phase === "active" ? "Ends " : "Ended "}
        <RelativeTime value={request.expiresAt} />
        {by}
      </span>
    );
  }

  if (request.phase === "denied") {
    return (
      <span className="truncate text-xs text-kumo-subtle" title={request.note}>
        {request.note === "" ? `Denied${by}` : `${request.note}${by}`}
      </span>
    );
  }

  return null;
}
