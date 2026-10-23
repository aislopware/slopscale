import { Button } from "@cloudflare/kumo/components/button";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Webhook } from "~/api/queries.ts";
import { CreatedKey, useCreatedKey } from "~/components/keys/created-key.tsx";
import { DeleteDialog } from "~/components/ui/confirm-dialog.tsx";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import type { WebhookMutations } from "~/components/webhooks/mutations.ts";

const secretNote = "The secret signs every delivery and is shown only once. Copy it now.";

/** Confirms, rotates, then shows the new secret once. */
export function RotateSecretDialog({
  webhook,
  open,
  onOpenChange,
  mutations,
}: {
  readonly webhook: Webhook;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: WebhookMutations;
}): ReactElement {
  const { rotate } = mutations;
  const [secret, setSecret] = useCreatedKey(open);

  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title={secret === null ? "Rotate secret" : "Secret rotated"}
        description={
          secret === null
            ? "Deliveries are signed with the new secret from now on. Update the receiver or it cannot verify them."
            : undefined
        }
      >
        {secret === null ? (
          <div className="flex flex-col gap-4">
            <DialogError message={rotate.isError ? errorMessage(rotate.error) : undefined} />
            <DialogFooter>
              <Button
                variant="primary"
                loading={rotate.isPending}
                onClick={() => {
                  rotate.mutate(
                    { params: { path: { id: webhook.id } } },
                    {
                      onSuccess: (data) => {
                        setSecret(data.webhook.secret ?? "");
                      },
                    },
                  );
                }}
              >
                Rotate secret
              </Button>
              <DialogClose render={<Button variant="secondary">Cancel</Button>} />
            </DialogFooter>
          </div>
        ) : (
          <CreatedKey
            value={secret}
            note={secretNote}
            onDone={() => {
              onOpenChange(false);
            }}
          />
        )}
      </DialogContent>
    </DialogRoot>
  );
}

export function DeleteWebhookDialog({
  webhook,
  open,
  onOpenChange,
  mutations,
  onDeleted,
}: {
  readonly webhook: Webhook;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: WebhookMutations;
  /** Runs after the webhook is gone, such as leaving its page. */
  readonly onDeleted?: () => void;
}): ReactElement {
  const { remove } = mutations;

  return (
    <DeleteDialog
      open={open}
      onOpenChange={onOpenChange}
      resourceType="webhook"
      resourceName={webhook.url}
      deleteButtonText="Delete webhook"
      isDeleting={remove.isPending}
      {...(remove.isError ? { errorMessage: errorMessage(remove.error) } : {})}
      onDelete={() => {
        remove.mutate(
          { params: { path: { id: webhook.id } } },
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
