import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { App } from "~/api/queries.ts";
import type { AppMutations } from "~/components/apps/mutations.ts";
import { DeleteDialog } from "~/components/ui/confirm-dialog.tsx";

export function DeleteAppDialog({
  app,
  open,
  onOpenChange,
  mutations,
  onDeleted,
}: {
  readonly app: App;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AppMutations;
  /** Called once the app is gone, so its page can leave. */
  readonly onDeleted?: () => void;
}): ReactElement {
  const { remove } = mutations;

  return (
    <DeleteDialog
      open={open}
      onOpenChange={onOpenChange}
      resourceType="app"
      resourceName={app.name}
      deleteButtonText="Delete app"
      isDeleting={remove.isPending}
      {...(remove.isError ? { errorMessage: errorMessage(remove.error) } : {})}
      onDelete={() => {
        remove.mutate(
          { params: { path: { id: app.id } } },
          {
            onSuccess: () => {
              onOpenChange(false);
              onDeleted?.();
            },
          },
        );
      }}
    />
  );
}
