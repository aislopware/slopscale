import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

import type { AuditEvent } from "~/api/queries.ts";
import { clientError } from "~/components/audit/cells.tsx";
import { Frame, framePanelClass } from "~/components/ui/frame.tsx";

/** One actor: the user behind the call, or the credential kind when no user is bound. */
function actorKey(event: AuditEvent): string {
  return event.actorUserId === "" ? `${event.actorKind}:${event.actorName}` : event.actorUserId;
}

function Stat({
  label,
  value,
  hint,
  alert = false,
}: {
  readonly label: string;
  readonly value: number;
  readonly hint: string;
  /** Draws the number in the danger colour once it is not zero. */
  readonly alert?: boolean;
}): ReactElement {
  return (
    <div className={cn(framePanelClass, "flex flex-col gap-1 px-5 py-4")}>
      <span className="text-xs text-kumo-subtle">{label}</span>
      <span
        className={cn(
          "text-xl font-semibold tabular-nums",
          alert && value > 0 ? "text-kumo-danger" : "text-kumo-strong",
        )}
      >
        {value}
      </span>
      <span className="text-xs text-kumo-subtle">{hint}</span>
    </div>
  );
}

/**
 * What the loaded pages add up to. The server pages the log, so these count what is on screen,
 * which is what an operator is reading through; loading more raises them.
 */
export function AuditStats({ events }: { readonly events: readonly AuditEvent[] }): ReactElement {
  const actors = new Set(events.map((event) => actorKey(event)));
  const failures = events.filter((event) => event.outcome >= clientError).length;

  return (
    <Frame className="grid grid-cols-3 gap-1">
      <Stat label="Events loaded" value={events.length} hint="In the selected range" />
      <Stat label="Distinct actors" value={actors.size} hint="Users, keys and the CLI" />
      <Stat label="Failures" value={failures} hint="Outcome 400 or worse" alert />
    </Frame>
  );
}
