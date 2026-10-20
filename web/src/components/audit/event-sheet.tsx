import { useState } from "react";
import type { ReactElement } from "react";

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
import { Sheet, SheetFact, SheetSection } from "~/components/ui/sheet.tsx";
import { formatAbsolute, parseTime } from "~/lib/time.ts";

function SheetBody({ event }: { readonly event: AuditEvent }): ReactElement {
  const at = parseTime(event.createdAt);
  const fields = Object.entries(event.detail);
  const actorKind = actorKindLabel(event);
  const json = JSON.stringify(event.detail, null, 2);

  return (
    <>
      <SheetSection title="Basic information">
        <SheetFact label="Time">
          {at === null ? (
            event.createdAt
          ) : (
            <time dateTime={at.toISOString()}>{formatAbsolute(at)}</time>
          )}
        </SheetFact>
        <SheetFact label="Actor">
          <span className="flex flex-col gap-0.5">
            <span>{actorName(event)}</span>
            {actorKind === null ? null : (
              <span className="text-xs text-kumo-subtle">{actorKind}</span>
            )}
          </span>
        </SheetFact>
        <SheetFact label="Action">
          <ActionCell event={event} />
        </SheetFact>
        {event.targetKind === "" && event.targetId === "" && event.targetName === "" ? null : (
          <SheetFact label="Target">
            <TargetCell event={event} />
          </SheetFact>
        )}
        <SheetFact label="Result">
          <span className="flex items-center gap-2">
            <ResultCell event={event} />
            <span className="text-kumo-subtle">HTTP status {event.outcome}</span>
          </span>
        </SheetFact>
        {event.remoteAddr === "" ? null : (
          <SheetFact label="Remote address">
            <CopyText value={event.remoteAddr} label="Copy the remote address" />
          </SheetFact>
        )}
        <SheetFact label="Audit record">
          <CopyText value={event.id} label="Copy the audit record ID" />
        </SheetFact>
      </SheetSection>
      {fields.length === 0 ? null : (
        <SheetSection title="Details">
          {fields.map(([key, value]) => (
            <SheetFact key={key} label={key}>
              <CopyText
                value={detailText(value)}
                copy={rawText(value)}
                wrap
                label={`Copy ${key}`}
              />
            </SheetFact>
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
        </SheetSection>
      )}
    </>
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
    <Sheet
      open={event !== null}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
      title="Event details"
      description={shown === null ? "" : shown.action}
    >
      {shown === null ? null : <SheetBody event={shown} />}
    </Sheet>
  );
}
