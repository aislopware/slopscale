import { DeleteResource } from "@cloudflare/kumo";
import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { ClockCounterClockwiseIcon, DotsThreeIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";

const actionsIconSize = 18;

export interface KeyAction {
  readonly pending: boolean;
  readonly error: string | undefined;
  /** Runs the mutation; call `done` once it succeeds to close the dialog. */
  readonly run: (done: () => void) => void;
}

export interface ExpireAction extends KeyAction {
  readonly title: string;
  readonly description: string;
}

export interface DeleteAction extends KeyAction {
  /** "pre-auth key" / "API key": what the confirmation asks the operator to name. */
  readonly resourceType: string;
  /** The prefix the operator has to type back to unlock the red button. */
  readonly resourceName: string;
}

export interface KeyActionsProps {
  /** Accessible name of the trigger, such as "Actions for key tskey-abc". */
  readonly label: string;
  readonly disabled?: boolean;
  readonly expire: ExpireAction;
  readonly remove: DeleteAction;
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
            icon={ClockCounterClockwiseIcon}
            variant="danger"
            disabled={disabled}
            onClick={() => {
              setDialog("expire");
            }}
          >
            Expire…
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
      <ConfirmDialog
        open={dialog === "expire"}
        onOpenChange={(next) => {
          if (!next) {
            close();
          }
        }}
        title={expire.title}
        description={expire.description}
        confirmLabel="Expire key"
        loading={expire.pending}
        error={expire.error}
        onConfirm={() => {
          expire.run(close);
        }}
      />
      <DeleteResource
        open={dialog === "delete"}
        onOpenChange={(next) => {
          if (!next) {
            close();
          }
        }}
        resourceType={remove.resourceType}
        resourceName={remove.resourceName}
        deleteButtonText={`Delete ${remove.resourceType}`}
        isDeleting={remove.pending}
        {...(remove.error === undefined ? {} : { errorMessage: remove.error })}
        onDelete={() => {
          remove.run(close);
        }}
      />
    </>
  );
}
