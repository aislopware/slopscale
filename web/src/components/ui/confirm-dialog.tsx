import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { cn } from "@cloudflare/kumo/utils";
import { WarningIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import {
  dialogActionsClass,
  dialogFrameClass,
  dialogHeaderClass,
  dialogPanelClass,
} from "~/components/ui/dialog.tsx";

export interface ConfirmDialogProps {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly title: string;
  readonly description?: string;
  readonly confirmLabel?: string;
  readonly destructive?: boolean;
  readonly loading?: boolean;
  readonly error?: string | undefined;
  readonly onConfirm: () => void;
}

/** A modal yes/no question; destructive by default because that is what it is for. */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel = "Confirm",
  destructive = true,
  loading = false,
  error,
  onConfirm,
}: ConfirmDialogProps): ReactElement {
  return (
    <Dialog.Root role="alertdialog" open={open} onOpenChange={onOpenChange}>
      <Dialog size="sm" className={dialogFrameClass}>
        <div className={dialogPanelClass}>
          <div className={cn(dialogHeaderClass, "justify-start gap-3")}>
            {destructive ? (
              <WarningIcon size={20} weight="fill" className="shrink-0 text-kumo-danger" />
            ) : null}
            <Dialog.Title className="text-lg font-semibold text-kumo-default">{title}</Dialog.Title>
          </div>
          <div className="flex flex-col gap-4 p-6">
            {description === undefined ? null : (
              <Dialog.Description className="max-w-prose text-pretty text-kumo-subtle">
                {description}
              </Dialog.Description>
            )}
            {error === undefined || error === "" ? null : (
              <Banner variant="error" title="Request failed" description={error} />
            )}
          </div>
        </div>
        <div className={dialogActionsClass}>
          <Dialog.Close render={<Button variant="secondary">Cancel</Button>} />
          <Button
            variant={destructive ? "destructive" : "primary"}
            loading={loading}
            onClick={onConfirm}
          >
            {confirmLabel}
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
