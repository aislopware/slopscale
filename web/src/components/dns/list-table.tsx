import { Table } from "@cloudflare/kumo/components/table";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import { TableScroll } from "~/components/table/scroll-panel.tsx";
import { actionsColumnClass, tableClass, tableRowClass } from "~/components/table/styles.ts";

export interface ListColumn {
  readonly key: string;
  /** The heading; empty for the actions column, which carries a hidden name instead. */
  readonly label: string;
  readonly className?: string;
}

/**
 * A short editable list as the dashboard's DataTable draws one: the canvas header row with 13px
 * subtle headings, 44px rows and the ⋯ menu in a 50px column at the end. It is for the handful of
 * entries a DNS or relay setting holds, which need no sorting or paging, so it renders rows the
 * caller builds rather than a `useAppTable` model. The empty state takes the place of the rows.
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
    <TableScroll below={empty}>
      {() => (
        <Table className={tableClass}>
          <Table.Header>
            <Table.Row>
              {columns.map((column) => (
                <Table.Head key={column.key} className={column.className}>
                  {column.label}
                </Table.Head>
              ))}
              {actions ? (
                <Table.Head className={actionsColumnClass}>
                  <span className="sr-only">Actions</span>
                </Table.Head>
              ) : null}
            </Table.Row>
          </Table.Header>
          {empty === null ? <Table.Body>{children}</Table.Body> : null}
        </Table>
      )}
    </TableScroll>
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

/** A cell of a `ListRow`; `actions` is the ⋯ menu's column. */
export function ListCell({
  children,
  className,
  actions = false,
}: {
  readonly children?: ReactNode;
  readonly className?: string;
  readonly actions?: boolean;
}): ReactElement {
  return (
    <Table.Cell className={cn(actions ? actionsColumnClass : undefined, className)}>
      {children}
    </Table.Cell>
  );
}
