import { Combobox } from "@cloudflare/kumo/components/combobox";
import { Field } from "@cloudflare/kumo/components/field";
import { cn } from "@cloudflare/kumo/utils";
import { useLayoutEffect, useRef } from "react";
import type { ReactElement, ReactNode } from "react";

export interface PickerItem {
  readonly value: string;
  readonly label: string;
  /** A second, muted line: a username under a display name, a count under a group. */
  readonly hint?: string;
  /** What a chosen item's chip says, where the label alone is ambiguous: a machine's owner. */
  readonly chip?: string;
}

/**
 * A multi-select over a fixed list, on Kumo's combobox: chosen items sit as chips in the field, the
 * rest filter as the operator types, the first match highlighted so Enter takes it. Values are ids;
 * the items give them names.
 *
 * Three things Base UI's combobox does that a form does not want are undone here. Escape on a
 * closed list clears every chip, which reads as "never mind" to nobody, so that change is refused
 * and the key goes on to close whatever the field sits in. Enter with nothing to pick would submit
 * the whole form, which a half-typed name never means, so the key stops at the field. And Kumo
 * shows an error's message without marking the input invalid, so `aria-invalid` is set on it here,
 * for screen readers and for a form that moves to its first invalid field.
 */
export function MultiPicker({
  label,
  description,
  placeholder,
  items,
  value,
  onValueChange,
  disabled = false,
  empty = "Nothing matches",
  error,
  triggerClassName,
}: {
  readonly label: ReactNode;
  readonly description?: ReactNode;
  readonly placeholder: string;
  readonly items: readonly PickerItem[];
  readonly value: readonly string[];
  readonly onValueChange: (value: string[]) => void;
  readonly disabled?: boolean;
  readonly empty?: string;
  /** What is wrong with the choice, under the field in the danger colour. */
  readonly error?: string | undefined;
  /** Classes for the chip field, such as the control fill inside a rule builder's tinted row. */
  readonly triggerClassName?: string;
}): ReactElement {
  const field = useRef<HTMLDivElement>(null);
  const invalid = error !== undefined && error !== "";

  useLayoutEffect(() => {
    const input = field.current?.querySelector("input");

    if (input === null || input === undefined) {
      return;
    }

    if (invalid) {
      input.setAttribute("aria-invalid", "true");
    } else {
      input.removeAttribute("aria-invalid");
    }
  });

  // An id with no item (a record the caller may not list) is kept as a chip so that saving
  // the picker never drops it silently.
  const selected = value.map(
    (id) => items.find((item) => item.value === id) ?? { value: id, label: `#${id}` },
  );

  // A picker nobody may edit shows its members as plain text: a disabled combobox would still
  // draw removable-looking chips and an input that invites typing.
  if (disabled) {
    return (
      <Field label={label} {...(description === undefined ? {} : { description })}>
        <ul
          aria-label={typeof label === "string" ? label : undefined}
          className="flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border border-kumo-line bg-kumo-recessed px-2 py-1.5"
        >
          {selected.length === 0 ? (
            <li className="text-sm text-kumo-subtle">{placeholder}</li>
          ) : (
            selected.map((item) => (
              <li key={item.value} className="text-sm text-kumo-default">
                {item.chip ?? item.label}
              </li>
            ))
          )}
        </ul>
      </Field>
    );
  }

  return (
    <div
      ref={field}
      role="presentation"
      className="contents"
      onKeyDown={(event) => {
        // The combobox has already picked the highlighted item by the time the key gets here.
        if (event.key === "Enter" && event.target instanceof HTMLInputElement) {
          event.preventDefault();
        }
      }}
    >
      <Combobox<PickerItem, true>
        multiple
        autoHighlight
        items={[...items]}
        value={selected}
        disabled={disabled}
        label={label}
        {...(description === undefined ? {} : { description })}
        {...(invalid ? { error } : {})}
        isItemEqualToValue={(item, chosen) => item.value === chosen.value}
        itemToStringLabel={(item) => item.label}
        onValueChange={(next, details) => {
          if (details.reason === "escape-key") {
            details.allowPropagation();

            return;
          }

          onValueChange(next.map((item) => item.value));
        }}
      >
        <Combobox.TriggerMultipleWithInput
          className={cn("w-full", triggerClassName)}
          placeholder={selected.length === 0 ? placeholder : ""}
          inputSide="right"
          renderItem={(item: PickerItem) => (
            <Combobox.Chip key={item.value} removeLabel={`Remove ${item.chip ?? item.label}`}>
              {item.chip ?? item.label}
            </Combobox.Chip>
          )}
        />
        <Combobox.Content className="max-h-64 overflow-y-auto">
          <Combobox.Empty>{empty}</Combobox.Empty>
          <Combobox.List>
            {(item: PickerItem) => (
              <Combobox.Item key={item.value} value={item}>
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="truncate">{item.label}</span>
                  {item.hint === undefined || item.hint === "" ? null : (
                    <span className="truncate text-xs text-kumo-subtle">{item.hint}</span>
                  )}
                </span>
              </Combobox.Item>
            )}
          </Combobox.List>
        </Combobox.Content>
      </Combobox>
    </div>
  );
}
