import { useBlocker } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";

/** Asks before a navigation would drop unsaved edits, and warns on reload too. */
export function LeaveGuard({ dirty }: { readonly dirty: boolean }): ReactElement {
  const blocker = useBlocker({
    shouldBlockFn: () => dirty,
    enableBeforeUnload: dirty,
    withResolver: true,
  });

  return (
    <ConfirmDialog
      open={blocker.status === "blocked"}
      onOpenChange={(open) => {
        if (!open) {
          blocker.reset?.();
        }
      }}
      title="Leave without saving?"
      description="The policy has unsaved changes. They are lost if you leave this page."
      confirmLabel="Leave"
      onConfirm={() => {
        blocker.proceed?.();
      }}
    />
  );
}
