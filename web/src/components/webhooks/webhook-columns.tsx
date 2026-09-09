import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import type { ReactElement } from "react";

import type { Webhook } from "~/api/queries.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Status } from "~/components/ui/status.tsx";
import { UrlText } from "~/components/ui/url-text.tsx";
import {
  countEvents,
  deliveryLabel,
  deliveryState,
  providerLabel,
} from "~/components/webhooks/model.ts";
import { WebhookMenu } from "~/components/webhooks/webhook-menu.tsx";

const helper = createAppColumnHelper<Webhook>();

export const webhookColumns = helper.columns([
  helper.accessor((webhook) => `${webhook.url} ${webhook.description}`, {
    id: "url",
    header: "Endpoint",
    enableSorting: true,
    cell: ({ row }) => <EndpointCell webhook={row.original} />,
    meta: { className: "w-[30%] min-w-48" },
  }),
  helper.accessor((webhook) => providerLabel(webhook.providerType), {
    id: "provider",
    header: "Provider",
    enableSorting: true,
    cell: ({ getValue }) => <span className="whitespace-nowrap">{getValue()}</span>,
    meta: { className: "hidden whitespace-nowrap md:table-cell" },
  }),
  helper.accessor((webhook) => webhook.subscriptions.join(" "), {
    id: "subscriptions",
    header: "Events",
    enableSorting: false,
    cell: ({ row, table }) => (
      <SubscriptionsCell webhook={row.original} eventTypes={table.options.meta?.eventTypes} />
    ),
    meta: { className: "hidden min-w-40 md:table-cell" },
  }),
  helper.accessor((webhook) => webhook.lastDeliveryAt ?? "", {
    id: "delivery",
    header: "Last delivery",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <DeliveryCell webhook={row.original} />,
    // The header stays on one line; the cell may wrap its badge and time on a phone.
    meta: { className: "w-[22%] [&>button]:whitespace-nowrap" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const { me, eventTypes } = table.options.meta ?? {};

      return me === undefined ? null : (
        <WebhookMenu webhook={row.original} eventTypes={eventTypes ?? []} me={me} />
      );
    },
    meta: { className: "w-12 text-right", sticky: "right" },
  }),
]);

function EndpointCell({ webhook }: { readonly webhook: Webhook }): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <Tooltip content={webhook.url}>
        <UrlText url={webhook.url} className="truncate text-sm [&>span:nth-child(2)]:font-medium" />
      </Tooltip>
      {webhook.description === "" ? null : (
        <span className="truncate text-xs text-kumo-subtle">{webhook.description}</span>
      )}
      {/* The provider and events columns are hidden on small screens, so the endpoint carries them there. */}
      <span className="truncate text-xs text-kumo-subtle md:hidden">
        {`${providerLabel(webhook.providerType)} · ${countEvents(webhook.subscriptions.length)}`}
      </span>
    </div>
  );
}

function SubscriptionsCell({
  webhook,
  eventTypes,
}: {
  readonly webhook: Webhook;
  readonly eventTypes?: readonly string[] | undefined;
}): ReactElement {
  const isAll =
    eventTypes !== undefined &&
    eventTypes.length > 0 &&
    eventTypes.every((type) => webhook.subscriptions.includes(type));

  const label = isAll ? "All events" : countEvents(webhook.subscriptions.length);

  return (
    <Tooltip content={webhook.subscriptions.join(", ")}>
      <span>{label}</span>
    </Tooltip>
  );
}

function DeliveryCell({ webhook }: { readonly webhook: Webhook }): ReactElement {
  const state = deliveryState(webhook);

  if (state === "never") {
    return <span className="text-kumo-subtle">Never</span>;
  }

  return (
    <span className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
      <Tooltip content={webhook.lastDeliveryStatus}>
        <Status tone={state === "ok" ? "success" : "danger"}>{deliveryLabel(webhook)}</Status>
      </Tooltip>
      <span className="whitespace-nowrap text-kumo-subtle">
        <RelativeTime value={webhook.lastDeliveryAt} />
      </span>
    </span>
  );
}
