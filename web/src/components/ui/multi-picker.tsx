import { Badge } from "@cloudflare/kumo/components/badge";
import { Combobox } from "@cloudflare/kumo/components/combobox";
import { Field } from "@cloudflare/kumo/components/field";
import type { ReactElement, ReactNode } from "react";

export interface PickerItem {
  readonly value: string;
  readonly label: string;
  /** A second, muted line: a username under a display name, a count under a group. */
  readonly hint?: string;
}

/**
 * A multi-select over a fixed list, on Kumo's combobox: chosen items sit as chips in the field, the
 * rest filter as the operator types. Values are ids; the items give them names.
 */
export function MultiPicker({
  label,
  description,
  placeholder,
  items,
  value,
  onValueChange,
  disabled = false,
  empty = "Nothing matches.",
}: {
  readonly label: ReactNode;
  readonly description?: ReactNode;
  readonly placeholder: string;
  readonly items: readonly PickerItem[];
  readonly value: readonly string[];
  readonly onValueChange: (value: string[]) => void;
  readonly disabled?: boolean;
  readonly empty?: string;
}): ReactElement {
  // An id with no item (a record the caller may not list) is kept as a chip so that saving
  // the picker never drops it silently.
  const selected = value.map(
    (id) => items.find((item) => item.value === id) ?? { value: id, label: `#${id}` },
  );

  // A picker nobody may edit shows its members as plain badges: a disabled combobox would still
  // draw removable-looking chips and an input that invites typing.
  if (disabled) {
    return (
      <Field label={label} hideLabel {...(description === undefined ? {} : { description })}>
        <ul
          aria-label={typeof label === "string" ? label : undefined}
          className="flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border border-kumo-line bg-kumo-recessed px-2 py-1.5"
        >
          {selected.length === 0 ? (
            <li className="text-sm text-kumo-subtle">{placeholder}</li>
          ) : (
            selected.map((item) => (
              <li key={item.value}>
                <Badge variant="outline">{item.label}</Badge>
              </li>
            ))
          )}
        </ul>
      </Field>
    );
  }

  return (
    <Combobox<PickerItem, true>
      multiple
      items={[...items]}
      value={selected}
      disabled={disabled}
      label={label}
      {...(description === undefined ? {} : { description })}
      isItemEqualToValue={(item, chosen) => item.value === chosen.value}
      itemToStringLabel={(item) => item.label}
      onValueChange={(next) => {
        onValueChange(next.map((item) => item.value));
      }}
    >
      <Combobox.TriggerMultipleWithInput
        className="w-full"
        placeholder={selected.length === 0 ? placeholder : ""}
        inputSide="right"
        renderItem={(item: PickerItem) => (
          <Combobox.Chip key={item.value} removeLabel={`Remove ${item.label}`}>
            {item.label}
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
                {item.hint === undefined ? null : (
                  <span className="truncate text-xs text-kumo-subtle">{item.hint}</span>
                )}
              </span>
            </Combobox.Item>
          )}
        </Combobox.List>
      </Combobox.Content>
    </Combobox>
  );
}
