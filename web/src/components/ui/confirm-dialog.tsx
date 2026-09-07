import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { Dialog } from "@cloudflare/kumo/components/dialog";
import { WarningIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

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
      <Dialog size="sm" className="flex flex-col gap-4 px-6 py-5">
        <div className="flex items-center gap-3">
          {destructive ? (
            <span className="flex size-10 shrink-0 items-center justify-center rounded-full bg-kumo-danger/20">
              <WarningIcon size={20} weight="fill" className="text-kumo-danger" />
            </span>
          ) : null}
          <Dialog.Title className="text-lg font-semibold text-kumo-default">{title}</Dialog.Title>
        </div>
        {description === undefined ? null : (
          <Dialog.Description className="text-kumo-subtle">{description}</Dialog.Description>
        )}
        {error === undefined || error === "" ? null : (
          <Banner variant="error" title="Request failed" description={error} />
        )}
        <div className="flex justify-end gap-2 pt-1">
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
