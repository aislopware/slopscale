import { Empty } from "@cloudflare/kumo/components/empty";
import { SkeletonLine } from "@cloudflare/kumo/components/loader";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { useQuery } from "@tanstack/react-query";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import type { Webhook, WebhookDelivery } from "~/api/queries.ts";
import { Callout } from "~/components/ui/callout.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Sheet } from "~/components/ui/sheet.tsx";
import { Status } from "~/components/ui/status.tsx";
import { formatDuration, urlHost } from "~/components/webhooks/model.ts";

const skeletonRows = 4;

/**
 * The newest deliveries of one webhook in a sheet at the right edge, as the dashboard opens a log:
 * the page stays in view beside it. Always rendered, driven by `open`, so it slides out as well as
 * in; the deliveries are fetched only while it is open.
 */
export function DeliveriesSheet({
  webhook,
  open,
  onOpenChange,
}: {
  readonly webhook: Webhook;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  return (
    <Sheet
      open={open}
      onOpenChange={onOpenChange}
      title="Deliveries"
      description={urlHost(webhook.url)}
    >
      <div className="flex flex-col gap-3">
        <p className="text-kumo-subtle">
          The last hundred deliveries, newest first. A failed one shows the result of its final
          retry.
        </p>
        <DeliveriesBody webhook={webhook} open={open} />
      </div>
    </Sheet>
  );
}

function DeliveriesBody({
  webhook,
  open,
}: {
  readonly webhook: Webhook;
  readonly open: boolean;
}): ReactElement {
  // The server keeps the last hundred per webhook, newest first.
  const deliveries = useQuery({
    ...api.queryOptions("get", "/api/v1/webhook/{id}/deliveries", {
      params: { path: { id: webhook.id } },
    }),
    enabled: open,
  });

  if (deliveries.isError) {
    return (
      <Callout
        tone="error"
        title="The deliveries could not be loaded"
        description={errorMessage(deliveries.error)}
      />
    );
  }

  const rows = deliveries.data?.deliveries ?? [];

  if (deliveries.isSuccess && rows.length === 0) {
    return (
      <Empty
        size="sm"
        title="No deliveries yet"
        description="Nothing has been sent to this endpoint yet."
      />
    );
  }

  return (
    <ul aria-label="Deliveries" className="flex flex-col">
      {deliveries.isSuccess
        ? rows.map((delivery) => <DeliveryItem key={delivery.id} delivery={delivery} />)
        : Array.from({ length: skeletonRows }, (_, index) => (
            <li key={index} className="border-b border-kumo-line py-3">
              <SkeletonLine className="w-full" />
            </li>
          ))}
    </ul>
  );
}

/**
 * One delivery as a log line: how it went and which event it carried, when, then how many tries it
 * took and how long. The full error text is on the result's hover.
 */
function DeliveryItem({ delivery }: { readonly delivery: WebhookDelivery }): ReactElement {
  return (
    <li className="flex min-w-0 flex-col gap-1.5 border-b border-kumo-line py-3">
      <div className="flex min-w-0 items-center justify-between gap-3">
        <span className="flex min-w-0 items-center gap-2">
          <Tooltip content={delivery.status}>
            <Status tone={delivery.ok ? "success" : "danger"}>
              {resultLabel(delivery.status)}
            </Status>
          </Tooltip>
          <span className="truncate font-mono text-[0.85em]">{delivery.eventType}</span>
        </span>
        <span className="shrink-0 text-xs whitespace-nowrap text-kumo-subtle">
          <RelativeTime value={delivery.at} />
        </span>
      </div>
      <span className="text-xs text-kumo-subtle tabular-nums">
        {delivery.attempts === 1 ? "1 attempt" : `${delivery.attempts} attempts`} ·{" "}
        {formatDuration(delivery.durationMs)}
      </span>
    </li>
  );
}

function resultLabel(status: string): string {
  const maxLength = 32;

  if (Number.isInteger(Number(status))) {
    return `HTTP ${status}`;
  }

  // The server reports a word (unreachable, rejected) or a short error; the line starts a phrase.
  const sentence = status.charAt(0).toUpperCase() + status.slice(1);

  return sentence.length > maxLength ? `${sentence.slice(0, maxLength)}…` : sentence;
}
