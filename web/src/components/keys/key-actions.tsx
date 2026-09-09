import { DeleteResource } from "@cloudflare/kumo";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { ArrowsClockwiseIcon, ClockCounterClockwiseIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";

/** Why every item is unavailable when the whole menu is disabled by the caller's credentials. */
const readOnlyReason = "Your credentials may not change keys";

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

/**
 * The rotate entry: the dialog belongs to the caller, because minting a secret ends in a one-time
 * reveal rather than in a yes/no answer.
 */
export interface RotateEntry {
  readonly onSelect: () => void;
  /** Why the key cannot be rotated, or undefined while it can. */
  readonly reason: string | undefined;
}

export interface KeyActionsProps {
  /** Accessible name of the trigger, such as "Actions for key tskey-abc". */
  readonly label: string;
  readonly disabled?: boolean;
  /** Present only for a credential whose secret can be replaced in place, such as an API key. */
  readonly rotate?: RotateEntry;
  /** Absent for a credential that cannot expire, such as an OAuth client. */
  readonly expire?: ExpireAction;
  readonly remove: DeleteAction;
}

type Dialog = "expire" | "delete";

/** The row menu the key tables use: expire the key, or delete it outright. */
export function KeyActions({
  label,
  disabled = false,
  rotate,
  expire,
  remove,
}: KeyActionsProps): ReactElement {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const close = (): void => {
    setDialog(null);
  };

  return (
    <>
      <RowMenu label={label}>
        {rotate === undefined ? null : (
          <>
            <DisabledReason reason={disabled ? readOnlyReason : rotate.reason}>
              <DropdownMenu.Item
                icon={ArrowsClockwiseIcon}
                disabled={disabled || rotate.reason !== undefined}
                onClick={() => {
                  rotate.onSelect();
                }}
              >
                Rotate secret…
              </DropdownMenu.Item>
            </DisabledReason>
            <DropdownMenu.Separator />
          </>
        )}
        {expire === undefined ? null : (
          <>
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
          </>
        )}
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
      </RowMenu>
      {expire === undefined ? null : (
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
      )}
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
