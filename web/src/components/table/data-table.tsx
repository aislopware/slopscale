import { Table } from "@cloudflare/kumo/components/table";
import { cn } from "@cloudflare/kumo/utils";
import { CaretDownIcon, CaretUpDownIcon, CaretUpIcon } from "@phosphor-icons/react";
import type { RowData, SortDirection } from "@tanstack/react-table";
import { useEffect, useRef } from "react";
import type { ReactElement, ReactNode } from "react";

import type { AppTableInstance } from "~/components/table/app-table.tsx";
import { PagingBand, smallestPageSize } from "~/components/table/paging.tsx";
import { TableScroll } from "~/components/table/scroll-panel.tsx";
import { pinnedEdgeClass, tableClass, tableRowClass } from "~/components/table/styles.ts";

export interface DataTableProps<TData extends RowData> {
  /**
   * The table from `useAppTable`. It comes as a prop, not through context, so a re-render of the
   * page that owns it (a sort, a new page, new data or meta) always reaches the rows.
   */
  readonly table: AppTableInstance<TData>;
  /** Rendered in place of the body when the (filtered) model is empty. */
  readonly empty: ReactNode;
  /**
   * Rendered under the card: "Showing N of M". A table with enough rows to page shows the range,
   * the page size and the paging controls there instead.
   */
  readonly footer?: ReactNode;
  /**
   * Where the footer and the pager go: under the card (`below`, the account-level DataTable of
   * §4.A) or in a band inside its frame (`inside`, the Zero Trust list of §13.3, for a list that is
   * the page's subject, such as Machines).
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
   * Drawn on the header row after the first column, such as the actions for the ticked rows next to
   * the select-all box. It stays mounted, so a dialog inside it outlives the selection it acts on.
   */
  readonly headerOverlay?: ReactNode;
  /**
   * Whether the overlay stands in for the other headings. They keep their room, invisible, so no
   * column changes width and no row moves when the overlay comes and goes.
   */
  readonly overlayShown?: boolean;
}

/**
 * The Cloudflare dashboard's DataTable: Kumo's table parts styled by `tableClass` in the card
 * `TableScroll` draws, with "Showing x-y of z" (or the pager) under the card. Columns declare
 * `meta.className` for cell widths and alignment, `meta.numeric` for a column of figures,
 * `meta.sticky` to pin a column to an edge (the row's ⋯ menu, 50px) and `enableSorting` for a
 * sortable header; everything else is the column's `cell` renderer.
 */
export function DataTable<TData extends RowData>({
  table,
  empty,
  footer,
  footerPlacement = "below",
  onRowClick,
  isRowClickable,
  headerOverlay,
  overlayShown = false,
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

  const band = pageBand(table, footer);
  const inside = footerPlacement === "inside";

  return (
    // A container while there is a header overlay, so the overlay can reach the card's right edge.
    <div className={cn("flex min-w-0 flex-col gap-2", headerOverlay !== undefined && "@container")}>
      <TableScroll
        pinnedRight={pinnedRight}
        below={rows.length === 0 ? empty : null}
        footer={inside ? band : null}
      >
        {(overflowing) => (
          <Table className={tableClass}>
            <Table.Header>
              {table.getHeaderGroups().map((group) => (
                <Table.Row key={group.id}>
                  {group.headers.map((header, index) => {
                    const { meta } = header.column.columnDef;

                    return (
                      <Table.Head
                        key={header.id}
                        {...(meta?.sticky === undefined ? {} : { sticky: meta.sticky })}
                        // A column whose cells sit at the top still has its heading on the
                        // header row's line, so `align-top` in the meta reaches the cells alone.
                        className={cn(
                          cellClass(meta, overflowing),
                          "align-middle",
                          overlayShown && index > 0 && "invisible",
                        )}
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
                        {index === 0 && headerOverlay !== undefined ? (
                          // As wide as the card left of the cell's edge (100cqw is the card, 100% the
                          // cell), so the overlay can put its actions at the right end of the row.
                          <div className="absolute inset-y-0 left-full flex w-[calc(100cqw-100%)] items-center pr-4">
                            {headerOverlay}
                          </div>
                        ) : null}
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
      {inside || band === null || band === undefined ? null : (
        <div data-slot="table-footer">{band}</div>
      )}
    </div>
  );
}

/** The classes a column asks for, in the order that lets `meta.className` override the convention. */
function cellClass(
  meta:
    | { readonly className?: string; readonly numeric?: boolean; readonly sticky?: string }
    | undefined,
  overflowing: boolean,
): string {
  return cn(
    meta?.numeric === true && "text-right tabular-nums",
    // The fade reads as the pinned column's edge, so it is drawn only while something is scrolled
    // behind it; Kumo's own fade is wider than the dashboard's.
    meta?.sticky === "right" && (overflowing ? pinnedEdgeClass : "before:hidden"),
    meta?.className,
  );
}

/**
 * The band under the panel: the page's own count while the rows fit the smallest page, and the
 * range with the page size and the paging controls once there are enough rows to page.
 */
function pageBand<TData extends RowData>(
  table: AppTableInstance<TData>,
  fallback: ReactNode,
): ReactNode {
  const total = table.getRowCount();

  if (total <= smallestPageSize) {
    return fallback;
  }

  const { pageIndex, pageSize } = table.state.pagination;

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
