import { Button } from "@cloudflare/kumo/components/button";
import { CheckIcon, SignOutIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { useMachineBulk } from "~/components/machines/bulk.ts";
import type { BulkAction } from "~/components/machines/bulk.ts";
import type { MachineSelection } from "~/components/machines/selection.tsx";
import { plural } from "~/components/overview/plural.ts";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { FrameBand } from "~/components/ui/frame.tsx";

/**
 * What the band above the table says while machines are ticked: how many, and the three things
 * worth doing to a group of them. Deleting asks first, because it cannot be undone.
 */
export function MachineBulkBar({
  selection,
}: {
  readonly selection: MachineSelection;
}): ReactElement | null {
  const bulk = useMachineBulk();
  const [confirming, setConfirming] = useState(false);
  const ids = [...selection.selected];

  if (ids.length === 0) {
    return null;
  }

  const busy = bulk.running !== null;

  async function run(action: BulkAction): Promise<void> {
    await bulk.run(action, ids);
    selection.clear();
    setConfirming(false);
  }

  return (
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
          Delete…
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
        title={`Delete ${plural(ids.length, "machine")}?`}
        description="Each machine leaves the tailnet and has to register again to come back. Their routes and shares go with them."
        confirmLabel="Delete machines"
        loading={bulk.running === "delete"}
        onConfirm={() => {
          void run("delete");
        }}
      />
    </>
  );
}
