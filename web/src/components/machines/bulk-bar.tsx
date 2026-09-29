import { Button } from "@cloudflare/kumo/components/button";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import {
  ArrowsClockwiseIcon,
  CheckIcon,
  SignOutIcon,
  TrashIcon,
  XIcon,
} from "@phosphor-icons/react";
import type { Icon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import {
  clientUpdatePlanSummary,
  planClientUpdates,
  useClientUpdateBulk,
  useMachineBulk,
} from "~/components/machines/bulk.ts";
import type { BulkAction, ClientUpdateOutcome } from "~/components/machines/bulk.ts";
import { BulkClientUpdateDialog } from "~/components/machines/dialogs.tsx";
import type { MachineSelection } from "~/components/machines/selection.tsx";
import { plural } from "~/components/overview/plural.ts";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { DialogClose, DialogContent, DialogFooter, DialogRoot } from "~/components/ui/dialog.tsx";
import { MachineName } from "~/components/ui/machine-name.tsx";

/**
 * What the table's header row says while machines are ticked: how many, and the things worth doing
 * to a group of them. Deleting asks first, because it cannot be undone; updating the clients
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
  const [updating, setUpdating] = useState(false);
  const ids = [...selection.selected];
  const plan = planClientUpdates(nodes, selection.selected);
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
    await updates.run(nodes, plan.eligible);
    setUpdating(false);
    selection.clear();
  }

  // The toolbar goes when the last tick does, but the two dialogs keep their place in the tree: a run
  // clears the selection, and a dialog that moved would be torn down as it opened or closed.
  return (
    <>
      {ids.length === 0 ? null : (
        <>
          {/* pl-3 starts the count where the machine names start in the column below. */}
          <div
            role="toolbar"
            aria-label="Actions for the selected machines"
            className="flex items-center gap-2 pl-3 font-normal text-kumo-default"
          >
            <span className="mr-1 font-medium">
              <span className="@3xl:hidden">{`${ids.length} selected`}</span>
              <span className="hidden @3xl:inline">{`${plural(ids.length, "machine")} selected`}</span>
            </span>
            <BulkButton
              icon={CheckIcon}
              label="Approve"
              disabled={busy}
              loading={bulk.running === "approve"}
              onClick={() => {
                void run("approve");
              }}
            />
            <BulkButton
              icon={ArrowsClockwiseIcon}
              label="Update clients…"
              disabled={busy || plan.eligible.length === 0}
              loading={updates.running}
              onClick={() => {
                setUpdating(true);
              }}
            />
            <BulkButton
              icon={SignOutIcon}
              label="Expire"
              disabled={busy}
              loading={bulk.running === "expire"}
              onClick={() => {
                void run("expire");
              }}
            />
            <BulkButton
              icon={TrashIcon}
              label="Remove…"
              disabled={busy}
              onClick={() => {
                setConfirming(true);
              }}
            />
            <BulkButton
              variant="ghost"
              icon={XIcon}
              label="Clear selection"
              disabled={busy}
              onClick={() => {
                selection.clear();
              }}
            />
          </div>
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
      <BulkClientUpdateDialog
        summary={clientUpdatePlanSummary(plan)}
        open={updating}
        onOpenChange={setUpdating}
        pending={updates.running}
        onConfirm={() => {
          void runUpdates();
        }}
      />
      <Refusals outcome={updates.outcome} onDismiss={dismissRefusals} />
    </>
  );
}

/**
 * One action on the header row, which must stay one line or the rows under it would move: labelled
 * where the frame has room, the icon alone with its label as a tooltip where it does not.
 */
function BulkButton({
  icon,
  label,
  variant = "secondary",
  disabled,
  loading = false,
  onClick,
}: {
  readonly icon: Icon;
  readonly label: string;
  readonly variant?: "secondary" | "ghost";
  readonly disabled: boolean;
  readonly loading?: boolean;
  readonly onClick: () => void;
}): ReactElement {
  const shared = { variant, size: "sm", icon, disabled, loading, onClick } as const;

  return (
    <>
      <Button {...shared} className="hidden @3xl:inline-flex">
        {label}
      </Button>
      <Tooltip
        content={label}
        render={<Button {...shared} shape="square" aria-label={label} className="@3xl:hidden" />}
      />
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
              <MachineName
                name={<span className="font-medium text-kumo-default">{refusal.name}</span>}
                owner={refusal.owner}
              />
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
