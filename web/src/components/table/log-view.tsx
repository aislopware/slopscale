import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { Select } from "@cloudflare/kumo/components/select";
import { GearSixIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { tablePageSize } from "~/components/table/app-table.tsx";
import { smallestPageSize } from "~/components/table/paging.tsx";

/** The most rows a page of a log's table holds. */
const largestPageSize = 100;

/** The log toolbar's "Limit" choices: the rows one page of the table holds. */
export const logLimits: readonly number[] = [smallestPageSize, tablePageSize, largestPageSize];

/** "Limit: 50", the rows one page of a log's table holds; on a phone the number alone. */
export function LimitSelect({
  value,
  onChange,
  limits = logLimits,
}: {
  readonly value: number;
  readonly onChange: (size: number) => void;
  readonly limits?: readonly number[];
}): ReactElement {
  return (
    <Select
      aria-label="Rows per page"
      className="w-20 sm:w-28"
      value={value}
      renderValue={(size) => (
        <span>
          <span className="max-sm:hidden">Limit: </span>
          {String(size)}
        </span>
      )}
      onValueChange={(size) => {
        if (typeof size === "number") {
          onChange(size);
        }
      }}
    >
      {limits.map((size) => (
        <Select.Option key={size} value={size}>
          {size}
        </Select.Option>
      ))}
    </Select>
  );
}

/** A column a reader may switch off, as the Columns menu lists it. */
export interface ColumnChoice {
  readonly id: string;
  readonly label: string;
}

/**
 * The log toolbar's Columns chooser: one tick per column a reader may hide, in the table's order;
 * the menu stays open while ticking, and the last item puts every column back.
 */
export function ColumnsMenu({
  choices,
  columns,
}: {
  readonly choices: readonly ColumnChoice[];
  readonly columns: HiddenColumns;
}): ReactElement {
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button variant="secondary" icon={GearSixIcon} aria-label="Columns">
            <span className="max-sm:hidden">Columns</span>
          </Button>
        }
      />
      <DropdownMenu.Content align="end">
        {choices.map((choice) => (
          <DropdownMenu.CheckboxItem
            key={choice.id}
            checked={!columns.hidden.has(choice.id)}
            onCheckedChange={() => {
              columns.toggle(choice.id);
            }}
          >
            {choice.label}
          </DropdownMenu.CheckboxItem>
        ))}
        <DropdownMenu.Separator />
        <DropdownMenu.Item
          onClick={() => {
            columns.reset();
          }}
        >
          Reset columns
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}

function readHidden(key: string): ReadonlySet<string> {
  try {
    const stored: unknown = JSON.parse(localStorage.getItem(key) ?? "[]");

    return new Set(
      Array.isArray(stored) ? stored.filter((value) => typeof value === "string") : [],
    );
  } catch {
    return new Set();
  }
}

export interface HiddenColumns {
  readonly hidden: ReadonlySet<string>;
  readonly toggle: (id: string) => void;
  readonly reset: () => void;
}

/**
 * The columns a reader switched off in a log's Columns menu, kept in the browser under the given
 * key so the choice outlives the visit, the way the audit log keeps its own.
 */
export function useHiddenColumns(key: string): HiddenColumns {
  const [hidden, setHidden] = useState(() => readHidden(key));

  return {
    hidden,
    toggle: (id) => {
      const next = new Set(hidden);

      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }

      setHidden(next);
      localStorage.setItem(key, JSON.stringify([...next]));
    },
    reset: () => {
      setHidden(new Set());
      localStorage.removeItem(key);
    },
  };
}
