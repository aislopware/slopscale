import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { ColumnsIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

/** The audit columns a reader may hide. Time and Action name the event, so they always show. */
export const optionalAuditColumns = ["target", "actor", "result", "details"] as const;

export type AuditColumn = (typeof optionalAuditColumns)[number];

const columnLabels: Record<AuditColumn, string> = {
  target: "Target",
  actor: "Actor",
  result: "Result",
  details: "Details",
};

const storageKey = "slopscale-admin.audit.hidden-columns";

function isColumn(value: unknown): value is AuditColumn {
  return (optionalAuditColumns as readonly unknown[]).includes(value);
}

function readHidden(): ReadonlySet<AuditColumn> {
  try {
    const stored: unknown = JSON.parse(localStorage.getItem(storageKey) ?? "[]");

    return new Set(Array.isArray(stored) ? stored.filter((value) => isColumn(value)) : []);
  } catch {
    return new Set();
  }
}

/** The hidden columns, kept in the browser so the choice outlives the visit. */
export function useHiddenColumns(): [ReadonlySet<AuditColumn>, (column: AuditColumn) => void] {
  const [hidden, setHidden] = useState(readHidden);

  function toggle(column: AuditColumn): void {
    const next = new Set(hidden);

    if (next.has(column)) {
      next.delete(column);
    } else {
      next.add(column);
    }

    setHidden(next);
    localStorage.setItem(storageKey, JSON.stringify([...next]));
  }

  return [hidden, toggle];
}

/** The log toolbar's Columns chooser: one tick per optional column, the menu stays open. */
export function ColumnsMenu({
  hidden,
  onToggle,
}: {
  readonly hidden: ReadonlySet<AuditColumn>;
  readonly onToggle: (column: AuditColumn) => void;
}): ReactElement {
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button variant="secondary" icon={ColumnsIcon}>
            Columns
          </Button>
        }
      />
      <DropdownMenu.Content align="end">
        {optionalAuditColumns.map((column) => (
          <DropdownMenu.CheckboxItem
            key={column}
            checked={!hidden.has(column)}
            onCheckedChange={() => {
              onToggle(column);
            }}
          >
            {columnLabels[column]}
          </DropdownMenu.CheckboxItem>
        ))}
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}
