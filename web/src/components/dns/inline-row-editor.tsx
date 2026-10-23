import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { Table } from "@cloudflare/kumo/components/table";
import { TrashIcon } from "@phosphor-icons/react";
import { useEffect, useRef } from "react";
import type { ReactElement, ReactNode, SubmitEvent } from "react";

const textFieldSelector = "input:not([aria-hidden='true']):not([type='hidden']), textarea";

/** The id of a row's Edit button, so closing its editor can hand the focus back to it. */
export function editToggleId(table: string, key: string): string {
  return `${table}-edit-${key}`;
}

/** Puts the focus back on the row's Edit button once its editor is gone. */
export function focusEditToggle(id: string): void {
  requestAnimationFrame(() => {
    document.querySelector<HTMLElement>(`#${CSS.escape(id)}`)?.focus();
  });
}

/**
 * The row's Edit button, "Close" while its editor is open, as the dashboard's DNS records have it
 * in place of the ⋯ menu.
 */
export function EditToggle({
  id,
  open,
  label,
  disabled = false,
  onToggle,
}: {
  readonly id: string;
  readonly open: boolean;
  /** What the row is, for the button's accessible name: "Edit corp.example". */
  readonly label: string;
  readonly disabled?: boolean;
  readonly onToggle: () => void;
}): ReactElement {
  return (
    <Button
      id={id}
      variant="ghost"
      size="sm"
      aria-expanded={open}
      aria-label={`${open ? "Close" : "Edit"} ${label}`}
      disabled={disabled}
      onClick={onToggle}
    >
      {open ? "Close" : "Edit"}
    </Button>
  );
}

/**
 * Editing one row of a small table in place, as the dashboard edits a DNS record: the row stays,
 * and a panel opens under it with a brand line down its left edge, the fields on one line where
 * they fit, and Save, Cancel and Delete under a hairline. Save waits for a change; Escape cancels.
 * It is a row of its own whose one cell spans the table.
 */
export function InlineRowEditor({
  columns,
  label,
  dirty,
  pending,
  error,
  onSave,
  onCancel,
  onDelete,
  children,
}: {
  /** How many columns the cell spans: every column of the table. */
  readonly columns: number;
  /** The form's accessible name: "Edit corp.example". */
  readonly label: string;
  readonly dirty: boolean;
  readonly pending: boolean;
  /** The server's refusal of the last save, if it refused. */
  readonly error: string | undefined;
  readonly onSave: () => void;
  readonly onCancel: () => void;
  readonly onDelete: () => void;
  readonly children: ReactNode;
}): ReactElement {
  const form = useRef<HTMLFormElement>(null);

  useEffect(() => {
    // The first text field: a select keeps a hidden input of its own, which takes no focus.
    form.current?.querySelector<HTMLElement>(textFieldSelector)?.focus();
  }, []);

  // Escape cancels. The listener is on the form's own element, so the Escape that closes a
  // select's list, which is portalled out of the form, never reaches it.
  useEffect(() => {
    const node = form.current;
    const keyDown = (event: KeyboardEvent): void => {
      if (event.key === "Escape" && !event.defaultPrevented) {
        event.preventDefault();
        onCancel();
      }
    };

    node?.addEventListener("keydown", keyDown);

    return (): void => {
      node?.removeEventListener("keydown", keyDown);
    };
  }, [onCancel]);

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    if (dirty && !pending) {
      onSave();
    }
  }

  return (
    <Table.Row className="bg-kumo-base even:bg-kumo-base">
      <Table.Cell colSpan={columns} className="h-auto! p-0!">
        <form
          ref={form}
          aria-label={label}
          noValidate
          onSubmit={submit}
          className="flex flex-col gap-3 border-l-3 border-kumo-brand bg-kumo-elevated p-4"
        >
          {children}
          {error === undefined ? null : (
            <Banner variant="error" title="Not saved" description={error} />
          )}
          <div className="flex flex-wrap items-center gap-2 border-t border-kumo-line pt-3">
            <Button type="submit" variant="primary" disabled={!dirty} loading={pending}>
              Save
            </Button>
            <Button variant="secondary" onClick={onCancel}>
              Cancel
            </Button>
            <Button
              variant="secondary-destructive"
              icon={TrashIcon}
              disabled={pending}
              onClick={onDelete}
            >
              Delete
            </Button>
          </div>
        </form>
      </Table.Cell>
    </Table.Row>
  );
}
