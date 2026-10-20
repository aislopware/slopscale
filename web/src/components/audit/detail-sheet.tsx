import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { cn } from "@cloudflare/kumo/utils";
import { XIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import type { AuditEvent } from "~/api/queries.ts";
import {
  ActionCell,
  ResultCell,
  TargetCell,
  actorName,
  detailText,
  rawText,
  actorKindLabel,
} from "~/components/audit/cells.tsx";
import { CopyButton, CopyText } from "~/components/ui/copy-text.tsx";
import { formatAbsolute, parseTime } from "~/lib/time.ts";

/**
 * Kumo's dialog turned into the Zero Trust log's detail sheet: pinned to the right edge at full
 * height, 432px wide (at most three quarters of a tablet's width, the whole of a phone's), sliding
 * in from the right over 300ms instead of scaling up in the middle.
 */
const sheetClass = cn(
  "inset-y-0 top-0 right-0 left-auto flex h-dvh translate-x-0 flex-col rounded-none sm:top-0",
  "w-full max-w-full sm:w-[432px] md:max-w-[75%]",
  "border-l border-kumo-line shadow-lg ring-0 duration-300",
  "data-starting-style:translate-x-full data-starting-style:scale-100 data-starting-style:opacity-100",
  "data-ending-style:translate-x-full data-ending-style:scale-100 data-ending-style:opacity-100",
);

/** One fact in the sheet: a 12px semibold label over its 14px value, a line under it. */
function Fact({
  label,
  children,
}: {
  readonly label: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-1.5 border-b border-kumo-line py-3">
      <dt className="text-xs font-semibold text-kumo-default">{label}</dt>
      <dd className="min-w-0 [overflow-wrap:anywhere] text-kumo-default">{children}</dd>
    </div>
  );
}

function FactGroup({
  title,
  children,
}: {
  readonly title: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <section className="flex flex-col gap-1">
      <h3 className="text-lg font-semibold text-kumo-default">{title}</h3>
      <dl className="flex flex-col">{children}</dl>
    </section>
  );
}

function SheetBody({ event }: { readonly event: AuditEvent }): ReactElement {
  const at = parseTime(event.createdAt);
  const fields = Object.entries(event.detail);
  const actorKind = actorKindLabel(event);
  const json = JSON.stringify(event.detail, null, 2);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-8 overflow-y-auto px-6 py-6">
      <FactGroup title="Basic information">
        <Fact label="Time">
          {at === null ? (
            event.createdAt
          ) : (
            <time dateTime={at.toISOString()}>{formatAbsolute(at)}</time>
          )}
        </Fact>
        <Fact label="Actor">
          <span className="flex flex-col gap-0.5">
            <span>{actorName(event)}</span>
            {actorKind === null ? null : (
              <span className="text-xs text-kumo-subtle">{actorKind}</span>
            )}
          </span>
        </Fact>
        <Fact label="Action">
          <ActionCell event={event} />
        </Fact>
        {event.targetKind === "" && event.targetId === "" && event.targetName === "" ? null : (
          <Fact label="Target">
            <TargetCell event={event} />
          </Fact>
        )}
        <Fact label="Result">
          <span className="flex items-center gap-2">
            <ResultCell event={event} />
            <span className="text-kumo-subtle">HTTP status {event.outcome}</span>
          </span>
        </Fact>
        {event.remoteAddr === "" ? null : (
          <Fact label="Remote address">
            <CopyText value={event.remoteAddr} label="Copy the remote address" />
          </Fact>
        )}
        <Fact label="Audit record">
          <CopyText value={event.id} label="Copy the audit record ID" />
        </Fact>
      </FactGroup>
      {fields.length === 0 ? null : (
        <FactGroup title="Details">
          {fields.map(([key, value]) => (
            <Fact key={key} label={key}>
              <CopyText
                value={detailText(value)}
                copy={rawText(value)}
                wrap
                label={`Copy ${key}`}
              />
            </Fact>
          ))}
          <div className="flex flex-col gap-1.5 py-3">
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs font-semibold text-kumo-default">Recorded data</span>
              <CopyButton value={json} label="Copy the recorded data" />
            </div>
            <pre className="overflow-x-auto rounded-lg bg-kumo-recessed px-3 py-2 font-mono text-xs leading-relaxed text-kumo-default ring ring-kumo-hairline">
              {json}
            </pre>
          </div>
        </FactGroup>
      )}
    </div>
  );
}

/**
 * Every recorded field of one event in a sheet at the right edge, as the Zero Trust admin log opens
 * a row: the basic information first, then the event's own details, each value one click from the
 * clipboard, and the recorded data as JSON. Always rendered, driven by `event`, so it slides out as
 * well as in; the last event stays in it while it leaves.
 */
export function EventSheet({
  event,
  onClose,
}: {
  /** The event to show, or null to close the sheet. */
  readonly event: AuditEvent | null;
  readonly onClose: () => void;
}): ReactElement {
  const [shown, setShown] = useState<AuditEvent | null>(event);

  if (event !== null && event !== shown) {
    setShown(event);
  }

  return (
    <Dialog.Root
      open={event !== null}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <Dialog className={sheetClass} style={{ transitionProperty: "translate" }}>
        <div className="flex items-start justify-between gap-4 border-b border-kumo-line px-6 py-5">
          <div className="flex min-w-0 flex-col gap-1">
            <Dialog.Title className="text-2xl font-semibold text-kumo-default">
              Event details
            </Dialog.Title>
            <Dialog.Description className="truncate text-kumo-subtle">
              {shown === null ? "" : shown.action}
            </Dialog.Description>
          </div>
          <Dialog.Close
            render={<Button variant="ghost" shape="square" icon={XIcon} aria-label="Close" />}
          />
        </div>
        {shown === null ? null : <SheetBody event={shown} />}
      </Dialog>
    </Dialog.Root>
  );
}
