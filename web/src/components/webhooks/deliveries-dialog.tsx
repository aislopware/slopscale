import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { SkeletonLine } from "@cloudflare/kumo/components/loader";
import { Table } from "@cloudflare/kumo/components/table";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { useQuery } from "@tanstack/react-query";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import type { Webhook, WebhookDelivery } from "~/api/queries.ts";
import { tableEmptyClass } from "~/components/table/empty.ts";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Status } from "~/components/ui/status.tsx";
import { formatDuration, urlHost } from "~/components/webhooks/model.ts";

const columnCount = 5;
const skeletonRows = 3;

/** The newest deliveries of one webhook, newest first, with how each went. */
export function DeliveriesDialog({
  webhook,
  open,
  onOpenChange,
}: {
  readonly webhook: Webhook;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="lg"
        title="Deliveries"
        description={`The last hundred deliveries to ${urlHost(webhook.url)}, newest first. A failed row shows the status after the final retry.`}
      >
        {open ? <DeliveriesBody webhook={webhook} /> : null}
        <DialogFooter>
          <DialogClose render={<Button variant="secondary">Close</Button>} />
        </DialogFooter>
      </DialogContent>
    </DialogRoot>
  );
}

function DeliveriesBody({ webhook }: { readonly webhook: Webhook }): ReactElement {
  // The server keeps the last hundred per webhook, newest first.
  const deliveries = useQuery(
    api.queryOptions("get", "/api/v1/webhook/{id}/deliveries", {
      params: { path: { id: webhook.id } },
    }),
  );

  if (deliveries.isError) {
    return <DialogError message={errorMessage(deliveries.error)} />;
  }

  const rows = deliveries.data?.deliveries ?? [];

  if (deliveries.isSuccess && rows.length === 0) {
    return (
      <Empty
        className={tableEmptyClass}
        size="sm"
        title="No deliveries yet"
        description="Nothing has been sent to this endpoint yet."
      />
    );
  }

  return (
    <div className="overflow-clip rounded-lg border border-kumo-line">
      <Table>
        <Table.Header variant="compact">
          <Table.Row>
            <Table.Head>Time</Table.Head>
            <Table.Head>Event</Table.Head>
            <Table.Head>Result</Table.Head>
            <Table.Head className="hidden sm:table-cell">Attempts</Table.Head>
            <Table.Head className="hidden text-right sm:table-cell">Took</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {deliveries.isPending
            ? Array.from({ length: skeletonRows }, (_, index) => (
                <Table.Row key={index}>
                  <Table.Cell colSpan={columnCount}>
                    <SkeletonLine className="w-full" />
                  </Table.Cell>
                </Table.Row>
              ))
            : rows.map((delivery, index) => (
                <DeliveryRow key={delivery.id} delivery={delivery} striped={index % 2 === 1} />
              ))}
        </Table.Body>
      </Table>
    </div>
  );
}

function DeliveryRow({
  delivery,
  striped,
}: {
  readonly delivery: WebhookDelivery;
  readonly striped: boolean;
}): ReactElement {
  return (
    <Table.Row className={striped ? "bg-kumo-elevated" : undefined}>
      <Table.Cell className="whitespace-nowrap text-kumo-subtle">
        <RelativeTime value={delivery.at} />
      </Table.Cell>
      <Table.Cell>
        <span className="font-mono text-[0.85em]">{delivery.eventType}</span>
      </Table.Cell>
      <Table.Cell>
        <ResultCell delivery={delivery} />
      </Table.Cell>
      <Table.Cell className="hidden tabular-nums sm:table-cell">{delivery.attempts}</Table.Cell>
      <Table.Cell className="hidden text-right whitespace-nowrap text-kumo-subtle tabular-nums sm:table-cell">
        {formatDuration(delivery.durationMs)}
      </Table.Cell>
    </Table.Row>
  );
}

/** The status code as a badge; an error text is shortened with the full text on hover. */
function ResultCell({ delivery }: { readonly delivery: WebhookDelivery }): ReactElement {
  return (
    <Tooltip content={delivery.status}>
      <Status tone={delivery.ok ? "success" : "danger"}>{resultLabel(delivery.status)}</Status>
    </Tooltip>
  );
}

function resultLabel(status: string): string {
  const maxLength = 32;

  if (Number.isInteger(Number(status))) {
    return `HTTP ${status}`;
  }

  return status.length > maxLength ? `${status.slice(0, maxLength)}…` : status;
}
