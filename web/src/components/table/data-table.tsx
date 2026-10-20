import { Table } from "@cloudflare/kumo/components/table";
import { cn } from "@cloudflare/kumo/utils";
import { CaretDownIcon, CaretUpDownIcon, CaretUpIcon } from "@phosphor-icons/react";
import type { RowData, SortDirection } from "@tanstack/react-table";
import { useEffect, useRef } from "react";
import type { ReactElement, ReactNode } from "react";

import type { AppTableInstance } from "~/components/table/app-table.tsx";
import { PagingBand } from "~/components/table/paging.tsx";
import { TableScroll } from "~/components/table/scroll-panel.tsx";
import {
  hideBelowClasses,
  pinnedEdgeClass,
  tableClass,
  tableRowClass,
} from "~/components/table/styles.ts";
import type { HideBelow } from "~/components/table/styles.ts";

export interface DataTableProps<TData extends RowData> {
  /**
   * The table from `useAppTable`. It comes as a prop, not through context, so a re-render of the
   * page that owns it (a sort, a new page, new data or meta) always reaches the rows.
   */
  readonly table: AppTableInstance<TData>;
  /** Rendered in place of the body when the (filtered) model is empty. */
  readonly empty: ReactNode;
  /**
   * A line of the table's own for the band at the foot of the card, such as "The 10 busiest" and a
   * link to the whole list on a preview table. Without it, and on any table with more rows than one
   * page holds, the band is the dashboard's: "Showing x-y of z", the rows per page and the pager.
   */
  readonly footer?: ReactNode;
  /**
   * Ignored: every table puts its band inside the card now (design spec §13.3). The prop goes once
   * its last caller drops it.
   */
  readonly footerPlacement?: "below" | "inside";
  /** Adds a click handler and pointer cursor to every row. */
  readonly onRowClick?: ((rowId: string) => void) | undefined;
  /**
   * Which rows the click reaches, when not all of them: a row that stands for nothing to open keeps
   * the plain cursor, so it does not promise a click that does nothing.
   */
  readonly isRowClickable?: ((rowId: string) => boolean) | undefined;
  /**
   * A band inside the card above the header row, such as the actions for the ticked rows: the
   * dashboard's DNS bulk bar, which takes the place of the info band and leaves the column headings
   * where they are. It stays mounted, so a dialog inside it outlives the selection it acts on.
   */
  readonly headerOverlay?: ReactNode;
  /** Whether the overlay stands in the band; while it does not, it stays mounted, hidden. */
  readonly overlayShown?: boolean;
  /**
   * What the band says while no rows are ticked, the dashboard's info band ("You have used 169 of
   * 3,500 records"). The band is always there on a table with an overlay, so ticking the first row
   * does not push the rows down under the pointer.
   */
  readonly headerInfo?: ReactNode;
}

/**
 * The Cloudflare dashboard's DataTable: Kumo's table parts styled by `tableClass` in the card
 * `TableScroll` draws, with the "Showing x-y of z", rows per page and pager band inside the card's
 * foot. Columns declare `meta.className` for cell widths and alignment, `meta.numeric` for a column
 * of figures, `meta.sticky` to pin a column to an edge (the row's ⋯ menu, 50px), `meta.hideBelow`
 * for a column a phone can do without and `enableSorting` for a sortable header; everything else is
 * the column's `cell` renderer. A row whose checkbox is ticked takes the tint fill.
 */
export function DataTable<TData extends RowData>({
  table,
  empty,
  footer,
  onRowClick,
  isRowClickable,
  headerOverlay,
  overlayShown = false,
  headerInfo,
}: DataTableProps<TData>): ReactElement {
  const { rows } = table.getRowModel();
  const pinnedRight = table
    .getAllColumns()
    .some((column) => column.columnDef.meta?.sticky === "right");
  // What the rows on the page depend on, by value: a state read hands back a fresh array for a slice
  // that did not change, and the instance itself is rebuilt on every render, so a reference would
  // make this look changed every time.
  const shows = `${table.state.globalFilter ?? ""}|${JSON.stringify(table.state.columnFilters)}|${table.getRowCount()}`;
  const shown = useRef(shows);
  const { setPageIndex } = table;

  // Back to the first page whenever the rows change under the table: a search, a filter or a
  // narrower collection. The row model's own reset cannot do this, because a page rebuilds its
  // filtered array on every render and a new array is not new data.
  useEffect(() => {
    if (shown.current !== shows) {
      shown.current = shows;
      setPageIndex(0);
    }
  }, [shows, setPageIndex]);

  return (
    // A container while there is a band above the header, so its actions can fold to icons.
    <div className={cn("flex min-w-0 flex-col", headerOverlay !== undefined && "@container")}>
      <TableScroll
        pinnedRight={pinnedRight}
        header={
          headerOverlay === undefined ? null : (
            <div
              data-slot="table-header-band"
              className="flex min-h-12 items-center border-b border-kumo-line bg-kumo-base px-4 py-2"
            >
              <div className={cn("w-full min-w-0 items-center", overlayShown ? "flex" : "hidden")}>
                {headerOverlay}
              </div>
              {overlayShown ? null : (
                <p className="text-kumo-subtle">
                  {headerInfo ?? "Tick rows to act on several at once."}
                </p>
              )}
            </div>
          )
        }
        below={rows.length === 0 ? empty : null}
        footer={pageBand(table, footer)}
      >
        {(overflowing) => (
          <Table className={tableClass}>
            <Table.Header>
              {table.getHeaderGroups().map((group) => (
                <Table.Row key={group.id}>
                  {group.headers.map((header) => {
                    const { meta } = header.column.columnDef;

                    return (
                      <Table.Head
                        key={header.id}
                        {...(meta?.sticky === undefined ? {} : { sticky: meta.sticky })}
                        // A column whose cells sit at the top still has its heading on the
                        // header row's line, so `align-top` in the meta reaches the cells alone.
                        className={cn(cellClass(meta, overflowing), "align-middle")}
                      >
                        {header.isPlaceholder ? null : (
                          <HeaderContent
                            sortable={header.column.getCanSort()}
                            sorted={header.column.getIsSorted()}
                            numeric={meta?.numeric ?? false}
                            onToggle={header.column.getToggleSortingHandler()}
                          >
                            <table.FlexRender header={header} />
                          </HeaderContent>
                        )}
                      </Table.Head>
                    );
                  })}
                </Table.Row>
              ))}
            </Table.Header>
            <Table.Body>
              {rows.map((row) => {
                const clickable = onRowClick !== undefined && (isRowClickable?.(row.id) ?? true);

                return (
                  <Table.Row
                    key={row.id}
                    className={cn(tableRowClass, clickable && "cursor-pointer")}
                    onClick={
                      !clickable || onRowClick === undefined
                        ? undefined
                        : (event) => {
                            // Clicks on controls inside a row belong to the control.
                            if (
                              !(event.target instanceof Element) ||
                              // Kumo's Checkbox is a span carrying the role, not a real input, so
                              // the role is what keeps a tick box from opening the row behind it.
                              event.target.closest(
                                "button, a, input, [role=checkbox], [role=menu]",
                              ) === null
                            ) {
                              onRowClick(row.id);
                            }
                          }
                    }
                  >
                    {row.getAllCells().map((cell) => {
                      const { meta } = cell.column.columnDef;

                      return (
                        <Table.Cell
                          key={cell.id}
                          {...(meta?.sticky === undefined ? {} : { sticky: meta.sticky })}
                          className={cellClass(meta, overflowing)}
                        >
                          <table.FlexRender cell={cell} />
                        </Table.Cell>
                      );
                    })}
                  </Table.Row>
                );
              })}
            </Table.Body>
          </Table>
        )}
      </TableScroll>
    </div>
  );
}

/** The classes a column asks for, in the order that lets `meta.className` override the convention. */
function cellClass(
  meta:
    | {
        readonly className?: string;
        readonly numeric?: boolean;
        readonly sticky?: string;
        readonly hideBelow?: HideBelow;
      }
    | undefined,
  overflowing: boolean,
): string {
  return cn(
    meta?.numeric === true && "text-right tabular-nums",
    // The fade reads as the pinned column's edge, so it is drawn only while something is scrolled
    // behind it; Kumo's own fade is wider than the dashboard's.
    meta?.sticky === "right" && (overflowing ? pinnedEdgeClass : "before:hidden"),
    meta?.hideBelow === undefined ? undefined : hideBelowClasses[meta.hideBelow],
    meta?.className,
  );
}

/**
 * The band at the foot of the card: the range, the rows per page and the pager, on every table that
 * has rows, a short one included (design spec §13.3). A table with a line of its own, a preview of
 * a longer list, shows that line instead while its rows fit one page.
 */
function pageBand<TData extends RowData>(
  table: AppTableInstance<TData>,
  custom: ReactNode,
): ReactNode {
  const total = table.getRowCount();
  const { pageIndex, pageSize } = table.state.pagination;

  if (total === 0) {
    return null;
  }

  if (custom !== undefined && custom !== null && total <= pageSize) {
    return custom;
  }

  return (
    <PagingBand
      page={pageIndex + 1}
      pageSize={pageSize}
      total={total}
      setPage={(page) => {
        table.setPageIndex(page - 1);
      }}
      setPageSize={(size) => {
        table.setPageSize(size);
      }}
    />
  );
}

function HeaderContent({
  sortable,
  sorted,
  numeric,
  onToggle,
  children,
}: {
  readonly sortable: boolean;
  readonly sorted: false | SortDirection;
  readonly numeric: boolean;
  readonly onToggle: ((event: unknown) => void) | undefined;
  readonly children: ReactNode;
}): ReactNode {
  if (!sortable) {
    return children;
  }

  const icon = <SortIcon sorted={sorted} />;

  return (
    <button
      type="button"
      onClick={onToggle}
      className={cn(
        "group/sort inline-flex h-6 items-center gap-1.5 rounded-sm hover:text-kumo-default focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none",
        sorted !== false && "text-kumo-default",
      )}
    >
      {numeric ? icon : null}
      {children}
      {numeric ? null : icon}
    </button>
  );
}

/**
 * The sort caret beside a sortable heading: the 12px up-down caret at half strength while the
 * column is not sorted, a single caret in the heading's own colour once it is.
 */
function SortIcon({ sorted }: { readonly sorted: false | SortDirection }): ReactElement {
  if (sorted === "asc") {
    return <CaretUpIcon size={12} weight="bold" aria-label="Sorted ascending" />;
  }

  if (sorted === "desc") {
    return <CaretDownIcon size={12} weight="bold" aria-label="Sorted descending" />;
  }

  return <CaretUpDownIcon size={12} aria-hidden className="opacity-50" />;
}
