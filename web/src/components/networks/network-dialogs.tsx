import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Network } from "~/api/queries.ts";
import type { NetworkMutations } from "~/components/networks/mutations.ts";
import { DeleteDialog } from "~/components/ui/confirm-dialog.tsx";

export function DeleteNetworkDialog({
  network,
  open,
  onOpenChange,
  mutations,
  onDeleted,
}: {
  readonly network: Network;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: NetworkMutations;
  /** Leaves the network's page once it is gone. */
  readonly onDeleted?: () => void;
}): ReactElement {
  const { remove } = mutations;

  return (
    <DeleteDialog
      open={open}
      onOpenChange={onOpenChange}
      resourceType="network"
      resourceName={network.name}
      deleteButtonText="Delete network"
      isDeleting={remove.isPending}
      {...(remove.isError ? { errorMessage: errorMessage(remove.error) } : {})}
      onDelete={() => {
        remove.mutate(
          { params: { path: { id: network.id } } },
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
