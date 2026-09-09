import { Button } from "@cloudflare/kumo/components/button";
import { ArrowsClockwiseIcon, CheckIcon, SignOutIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import {
  outdatedSelection,
  useClientUpdateBulk,
  useMachineBulk,
} from "~/components/machines/bulk.ts";
import type { BulkAction, ClientUpdateOutcome } from "~/components/machines/bulk.ts";
import type { MachineSelection } from "~/components/machines/selection.tsx";
import { plural } from "~/components/overview/plural.ts";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { DialogClose, DialogContent, DialogFooter, DialogRoot } from "~/components/ui/dialog.tsx";
import { FrameBand } from "~/components/ui/frame.tsx";

/**
 * What the band above the table says while machines are ticked: how many, and the things worth
 * doing to a group of them. Deleting asks first, because it cannot be undone; updating the clients
 * reaches only the ticked machines that are connected and behind, since the rest would refuse.
 */
export function MachineBulkBar({
  selection,
  nodes,
}: {
  readonly selection: MachineSelection;
  /** The machines the filters leave, which is what the ticked ids point into. */
  readonly nodes: readonly Node[];
}): ReactElement {
  const bulk = useMachineBulk();
  const updates = useClientUpdateBulk();
  const [confirming, setConfirming] = useState(false);
  const ids = [...selection.selected];
  const outdated = outdatedSelection(nodes, selection.selected);
  const busy = bulk.running !== null || updates.running;

  async function run(action: BulkAction): Promise<void> {
    await bulk.run(action, ids);
    selection.clear();
    setConfirming(false);
  }

  function dismissRefusals(): void {
    updates.clearOutcome();
  }

  async function runUpdates(): Promise<void> {
    await updates.run(nodes, outdated);
    selection.clear();
  }

  // The band goes when the last tick does, but the refusals dialog keeps its place in the tree: a
  // run clears the selection, and a dialog that moved would be torn down as it opened.
  return (
    <>
      {ids.length === 0 ? null : (
        <>
          {/* px-5 lines the count up with the first column of the table below. */}
          <FrameBand className="flex flex-wrap items-center gap-2 px-5">
            <span className="mr-1 font-medium text-kumo-default">
              {`${plural(ids.length, "machine")} selected`}
            </span>
            <Button
              variant="secondary"
              size="sm"
              icon={CheckIcon}
              disabled={busy}
              loading={bulk.running === "approve"}
              onClick={() => {
                void run("approve");
              }}
            >
              Approve
            </Button>
            <Button
              variant="secondary"
              size="sm"
              icon={ArrowsClockwiseIcon}
              disabled={busy || outdated.length === 0}
              loading={updates.running}
              onClick={() => {
                void runUpdates();
              }}
            >
              Update clients
            </Button>
            <Button
              variant="secondary"
              size="sm"
              icon={SignOutIcon}
              disabled={busy}
              loading={bulk.running === "expire"}
              onClick={() => {
                void run("expire");
              }}
            >
              Expire
            </Button>
            <Button
              variant="secondary"
              size="sm"
              icon={TrashIcon}
              disabled={busy}
              onClick={() => {
                setConfirming(true);
              }}
            >
              Remove…
            </Button>
            <Button
              variant="ghost"
              size="sm"
              disabled={busy}
              onClick={() => {
                selection.clear();
              }}
            >
              Clear selection
            </Button>
          </FrameBand>
          <ConfirmDialog
            open={confirming}
            onOpenChange={setConfirming}
            title={`Remove ${plural(ids.length, "machine")}?`}
            description="Each machine leaves the tailnet and has to register again to come back. Their routes and shares go with them."
            confirmLabel="Remove machines"
            loading={bulk.running === "delete"}
            onConfirm={() => {
              void run("delete");
            }}
          />
        </>
      )}
      <Refusals outcome={updates.outcome} onDismiss={dismissRefusals} />
    </>
  );
}

/**
 * Which clients would not take the update on, in their own words. The toast says how many; a
 * refusal is per machine and usually says something the operator has to act on, so it gets the room
 * a toast does not have.
 */
function Refusals({
  outcome,
  onDismiss,
}: {
  readonly outcome: ClientUpdateOutcome | null;
  readonly onDismiss: () => void;
}): ReactElement {
  const refused = outcome?.refused ?? [];

  return (
    <DialogRoot
      open={refused.length > 0}
      onOpenChange={(open) => {
        if (!open) {
          onDismiss();
        }
      }}
    >
      <DialogContent
        size="base"
        title="Clients that did not update"
        description={`${plural(outcome?.started ?? 0, "machine")} started. The rest answered for themselves.`}
      >
        <ul className="flex flex-col gap-3">
          {/* The machine is who this is about, not a state, so it reads as a name; what its client
              said is the line under it. */}
          {refused.map((refusal) => (
            <li key={refusal.nodeId} className="flex flex-col gap-0.5">
              <span className="font-medium text-kumo-default">{refusal.name}</span>
              <span className="text-kumo-subtle">{refusal.message}</span>
            </li>
          ))}
        </ul>
        <DialogFooter>
          <DialogClose render={<Button variant="secondary">Close</Button>} />
        </DialogFooter>
      </DialogContent>
    </DialogRoot>
  );
}
