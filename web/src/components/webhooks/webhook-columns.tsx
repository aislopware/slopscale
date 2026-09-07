import { Badge } from "@cloudflare/kumo/components/badge";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import type { ReactElement } from "react";

import type { Webhook } from "~/api/queries.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import {
  deliveryLabel,
  deliveryState,
  providerLabel,
  urlHost,
} from "~/components/webhooks/model.ts";
import { WebhookMenu } from "~/components/webhooks/webhook-menu.tsx";

const helper = createAppColumnHelper<Webhook>();
const maxChips = 3;

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
    cell: ({ row }) => <SubscriptionsCell webhook={row.original} />,
    meta: { className: "min-w-40" },
  }),
  helper.accessor((webhook) => webhook.lastDeliveryAt ?? "", {
    id: "delivery",
    header: "Last delivery",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <DeliveryCell webhook={row.original} />,
    meta: { className: "whitespace-nowrap" },
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
    meta: { className: "w-12 text-right" },
  }),
]);

function EndpointCell({ webhook }: { readonly webhook: Webhook }): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <Tooltip content={webhook.url}>
        <span className="truncate font-medium text-kumo-default">{urlHost(webhook.url)}</span>
      </Tooltip>
      <span className="truncate text-xs text-kumo-subtle">
        {webhook.description === "" ? webhook.url : webhook.description}
      </span>
    </div>
  );
}

/** The first few event types as chips, the rest folded into a count with the full list on hover. */
function SubscriptionsCell({ webhook }: { readonly webhook: Webhook }): ReactElement {
  const shown = webhook.subscriptions.slice(0, maxChips);
  const rest = webhook.subscriptions.slice(maxChips);

  return (
    <div className="flex flex-wrap gap-1">
      {shown.map((type) => (
        <Badge key={type} variant="secondary" className="font-mono text-[0.85em]">
          {type}
        </Badge>
      ))}
      {rest.length === 0 ? null : (
        <Tooltip content={rest.join(", ")}>
          <Badge variant="outline">{`+${rest.length}`}</Badge>
        </Tooltip>
      )}
    </div>
  );
}

function DeliveryCell({ webhook }: { readonly webhook: Webhook }): ReactElement {
  const state = deliveryState(webhook);

  if (state === "never") {
    return <span className="text-kumo-inactive">Never</span>;
  }

  return (
    <span className="flex items-center gap-2">
      <Tooltip content={webhook.lastDeliveryStatus}>
        <Badge variant={state === "ok" ? "success" : "error"}>{deliveryLabel(webhook)}</Badge>
      </Tooltip>
      <span className="text-kumo-subtle">
        <RelativeTime value={webhook.lastDeliveryAt} />
      </span>
    </span>
  );
}
