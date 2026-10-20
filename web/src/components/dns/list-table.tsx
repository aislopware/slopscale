import { Table } from "@cloudflare/kumo/components/table";
import { cn } from "@cloudflare/kumo/utils";
import { createContext, use } from "react";
import type { ReactElement, ReactNode } from "react";

import { TableScroll } from "~/components/table/scroll-panel.tsx";
import {
  actionsColumnClass,
  pinnedEdgeClass,
  tableClass,
  tableRowClass,
} from "~/components/table/styles.ts";

export interface ListColumn {
  readonly key: string;
  /** The heading; empty for the actions column, which carries a hidden name instead. */
  readonly label: string;
  readonly className?: string;
}

/** Whether the columns run past the card, so the pinned actions draw their 16px fade. */
const OverflowContext = createContext(false);

/** The pinned actions column's classes: its fade only while something is scrolled behind it. */
function pinnedClass(overflowing: boolean): string {
  return cn(actionsColumnClass, overflowing ? pinnedEdgeClass : "before:hidden");
}

/**
 * A short editable list as the dashboard's DataTable draws one: the canvas header row with 13px
 * subtle headings, 44px rows and the ⋯ menu in a 50px column pinned to the right edge, with the
 * 16px fade every other console table has, so on a phone the columns scroll under the menu rather
 * than taking it off screen. It is for the handful of entries a DNS or relay setting holds, which
 * need no sorting or paging, so it renders rows the caller builds rather than a `useAppTable`
 * model. The empty state takes the place of the rows.
 */
export function ListTable({
  columns,
  actions = false,
  empty,
  children,
}: {
  readonly columns: readonly ListColumn[];
  /** Whether the rows end in a ⋯ menu, which gets the pinned 50px column. */
  readonly actions?: boolean;
  /** Rendered under the header instead of the rows; null while there are rows. */
  readonly empty: ReactNode;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <TableScroll below={empty} pinnedRight={actions}>
      {(overflowing) => (
        <OverflowContext value={overflowing}>
          <Table className={tableClass}>
            <Table.Header>
              <Table.Row>
                {columns.map((column) => (
                  <Table.Head key={column.key} className={column.className}>
                    {column.label}
                  </Table.Head>
                ))}
                {actions ? (
                  <Table.Head sticky="right" className={pinnedClass(overflowing)}>
                    <span className="sr-only">Actions</span>
                  </Table.Head>
                ) : null}
              </Table.Row>
            </Table.Header>
            {empty === null ? <Table.Body>{children}</Table.Body> : null}
          </Table>
        </OverflowContext>
      )}
    </TableScroll>
  );
}

/**
 * Nameserver addresses in a cell: the line breaks between two addresses, never inside one, so an
 * IPv6 address or a DoH URL stays one run to read and copy. The column is as wide as its widest
 * address at least.
 */
export function AddressList({
  addresses,
}: {
  readonly addresses: readonly string[];
}): ReactElement {
  return (
    <span className="flex flex-wrap gap-x-2 gap-y-0.5">
      {addresses.map((address, index) => (
        <span key={address} className="whitespace-nowrap">
          {address}
          {index < addresses.length - 1 ? "," : ""}
        </span>
      ))}
    </span>
  );
}

/** A body row of a `ListTable`. */
export function ListRow({
  children,
  className,
}: {
  readonly children: ReactNode;
  readonly className?: string;
}): ReactElement {
  return <Table.Row className={cn(tableRowClass, className)}>{children}</Table.Row>;
}

/** A cell of a `ListRow`; `actions` is the ⋯ menu's column, pinned to the right edge. */
export function ListCell({
  children,
  className,
  actions = false,
}: {
  readonly children?: ReactNode;
  readonly className?: string;
  readonly actions?: boolean;
}): ReactElement {
  const overflowing = use(OverflowContext);

  return actions ? (
    <Table.Cell sticky="right" className={cn(pinnedClass(overflowing), className)}>
      {children}
    </Table.Cell>
  ) : (
    <Table.Cell className={className}>{children}</Table.Cell>
  );
}
