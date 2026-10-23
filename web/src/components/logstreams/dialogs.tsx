import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { LogStream } from "~/api/queries.ts";
import type { LogStreamMutations } from "~/components/logstreams/mutations.ts";
import { DeleteDialog } from "~/components/ui/confirm-dialog.tsx";

export function DeleteLogStreamDialog({
  stream,
  open,
  onOpenChange,
  mutations,
  onDeleted,
}: {
  readonly stream: LogStream;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: LogStreamMutations;
  /** Runs after the stream is gone, such as leaving its page. */
  readonly onDeleted?: () => void;
}): ReactElement {
  const { remove } = mutations;

  return (
    <DeleteDialog
      open={open}
      onOpenChange={onOpenChange}
      resourceType="log stream"
      resourceName={stream.name}
      deleteButtonText="Delete log stream"
      isDeleting={remove.isPending}
      {...(remove.isError ? { errorMessage: errorMessage(remove.error) } : {})}
      onDelete={() => {
        remove.mutate(
          { params: { path: { id: stream.id } } },
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
