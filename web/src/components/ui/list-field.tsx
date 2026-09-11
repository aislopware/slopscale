import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { PlusIcon, XIcon } from "@phosphor-icons/react";
import { useEffect, useRef, useState } from "react";
import type { KeyboardEvent, ReactElement, ReactNode, Ref } from "react";

export interface ListFieldProps {
  readonly label: string;
  readonly description?: ReactNode;
  /** What one value looks like, shown in an empty row. */
  readonly placeholder: string;
  /** The button that adds a row: "Add domain". */
  readonly addLabel: string;
  /**
   * The rows as typed, blank ones included; `listValues` in `~/lib/list.ts` turns them into what a
   * request wants and `listError` says whether one of them is wrong.
   */
  readonly value: readonly string[];
  readonly onValueChange: (rows: string[]) => void;
  /** Tidies a row once the operator leaves it: lower-casing a domain, prefixing `tag:`. */
  readonly normalize?: (row: string) => string;
  /** Why a row is wrong, or null when it is not; shown under the row once it has been left. */
  readonly validate?: (row: string) => string | null;
  /** Marks the field optional beside its label, the way Kumo's Field does. */
  readonly optional?: boolean;
  readonly disabled?: boolean;
}

const asIs = (row: string): string => row;
const noIssue = (): null => null;

/**
 * A list the operator edits one row at a time: every value is an input of its own with a remove
 * button beside it, and a button under the rows adds one. Rows can be read whole, edited in place
 * and removed without hunting for the X on a chip. Enter in a valid row adds the next one and
 * Backspace in an empty row takes it away, so a list can be typed without reaching for the mouse.
 */
export function ListField({
  label,
  description,
  placeholder,
  addLabel,
  value,
  onValueChange,
  normalize = asIs,
  validate = noIssue,
  optional = false,
  disabled = false,
}: ListFieldProps): ReactElement {
  const inputs = useRef<(HTMLInputElement | null)[]>([]);
  // Which row to focus once it exists; a row added by Enter or the button is typed into at once.
  const pendingFocus = useRef<number | null>(null);
  // Rows the operator has left once: a mistake shows then, not while the row is still being typed.
  const [touched, setTouched] = useState<ReadonlySet<number>>(new Set());

  useEffect(() => {
    const at = pendingFocus.current;

    if (at !== null) {
      pendingFocus.current = null;
      inputs.current[at]?.focus();
    }
  });

  function setRow(index: number, text: string): void {
    onValueChange(value.map((row, at) => (at === index ? text : row)));
  }

  function insertAfter(index: number): void {
    pendingFocus.current = index + 1;
    onValueChange([...value.slice(0, index + 1), "", ...value.slice(index + 1)]);
  }

  function remove(index: number): void {
    setTouched(
      new Set([...touched].filter((at) => at !== index).map((at) => (at > index ? at - 1 : at))),
    );
    onValueChange(value.filter((_, at) => at !== index));
  }

  function leave(index: number): void {
    setTouched(new Set([...touched, index]));

    const row = value[index] ?? "";
    const tidy = row.trim() === "" ? row : normalize(row.trim());

    if (tidy !== row) {
      setRow(index, tidy);
    }
  }

  function issueOf(index: number): string | null {
    const row = (value[index] ?? "").trim();

    return touched.has(index) && row !== "" ? validate(row) : null;
  }

  function onKeyDown(index: number, event: KeyboardEvent<HTMLInputElement>): void {
    const row = (value[index] ?? "").trim();

    if (event.key === "Enter") {
      // The form's own submit is the button on the band; Enter here works on the list.
      event.preventDefault();
      leave(index);

      if (row !== "" && validate(row) === null) {
        insertAfter(index);
      }

      return;
    }

    if (event.key === "Backspace" && row === "" && value.length > 0) {
      event.preventDefault();
      pendingFocus.current = Math.max(index - 1, 0);
      remove(index);
    }
  }

  return (
    // A fieldset's own minimum width is its content's, which is what let a long value push a
    // dialog wider than its frame.
    <fieldset disabled={disabled} className="flex min-w-0 flex-col gap-1.5">
      <legend className="mb-1.5 text-base font-medium text-kumo-default select-none">
        {label}
        {optional ? <span className="font-normal text-kumo-subtle"> (optional)</span> : null}
      </legend>
      <div className="flex flex-col gap-2">
        {value.map((row, index) => (
          <ListRow
            // Rows are keyed by position: a removed row hands its input to the one after it,
            // which is what the operator sees happen.
            // eslint-disable-next-line react/no-array-index-key
            key={index}
            inputRef={(node) => {
              inputs.current[index] = node;
            }}
            name={`${label} ${index + 1}`}
            row={row}
            issue={issueOf(index)}
            placeholder={placeholder}
            onChange={(text) => {
              setRow(index, text);
            }}
            onKeyDown={(event) => {
              onKeyDown(index, event);
            }}
            onBlur={() => {
              leave(index);
            }}
            onRemove={() => {
              remove(index);
            }}
          />
        ))}
        <Button
          variant="secondary"
          size="sm"
          icon={PlusIcon}
          className="self-start"
          onClick={() => {
            insertAfter(value.length - 1);
          }}
        >
          {addLabel}
        </Button>
      </div>
      {description === undefined ? null : (
        <p className="text-sm leading-snug text-kumo-subtle">{description}</p>
      )}
    </fieldset>
  );
}

function ListRow({
  inputRef,
  name,
  row,
  issue,
  placeholder,
  onChange,
  onKeyDown,
  onBlur,
  onRemove,
}: {
  readonly inputRef: Ref<HTMLInputElement>;
  readonly name: string;
  readonly row: string;
  readonly issue: string | null;
  readonly placeholder: string;
  readonly onChange: (text: string) => void;
  readonly onKeyDown: (event: KeyboardEvent<HTMLInputElement>) => void;
  readonly onBlur: () => void;
  readonly onRemove: () => void;
}): ReactElement {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center gap-2">
        <Input
          ref={inputRef}
          aria-label={name}
          aria-invalid={issue === null ? undefined : true}
          variant={issue === null ? "default" : "error"}
          className="min-w-0 flex-1"
          value={row}
          placeholder={placeholder}
          spellCheck={false}
          autoComplete="off"
          onChange={(event) => {
            onChange(event.target.value);
          }}
          onKeyDown={onKeyDown}
          onBlur={onBlur}
        />
        <Button
          variant="ghost"
          shape="square"
          size="sm"
          icon={XIcon}
          aria-label={`Remove ${row.trim() === "" ? "empty row" : row}`}
          onClick={onRemove}
        />
      </div>
      {issue === null ? null : <p className="text-sm leading-snug text-kumo-danger">{issue}</p>}
    </div>
  );
}
