import { Badge as KumoBadge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { ButtonGroup } from "@cloudflare/kumo/components/button-group";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { Popover } from "@cloudflare/kumo/components/popover";
import { Select } from "@cloudflare/kumo/components/select";
import { FunnelSimpleIcon, PlusIcon, XIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

/**
 * One row of a log's Filters popover: the field, how it compares, and the control that holds the
 * value. A row shows while it narrows the list, while it was just added and has no value yet, or
 * always when it is a scope every read has (the traffic pages' network).
 */
export interface LogFilter {
  readonly id: string;
  /** The field, in the console's words: "Action", "User", "Gateway". */
  readonly label: string;
  /** How the value compares, as one word or two: "is", "starts with". */
  readonly operator: string;
  /** The control that edits the value, or a read-only one for a filter picked from a row. */
  readonly value: ReactNode;
  /** Whether it narrows the list now; only these count on the button. */
  readonly active: boolean;
  /** Always shown, with no × until it leaves its default. */
  readonly permanent?: boolean;
  /** Offered under "Add filter" while it is not active. */
  readonly addable?: boolean;
  /** Puts the field back to "everything". */
  readonly onRemove: () => void;
}

/** How many filters narrow the list, for the button's count. */
export function activeFilterCount(filters: readonly LogFilter[]): number {
  return filters.filter((filter) => filter.active).length;
}

function FilterRow({
  filter,
  removable,
  onRemove,
}: {
  readonly filter: LogFilter;
  readonly removable: boolean;
  readonly onRemove: () => void;
}): ReactElement {
  return (
    // On a phone the field's name takes its own line and the operator, value and × share the next.
    <fieldset
      aria-label={`${filter.label} filter`}
      className="m-0 grid min-w-0 grid-cols-[auto_minmax(0,1fr)_2rem] items-center gap-2 border-0 p-0 sm:grid-cols-[7rem_8rem_minmax(0,1fr)_2rem]"
    >
      <span className="text-sm text-kumo-subtle max-sm:col-span-3">{filter.label}</span>
      <Select
        aria-label={`${filter.label} operator`}
        className="w-32 sm:w-full"
        value={filter.operator}
        items={[{ value: filter.operator, label: filter.operator }]}
      />
      <div className="min-w-0 [&>*]:w-full">{filter.value}</div>
      {removable ? (
        <Button
          variant="ghost"
          shape="square"
          size="sm"
          icon={XIcon}
          aria-label={`Remove the ${filter.label.toLowerCase()} filter`}
          onClick={onRemove}
        />
      ) : (
        <span />
      )}
    </fieldset>
  );
}

/**
 * The Zero Trust log toolbar's filter control: a "Filters" button with the count of filters in
 * force, joined to an × that clears them, opening a popover of field | operator | value rows with
 * "Clear all" and "Add filter". The filters themselves live in the page's address; this only lays
 * them out. A row added from the menu stays open until the popover closes, so it can be given a
 * value.
 */
export function LogFilters({
  filters,
  onClear,
}: {
  readonly filters: readonly LogFilter[];
  /** Puts every filter back to "everything". */
  readonly onClear: () => void;
}): ReactElement {
  const [pending, setPending] = useState<ReadonlySet<string>>(new Set());
  const count = activeFilterCount(filters);
  const shown = filters.filter(
    (filter) => filter.active || filter.permanent === true || pending.has(filter.id),
  );
  const addable = filters.filter(
    (filter) => filter.addable === true && !filter.active && !pending.has(filter.id),
  );
  const drop = (id: string): void => {
    const next = new Set(pending);

    next.delete(id);
    setPending(next);
  };

  return (
    <ButtonGroup aria-label="Filters" className="shrink-0">
      <Popover
        onOpenChange={(open) => {
          // Opened with nothing to show, the panel starts on the first field it offers, so the
          // first filter is a value away rather than a menu and a value.
          const [first] = addable;

          setPending(
            open && shown.length === 0 && first !== undefined ? new Set([first.id]) : new Set(),
          );
        }}
      >
        <Popover.Trigger
          render={
            <Button
              variant="secondary"
              icon={FunnelSimpleIcon}
              aria-label={count === 0 ? "Filters" : `Filters, ${String(count)} applied`}
            />
          }
        >
          Filters
          {count === 0 ? null : (
            <KumoBadge variant="primary" className="min-w-5 justify-center px-1.5 tabular-nums">
              {count}
            </KumoBadge>
          )}
        </Popover.Trigger>
        <Popover.Content
          align="end"
          className="flex w-[min(36rem,calc(100vw-2rem))] flex-col gap-3 p-4"
        >
          <div className="flex min-h-7 items-center justify-between gap-3">
            <Popover.Title className="text-sm font-semibold">Filters</Popover.Title>
            {count === 0 ? null : (
              <Button variant="secondary" size="xs" onClick={onClear}>
                Clear all
              </Button>
            )}
          </div>
          {shown.length === 0 ? (
            <p className="text-sm text-kumo-subtle">No filters. Add one to narrow the list.</p>
          ) : (
            shown.map((filter) => (
              <FilterRow
                key={filter.id}
                filter={filter}
                removable={filter.active || (filter.permanent !== true && pending.has(filter.id))}
                onRemove={() => {
                  drop(filter.id);

                  if (filter.active) {
                    filter.onRemove();
                  }
                }}
              />
            ))
          )}
          {addable.length === 0 ? null : (
            <AddFilter
              fields={addable}
              onAdd={(id) => {
                setPending(new Set([...pending, id]));
              }}
            />
          )}
        </Popover.Content>
      </Popover>
      {count === 0 ? null : (
        <Button
          variant="secondary"
          shape="square"
          icon={XIcon}
          aria-label="Clear filters"
          onClick={onClear}
        />
      )}
    </ButtonGroup>
  );
}

function AddFilter({
  fields,
  onAdd,
}: {
  readonly fields: readonly LogFilter[];
  readonly onAdd: (id: string) => void;
}): ReactElement {
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button variant="ghost" size="sm" icon={PlusIcon} className="self-start">
            Add filter
          </Button>
        }
      />
      <DropdownMenu.Content align="start">
        {fields.map((field) => (
          <DropdownMenu.Item
            key={field.id}
            onClick={() => {
              onAdd(field.id);
            }}
          >
            {field.label}
          </DropdownMenu.Item>
        ))}
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}
