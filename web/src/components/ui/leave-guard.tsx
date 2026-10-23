import { useBlocker } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";

/**
 * Asks before a navigation would drop unsaved edits, and warns on reload too, the way the
 * dashboard's editors ask "Leave without saving?". A navigation that is the save itself passes
 * `ignoreBlocker`.
 */
export function LeaveGuard({
  dirty,
  description = "You will lose any changes made to this form.",
}: {
  readonly dirty: boolean;
  readonly description?: string;
}): ReactElement {
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
      description={description}
      confirmLabel="Confirm and exit"
      onConfirm={() => {
        blocker.proceed?.();
      }}
    />
  );
}
