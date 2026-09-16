import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import type { ReactElement } from "react";

import { RequestMenu } from "~/components/access/request-menu.tsx";
import { phaseLabels } from "~/components/access/request-model.ts";
import type { RequestPhase, RequestRow } from "~/components/access/request-model.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { Badge } from "~/components/ui/badge.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import type { Tone } from "~/components/ui/status.tsx";
import { formatDuration } from "~/lib/time.ts";

const helper = createAppColumnHelper<RequestRow>();

const phaseTones: Record<RequestPhase, Tone> = {
  pending: "warning",
  active: "success",
  expired: "neutral",
  denied: "danger",
  cancelled: "neutral",
  revoked: "danger",
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
    // The reason is free text of any length, and this is the only column without a width of its
    // own, so without a cap it takes the slack and pushes the last columns under the pinned one.
    // min-width still wins over max-width, so the floor holds.
    meta: { className: "w-[30%] max-w-0 min-w-40" },
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
  helper.accessor((request) => (request.phase === "active" ? request.expiresAt : null), {
    id: "ends",
    header: "Ends",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <EndsCell request={row.original} />,
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

/** How long an active grant still has; nothing for a request that is not in effect. */
function EndsCell({ request }: { readonly request: RequestRow }): ReactElement | null {
  if (request.phase !== "active") {
    return null;
  }

  return (
    <span className="whitespace-nowrap text-kumo-subtle">
      <RelativeTime value={request.expiresAt} />
    </span>
  );
}

/** The phase, and under it who decided, what they said, or when the access ends. */
function StatusCell({ request }: { readonly request: RequestRow }): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <Badge tone={phaseTones[request.phase]}>{phaseLabels[request.phase]}</Badge>
      <Detail request={request} />
    </div>
  );
}

function Detail({ request }: { readonly request: RequestRow }): ReactElement | null {
  const by = request.decidedBy === "" ? "" : ` by ${request.decidedBy}`;

  if (request.phase === "revoked") {
    const endedBy = request.revokedBy === "" ? "" : ` by ${request.revokedBy}`;

    // Why the access was taken back is the part the requester came to read, so it is on the row
    // rather than behind a hover only a mouse can reach.
    return (
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className="truncate text-xs text-kumo-subtle">
          Ended <RelativeTime value={request.revokedAt} />
          {endedBy}
        </span>
        {request.revokeNote === "" ? null : (
          <Tooltip content={request.revokeNote}>
            <span className="truncate text-xs text-kumo-subtle">{request.revokeNote}</span>
          </Tooltip>
        )}
      </span>
    );
  }

  if (request.phase === "active") {
    // The Ends column carries the time wherever it is shown; saying it twice in one row is what
    // pushed the columns after it off the table.
    return (
      <span className="truncate text-xs text-kumo-subtle">
        <span className="md:hidden">
          Ends <RelativeTime value={request.expiresAt} />
          {by === "" ? "" : ", "}
        </span>
        {by === "" ? null : `approved${by}`}
      </span>
    );
  }

  if (request.phase === "expired") {
    return (
      <span className="truncate text-xs text-kumo-subtle">
        Ended <RelativeTime value={request.expiresAt} />
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
