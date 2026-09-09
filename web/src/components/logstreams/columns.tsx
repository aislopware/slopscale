import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import type { ReactElement } from "react";

import type { LogStream } from "~/api/queries.ts";
import { LogStreamMenu } from "~/components/logstreams/menu.tsx";
import {
  countersLabel,
  destinationLabel,
  statusLabel,
  streamState,
} from "~/components/logstreams/model.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Status } from "~/components/ui/status.tsx";
import { UrlText } from "~/components/ui/url-text.tsx";

const helper = createAppColumnHelper<LogStream>();

export const logStreamColumns = helper.columns([
  helper.accessor((stream) => `${stream.name} ${stream.url}`, {
    id: "name",
    header: "Stream",
    enableSorting: true,
    cell: ({ row }) => <StreamCell stream={row.original} />,
    meta: { className: "w-[30%] min-w-48" },
  }),
  helper.accessor((stream) => destinationLabel(stream.destination), {
    id: "destination",
    header: "Destination",
    enableSorting: true,
    cell: ({ getValue }) => <span className="whitespace-nowrap">{getValue()}</span>,
    meta: { className: "hidden whitespace-nowrap md:table-cell" },
  }),
  helper.accessor((stream) => stream.delivered, {
    id: "counters",
    header: "Entries",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => (
      <span className="whitespace-nowrap text-kumo-subtle">{countersLabel(row.original)}</span>
    ),
    meta: { className: "hidden md:table-cell" },
  }),
  helper.accessor((stream) => stream.lastDeliveryAt ?? "", {
    id: "delivery",
    header: "Last batch",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <DeliveryCell stream={row.original} />,
    meta: { className: "w-[22%] [&>button]:whitespace-nowrap" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const { me } = table.options.meta ?? {};

      return me === undefined ? null : <LogStreamMenu stream={row.original} me={me} />;
    },
    meta: { className: "w-12 text-right", sticky: "right" },
  }),
]);

function StreamCell({ stream }: { readonly stream: LogStream }): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="truncate font-medium text-kumo-default">{stream.name}</span>
      <Tooltip content={stream.url}>
        <UrlText url={stream.url} className="truncate text-xs" />
      </Tooltip>
      {/* The destination and counters are hidden on small screens, so the stream carries them there. */}
      <span className="truncate text-xs text-kumo-subtle md:hidden">
        {`${destinationLabel(stream.destination)} · ${countersLabel(stream)}`}
      </span>
    </div>
  );
}

function DeliveryCell({ stream }: { readonly stream: LogStream }): ReactElement {
  const state = streamState(stream);

  if (state === "disabled") {
    return <Status tone="neutral">Disabled</Status>;
  }

  if (state === "never") {
    return <span className="text-kumo-subtle">Never</span>;
  }

  return (
    <span className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
      <Tooltip content={stream.lastDeliveryStatus}>
        <Status tone={state === "ok" ? "success" : "danger"}>{statusLabel(stream)}</Status>
      </Tooltip>
      <span className="whitespace-nowrap text-kumo-subtle">
        <RelativeTime value={stream.lastDeliveryAt} />
      </span>
    </span>
  );
}
