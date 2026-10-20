import { Banner } from "@cloudflare/kumo/components/banner";
import { Input } from "@cloudflare/kumo/components/input";
import { LayerDialog } from "@cloudflare/kumo/components/layer-dialog";
import { useId, useState } from "react";
import type { ReactElement, ReactNode } from "react";

import { copyText } from "~/lib/copy.ts";

export interface ConfirmDialogProps {
  readonly open: boolean;
  /** Called with Base UI's event details too, so a caller can tell a dismissal from a cancel. */
  readonly onOpenChange: (open: boolean, eventDetails?: unknown) => void;
  readonly title: string;
  readonly description?: ReactNode;
  readonly confirmLabel?: string;
  readonly destructive?: boolean;
  readonly loading?: boolean;
  readonly error?: string | undefined;
  /**
   * The name of the thing being deleted. When given, the action stays disabled until it is typed
   * into the dialog, as the dashboard asks before deleting a named resource.
   */
  readonly confirmName?: string | undefined;
  readonly onConfirm: () => void;
}

/** "Type in <name> to confirm" over its input; the name copies itself when clicked. */
function TypedName({
  name,
  value,
  onChange,
}: {
  readonly name: string;
  readonly value: string;
  readonly onChange: (value: string) => void;
}): ReactElement {
  const inputId = useId();

  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={inputId} className="text-sm text-kumo-subtle">
        {"Type in "}
        <button
          type="button"
          title={`Copy ${name}`}
          onClick={() => {
            void copyText(name);
          }}
          className="cursor-copy font-medium text-kumo-default hover:underline"
        >
          {name}
        </button>
        {" to confirm"}
      </label>
      <Input
        id={inputId}
        aria-label={`Type ${name} to confirm`}
        autoComplete="off"
        spellCheck={false}
        value={value}
        onChange={(event) => {
          onChange(event.target.value);
        }}
      />
    </div>
  );
}

/**
 * A modal yes/no question as Kumo's LayerDialog alert, the dashboard's delete confirmation: a white
 * panel inside an elevated frame, 576px wide and 64px from the top, the title, the consequence in
 * subtle text, and on the frame below the panel Cancel on the left and the action on the right.
 * There is no close button; the reader has to choose. Destructive by default, because that is what
 * it is for. With `confirmName` the action waits until that name is typed in.
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel = "Confirm",
  destructive = true,
  loading = false,
  error,
  confirmName,
  onConfirm,
}: ConfirmDialogProps): ReactElement {
  const [typed, setTyped] = useState("");
  const named = confirmName === undefined || confirmName === "" || typed === confirmName;
  const failed = error !== undefined && error !== "";

  return (
    <LayerDialog.Alert
      open={open}
      onOpenChange={(next: boolean, eventDetails: unknown) => {
        if (!next) {
          setTyped("");
        }

        onOpenChange(next, eventDetails);
      }}
    >
      <LayerDialog.Content>
        <LayerDialog.Title>{title}</LayerDialog.Title>
        {/* Without a LayerDialog.Description the body is the alert's accessible description. */}
        <LayerDialog.Body>
          <div className="flex flex-col gap-4">
            {description === undefined ? null : (
              <p className="text-base text-pretty text-kumo-subtle">{description}</p>
            )}
            {failed ? <Banner variant="error" title="Request failed" description={error} /> : null}
            {confirmName === undefined ? null : (
              <TypedName name={confirmName} value={typed} onChange={setTyped} />
            )}
          </div>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="Cancel">
          <LayerDialog.Action
            variant={destructive ? "destructive" : "primary"}
            loading={loading}
            disabled={!named}
            onClick={onConfirm}
          >
            {confirmLabel}
          </LayerDialog.Action>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Alert>
  );
}

export interface DeleteDialogProps {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  /** What kind of thing goes, in lower case: "machine", "log stream". */
  readonly resourceType: string;
  /** Its name, which the reader types to confirm. */
  readonly resourceName: string;
  readonly onDelete: () => void;
  readonly isDeleting?: boolean;
  /** The action's label; "Delete {resourceType}" when not given. */
  readonly deleteButtonText?: string;
  readonly errorMessage?: string;
}

/**
 * Deleting a named resource, as the dashboard asks for it: "Delete <name>?", the consequence with
 * the name in bold, and the name typed in before the destructive action unlocks. It takes the props
 * of Kumo's `DeleteResource` block, whose look (an X, Cancel and Delete on the right) the live
 * dashboard no longer uses.
 */
export function DeleteDialog({
  open,
  onOpenChange,
  resourceType,
  resourceName,
  onDelete,
  isDeleting = false,
  deleteButtonText,
  errorMessage,
}: DeleteDialogProps): ReactElement {
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Delete ${resourceName}?`}
      description={
        <>
          {"This permanently deletes the "}
          <strong className="font-medium text-kumo-default">{resourceName}</strong>
          {` ${resourceType}. This cannot be undone.`}
        </>
      }
      confirmLabel={deleteButtonText ?? `Delete ${resourceType}`}
      loading={isDeleting}
      error={errorMessage}
      confirmName={resourceName}
      onConfirm={onDelete}
    />
  );
}
