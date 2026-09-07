import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { ClockIcon, DotsThreeIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";

const actionsIconSize = 18;

export interface KeyAction {
  readonly title: string;
  readonly description: string;
  readonly confirmLabel: string;
  readonly pending: boolean;
  readonly error: string | undefined;
  /** Runs the mutation; call `done` once it succeeds to close the dialog. */
  readonly run: (done: () => void) => void;
}

export interface KeyActionsProps {
  /** Accessible name of the trigger, such as "Actions for key tskey-abc". */
  readonly label: string;
  readonly disabled?: boolean;
  readonly expire: KeyAction;
  readonly remove: KeyAction;
}

type Dialog = "expire" | "delete";

/** The row menu both key tables use: expire the key, or delete it outright. */
export function KeyActions({
  label,
  disabled = false,
  expire,
  remove,
}: KeyActionsProps): ReactElement {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const close = (): void => {
    setDialog(null);
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenu.Trigger
          render={
            <Button
              variant="ghost"
              shape="square"
              size="sm"
              icon={<DotsThreeIcon size={actionsIconSize} weight="bold" />}
              aria-label={label}
            />
          }
        />
        <DropdownMenu.Content align="end">
          <DropdownMenu.Item
            icon={ClockIcon}
            disabled={disabled}
            onClick={() => {
              setDialog("expire");
            }}
          >
            Expire
          </DropdownMenu.Item>
          <DropdownMenu.Separator />
          <DropdownMenu.Item
            icon={TrashIcon}
            variant="danger"
            disabled={disabled}
            onClick={() => {
              setDialog("delete");
            }}
          >
            Delete…
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu>
      <ActionDialog
        action={expire}
        open={dialog === "expire"}
        destructive={false}
        onClose={close}
      />
      <ActionDialog action={remove} open={dialog === "delete"} destructive onClose={close} />
    </>
  );
}

function ActionDialog({
  action,
  open,
  destructive,
  onClose,
}: {
  readonly action: KeyAction;
  readonly open: boolean;
  readonly destructive: boolean;
  readonly onClose: () => void;
}): ReactElement {
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          onClose();
        }
      }}
      title={action.title}
      description={action.description}
      confirmLabel={action.confirmLabel}
      destructive={destructive}
      loading={action.pending}
      error={action.error}
      onConfirm={() => {
        action.run(onClose);
      }}
    />
  );
}
