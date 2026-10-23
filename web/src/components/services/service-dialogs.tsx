import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Service } from "~/api/queries.ts";
import { serviceLabel } from "~/components/services/model.ts";
import type { ServiceMutations } from "~/components/services/mutations.ts";
import { DeleteDialog } from "~/components/ui/confirm-dialog.tsx";

export function DeleteServiceDialog({
  service,
  open,
  onOpenChange,
  mutations,
  onDeleted,
}: {
  readonly service: Service;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: ServiceMutations;
  readonly onDeleted?: () => void;
}): ReactElement {
  const { remove } = mutations;

  return (
    <DeleteDialog
      open={open}
      onOpenChange={onOpenChange}
      resourceType="service"
      resourceName={serviceLabel(service.name)}
      deleteButtonText="Delete service"
      isDeleting={remove.isPending}
      {...(remove.isError ? { errorMessage: errorMessage(remove.error) } : {})}
      onDelete={() => {
        remove.mutate(
          { params: { path: { name: service.name } } },
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
