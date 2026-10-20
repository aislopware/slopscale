import { Combobox } from "@cloudflare/kumo/components/combobox";
import type { ReactElement, ReactNode } from "react";

import type { PickerItem } from "~/components/ui/multi-picker.tsx";

/**
 * The value of a builder condition: the chosen items as chips in one field, the rest filtering as
 * the operator types, like the value input of the dashboard's rule builders. Unlike the form picker
 * it carries an inline error, so a condition left empty says so under its own row.
 */
export function ConditionPicker({
  label,
  placeholder,
  items,
  value,
  onValueChange,
  error,
  empty = "Nothing matches.",
}: {
  readonly label: ReactNode;
  readonly placeholder: string;
  readonly items: readonly PickerItem[];
  readonly value: readonly string[];
  readonly onValueChange: (value: string[]) => void;
  readonly error?: string | undefined;
  readonly empty?: string;
}): ReactElement {
  // An id with no item (a record the caller may not list) stays a chip, so a save never drops it.
  const selected = value.map(
    (id) => items.find((item) => item.value === id) ?? { value: id, label: `#${id}` },
  );

  return (
    // Enter picks the highlighted item; with nothing to pick it would submit the whole form, which a
    // half-typed name never means, so the key stops here. Escape on a closed list would clear every
    // chip in the field, which reads as "never mind" to nobody, so it only ever closes the list.
    <div
      role="presentation"
      className="contents"
      onKeyDownCapture={(event) => {
        if (
          event.key === "Escape" &&
          event.target instanceof HTMLInputElement &&
          event.target.getAttribute("aria-expanded") !== "true"
        ) {
          event.stopPropagation();
        }
      }}
      onKeyDown={(event) => {
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
        label={label}
        {...(error === undefined ? {} : { error })}
        isItemEqualToValue={(item, chosen) => item.value === chosen.value}
        itemToStringLabel={(item) => item.label}
        onValueChange={(next) => {
          onValueChange(next.map((item) => item.value));
        }}
      >
        <Combobox.TriggerMultipleWithInput
          className="w-full bg-kumo-control"
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
