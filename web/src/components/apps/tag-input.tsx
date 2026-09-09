import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Field } from "@cloudflare/kumo/components/field";
import { Field as FieldPart } from "@cloudflare/kumo/primitives/field";
import { XIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { KeyboardEvent, ReactElement, ReactNode } from "react";

/** What ends an entry: the operator hits Enter, or types a comma or a space. */
const separators = new Set([",", " "]);

export interface TagInputProps {
  readonly label: ReactNode;
  readonly description?: ReactNode;
  readonly placeholder: string;
  readonly value: readonly string[];
  readonly onValueChange: (value: string[]) => void;
  /** Cleans an entry before it becomes a chip: lower-casing a domain, prefixing `tag:`. */
  readonly normalize?: (entry: string) => string;
  /** Why the entry cannot be added, or null when it can. */
  readonly validate?: (entry: string) => string | null;
  /**
   * Told whether the field holds text that is not a chip yet, so the form can hold its submit until
   * the entry is committed or cleared rather than silently dropping it.
   */
  readonly onPendingChange?: (pending: boolean) => void;
  readonly disabled?: boolean;
}

export interface PendingLists {
  /** Whether any tracked list holds text that is not a chip yet. */
  readonly pending: boolean;
  /** The `onPendingChange` handler for the list of that name. */
  readonly track: (list: string) => (pending: boolean) => void;
}

/**
 * Tracks which of a form's tag inputs hold text typed but not yet turned into a chip, so the form
 * holds its submit rather than silently dropping the entry.
 */
export function usePendingLists(): PendingLists {
  const [lists, setLists] = useState<ReadonlySet<string>>(new Set());

  return {
    pending: lists.size > 0,
    track: (list) => (pending) => {
      setLists((current) => {
        if (current.has(list) === pending) {
          return current;
        }

        const next = new Set(current);

        if (pending) {
          next.add(list);
        } else {
          next.delete(list);
        }

        return next;
      });
    },
  };
}

/**
 * A list the operator types one value at a time: each entry becomes a chip, and the field says at
 * once why one was refused rather than failing the whole form on submit. Enter, a comma or a space
 * ends an entry; Backspace on an empty field takes the last chip back.
 */
export function TagInput({
  label,
  description,
  placeholder,
  value,
  onValueChange,
  normalize,
  validate,
  onPendingChange,
  disabled = false,
}: TagInputProps): ReactElement {
  const [text, setText] = useState("");
  const [issue, setIssue] = useState<string | null>(null);

  function setEntry(entry: string): void {
    setText(entry);
    onPendingChange?.(entry.trim() !== "");
  }

  function commit(entry: string): void {
    const trimmed = entry.trim();

    if (trimmed === "") {
      setEntry("");

      return;
    }

    const problem = validate?.(trimmed) ?? null;

    if (problem !== null) {
      setIssue(problem);

      return;
    }

    const tag = normalize?.(trimmed) ?? trimmed;

    setIssue(null);
    setEntry("");

    if (!value.includes(tag)) {
      onValueChange([...value, tag]);
    }
  }

  function remove(tag: string): void {
    setIssue(null);
    onValueChange(value.filter((entry) => entry !== tag));
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>): void {
    if (event.key === "Enter" || separators.has(event.key)) {
      event.preventDefault();
      commit(text);

      return;
    }

    if (event.key === "Backspace" && text === "" && value.length > 0) {
      remove(value.at(-1) ?? "");
    }
  }

  return (
    <Field
      label={label}
      {...(description === undefined ? {} : { description })}
      {...(issue === null ? {} : { error: { message: issue, match: true as const } })}
    >
      {/* The Field labels the control inside this, so the box is a plain surface holding the
          chips. */}
      <div className="flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border border-kumo-line bg-kumo-base px-2 py-1.5 focus-within:ring-2 focus-within:ring-kumo-focus">
        {value.map((tag) => (
          <Badge key={tag} variant="outline" className="max-w-full gap-1 pr-1">
            <span className="truncate">{tag}</span>
            <Button
              variant="ghost"
              shape="square"
              size="xs"
              icon={XIcon}
              aria-label={`Remove ${tag}`}
              disabled={disabled}
              onClick={() => {
                remove(tag);
              }}
            />
          </Badge>
        ))}
        {/* Base UI's control: the Field's label, description and error are its own, not a sibling
            the screen reader has to guess at. */}
        <FieldPart.Control
          type="text"
          value={text}
          disabled={disabled}
          placeholder={value.length === 0 ? placeholder : ""}
          spellCheck={false}
          autoComplete="off"
          aria-invalid={issue === null ? undefined : true}
          className="min-w-32 flex-1 bg-transparent text-base text-kumo-default outline-none placeholder:text-kumo-subtle"
          onChange={(event) => {
            setIssue(null);
            setEntry(event.target.value);
          }}
          onKeyDown={onKeyDown}
          onBlur={() => {
            commit(text);
          }}
        />
      </div>
    </Field>
  );
}
