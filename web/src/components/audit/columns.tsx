import { useState } from "react";
import type { ReactElement } from "react";

import { ColumnsMenu as LogColumnsMenu } from "~/components/table/log-view.tsx";

/** Every audit column, in the order the table starts with. */
export const auditColumns = ["time", "action", "target", "actor", "result", "details"] as const;

export type AuditColumnId = (typeof auditColumns)[number];

/** The audit columns a reader may hide. Time and Action name the event, so they always show. */
export const optionalAuditColumns = ["target", "actor", "result", "details"] as const;

export type AuditColumn = (typeof optionalAuditColumns)[number];

export const auditColumnLabels: Record<AuditColumnId, string> = {
  time: "Time",
  action: "Action",
  target: "Target",
  actor: "Actor",
  result: "Result",
  details: "Details",
};

const hiddenKey = "slopscale-admin.audit.hidden-columns";
const orderKey = "slopscale-admin.audit.column-order";

function isColumn(value: unknown): value is AuditColumn {
  return (optionalAuditColumns as readonly unknown[]).includes(value);
}

function isColumnId(value: unknown): value is AuditColumnId {
  return (auditColumns as readonly unknown[]).includes(value);
}

function readStored(key: string): unknown[] {
  try {
    const stored: unknown = JSON.parse(localStorage.getItem(key) ?? "[]");

    return Array.isArray(stored) ? stored : [];
  } catch {
    return [];
  }
}

function readHidden(): ReadonlySet<AuditColumn> {
  return new Set(readStored(hiddenKey).filter((value) => isColumn(value)));
}

/**
 * The stored order, kept only as far as it names real columns once each; a column the stored order
 * does not know (one added since) goes where the default order puts it, at the end.
 */
export function readOrder(stored: readonly unknown[] = readStored(orderKey)): AuditColumnId[] {
  const known = [...new Set(stored.filter((value) => isColumnId(value)))];

  return [...known, ...auditColumns.filter((column) => !known.includes(column))];
}

/** The column moved to the place another one held, the rest keeping their order. */
export function moveColumn(
  order: readonly AuditColumnId[],
  column: AuditColumnId,
  to: number,
): AuditColumnId[] {
  const rest = order.filter((candidate) => candidate !== column);
  const at = Math.max(0, Math.min(to, rest.length));

  return [...rest.slice(0, at), column, ...rest.slice(at)];
}

export interface AuditColumns {
  readonly order: readonly AuditColumnId[];
  readonly hidden: ReadonlySet<AuditColumn>;
  readonly toggle: (column: AuditColumn) => void;
  /** Puts the column at this index of the full order. */
  readonly move: (column: AuditColumnId, to: number) => void;
  readonly reset: () => void;
}

/** Which columns show and in what order, kept in the browser so the choice outlives the visit. */
export function useAuditColumns(): AuditColumns {
  const [hidden, setHidden] = useState(readHidden);
  const [order, setOrder] = useState(() => readOrder());

  return {
    order,
    hidden,
    toggle: (column) => {
      const next = new Set(hidden);

      if (next.has(column)) {
        next.delete(column);
      } else {
        next.add(column);
      }

      setHidden(next);
      localStorage.setItem(hiddenKey, JSON.stringify([...next]));
    },
    move: (column, to) => {
      const next = moveColumn(order, column, to);

      setOrder(next);
      localStorage.setItem(orderKey, JSON.stringify(next));
    },
    reset: () => {
      setHidden(new Set());
      setOrder([...auditColumns]);
      localStorage.removeItem(hiddenKey);
      localStorage.removeItem(orderKey);
    },
  };
}

/** The log toolbar's Columns chooser for the audit log, in the table's current order. */
export function ColumnsMenu({ columns }: { readonly columns: AuditColumns }): ReactElement {
  return (
    <LogColumnsMenu
      choices={columns.order
        .filter((column) => isColumn(column))
        .map((column) => ({ id: column, label: auditColumnLabels[column] }))}
      columns={{
        hidden: columns.hidden,
        toggle: (id) => {
          if (isColumn(id)) {
            columns.toggle(id);
          }
        },
        reset: columns.reset,
      }}
    />
  );
}
