import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { PlusIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import { Mark } from "~/components/layout/mark.tsx";
import type { StatusFilter } from "~/components/machines/filters.ts";
import { tableEmptyClass } from "~/components/table/empty.ts";

/** The machines the first-run picture draws around the control server: a name and an address. */
const sketch = [
  { name: "web-server", address: "100.64.0.1", left: 50, top: 14 },
  { name: "laptop", address: "100.64.0.4", left: 17, top: 50 },
  { name: "db-replica", address: "100.64.0.2", left: 83, top: 50 },
  { name: "dev-vm", address: "100.64.0.3", left: 50, top: 86 },
] as const;

/**
 * The picture over the first-run card, as Mesh draws its empty node graph: the control server in
 * the middle on a soft glow, and a handful of machines around it, joined to it by dashed lines,
 * each a small card with its tailnet address in mono. It is decoration, so it is hidden from
 * assistive technology; the card under it says the same in words.
 */
export function MachinesSketch(): ReactElement {
  return (
    <div
      aria-hidden
      className="relative mx-auto h-56 w-full max-w-[300px] overflow-clip rounded-xl bg-kumo-base ring ring-kumo-line"
    >
      <div className="absolute inset-0 bg-[radial-gradient(circle_at_center,var(--color-kumo-brand)_0%,transparent_45%)] opacity-15" />
      <svg className="absolute inset-0 size-full" preserveAspectRatio="none">
        {sketch.map((machine) => (
          <line
            key={machine.name}
            x1="50%"
            y1="50%"
            x2={`${machine.left}%`}
            y2={`${machine.top}%`}
            stroke="var(--color-kumo-line)"
            strokeDasharray="3 3"
          />
        ))}
      </svg>
      <span className="absolute top-1/2 left-1/2 flex size-10 -translate-1/2 items-center justify-center rounded-full bg-kumo-base shadow-sm ring ring-kumo-line">
        <Mark className="size-6" />
      </span>
      {sketch.map((machine) => (
        <span
          key={machine.name}
          style={{ left: `${machine.left}%`, top: `${machine.top}%` }}
          className="absolute flex -translate-1/2 flex-col rounded-md bg-kumo-base px-2 py-1 shadow-xs ring ring-kumo-line"
        >
          <span className="flex items-center gap-1 text-[10px] font-medium text-kumo-default">
            <span className="size-1.5 rounded-full bg-kumo-success" />
            {machine.name}
          </span>
          <span className="font-mono text-[9px] text-kumo-subtle">{machine.address}</span>
        </span>
      ))}
    </div>
  );
}

/**
 * The Machines page of a tailnet nobody has joined yet, as Mesh greets an empty mesh: the picture,
 * then a card that says what a machine is here and offers the first one. The toolbar and the table
 * would only frame an empty box, so they wait for the first machine.
 */
export function MachinesFirstRun({
  onAddMachine,
}: {
  /** Opens the Add machine flow; absent for a caller who may not hand out keys. */
  readonly onAddMachine?: (() => void) | undefined;
}): ReactElement {
  return (
    <div className="flex flex-col gap-6">
      <MachinesSketch />
      <div className="flex flex-col items-center gap-4 rounded-lg bg-kumo-base px-6 py-12 text-center shadow-xs ring ring-kumo-line">
        <h2 className="text-2xl font-semibold text-kumo-default">No machines yet</h2>
        <p className="max-w-[60ch] text-base text-pretty text-kumo-subtle">
          A machine is any laptop, phone, server or container running Tailscale and pointed at this
          server. Once it joins, it reaches the other machines on the tailnet as the access rules
          allow.
        </p>
        {onAddMachine === undefined ? null : (
          <Button variant="primary" icon={PlusIcon} onClick={onAddMachine}>
            Add your first machine
          </Button>
        )}
      </div>
    </div>
  );
}

export interface MachinesEmptyProps {
  /** How many machines exist at all, before any filter. */
  readonly total: number;
  readonly status: StatusFilter;
  /** Whether a search or a user filter is narrowing the rows as well. */
  readonly narrowed: boolean;
  /** The caller sees only their own machines, so an empty list means they have none yet. */
  readonly ownOnly?: boolean;
  readonly onClearFilters: () => void;
}

/**
 * What the table says when it has no rows: how machines get here, or the way out of a filter that
 * hides them all. It does not repeat the toolbar's "Add machine" an inch above it.
 */
export function MachinesEmpty({
  total,
  status,
  narrowed,
  ownOnly = false,
  onClearFilters,
}: MachinesEmptyProps): ReactElement {
  if (total === 0 && ownOnly) {
    return (
      <Empty
        size="sm"
        className={tableEmptyClass}
        title="No machines of yours yet"
        description="Sign in on a machine and it appears here. Machines other users share with you show here too."
      />
    );
  }

  if (total === 0) {
    return (
      <Empty
        size="sm"
        className={tableEmptyClass}
        title="No machines yet"
        description="Register a machine with a pre-auth key or by signing in. New machines appear here on their own."
      />
    );
  }

  if (status === "pending" && !narrowed) {
    return (
      <Empty
        size="sm"
        className={tableEmptyClass}
        title="No machines need approval"
        contents={
          <Button variant="secondary" onClick={onClearFilters}>
            View all machines
          </Button>
        }
      />
    );
  }

  return (
    <Empty
      size="sm"
      className={tableEmptyClass}
      title="No machines match"
      description="Try a different search or filter."
      contents={
        <Button variant="secondary" onClick={onClearFilters}>
          Clear filters
        </Button>
      }
    />
  );
}
