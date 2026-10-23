import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { PostureIntegration } from "~/api/queries.ts";
import type { PostureIntegrationMutations } from "~/components/posture-integrations/mutations.ts";
import { DeleteDialog } from "~/components/ui/confirm-dialog.tsx";

export function DeletePostureIntegrationDialog({
  integration,
  open,
  onOpenChange,
  mutations,
  onDeleted,
}: {
  readonly integration: PostureIntegration;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: PostureIntegrationMutations;
  /** Runs after the integration is gone, such as leaving its page. */
  readonly onDeleted?: () => void;
}): ReactElement {
  const { remove } = mutations;

  return (
    <DeleteDialog
      open={open}
      onOpenChange={onOpenChange}
      resourceType="posture integration"
      resourceName={integration.name}
      deleteButtonText="Delete integration"
      isDeleting={remove.isPending}
      {...(remove.isError ? { errorMessage: errorMessage(remove.error) } : {})}
      onDelete={() => {
        remove.mutate(
          { params: { path: { id: integration.id } } },
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
