import { Pagination } from "@cloudflare/kumo/components/pagination";
import { Table } from "@cloudflare/kumo/components/table";
import { cn } from "@cloudflare/kumo/utils";
import { ArrowDownIcon, ArrowsDownUpIcon, ArrowUpIcon } from "@phosphor-icons/react";
import type { SortDirection } from "@tanstack/react-table";
import { useEffect, useRef } from "react";
import type { ReactElement, ReactNode } from "react";

import { useTableContext } from "~/components/table/app-table.tsx";
import { TableScroll } from "~/components/table/scroll-panel.tsx";
import {
  FrameBand,
  frameTableClass,
  frameTableRowClass,
  pinnedEdgeClass,
} from "~/components/ui/frame.tsx";

export interface DataTableProps {
  /** Rendered in place of the body when the (filtered) model is empty. */
  readonly empty: ReactNode;
  /**
   * Rendered on the band under the panel: "Showing N of M". A table with a second page shows its
   * paging controls there instead, because the range they name replaces that count.
   */
  readonly footer?: ReactNode;
  /** Adds a click handler and pointer cursor to every row. */
  readonly onRowClick?: ((rowId: string) => void) | undefined;
  /**
   * Whether the panel scrolls its rows with the header pinned to the top, from `lg` up: a phone has
   * no room for a window inside the page, so there the page keeps scrolling. On by default, since a
   * table shorter than the cap never reaches it.
   */
  readonly scroll?: boolean;
}

/**
 * Renders the table from the nearest `AppTable` provider with Kumo's table parts, as the panel of
 * the Frame it sits in, with the footer on the band below. Columns declare `meta.className` for
 * cell widths and alignment, `meta.numeric` for a column of figures, `meta.sticky` to pin a column
 * to an edge and `enableSorting` for a sortable header; everything else is the column's `cell`
 * renderer.
 */
export function DataTable({
  empty,
  footer,
  onRowClick,
  scroll = true,
}: DataTableProps): ReactElement {
  const table = useTableContext();
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
    <>
      <TableScroll
        scroll={scroll}
        pinnedRight={pinnedRight}
        below={rows.length === 0 ? empty : null}
      >
        {(overflowing) => (
          <Table className={frameTableClass}>
            <Table.Header variant="compact" {...(scroll ? { sticky: true } : {})}>
              {table.getHeaderGroups().map((group) => (
                <Table.Row key={group.id}>
                  {group.headers.map((header) => {
                    const { meta } = header.column.columnDef;

                    return (
                      <Table.Head
                        key={header.id}
                        {...(meta?.sticky === undefined ? {} : { sticky: meta.sticky })}
                        className={cellClass(meta, overflowing)}
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
              {rows.map((row) => (
                <Table.Row
                  key={row.id}
                  className={cn(frameTableRowClass, onRowClick !== undefined && "cursor-pointer")}
                  onClick={
                    onRowClick === undefined
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
              ))}
            </Table.Body>
          </Table>
        )}
      </TableScroll>
      <PageBand fallback={footer} />
    </>
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
    // The hairline reads as the pinned column's edge, so it appears only while there is something
    // scrolled behind it.
    meta?.sticky === "right" && overflowing && pinnedEdgeClass,
    meta?.className,
  );
}

/**
 * The band under the panel: the page's own count while everything fits on one page, and the range
 * with Previous and Next once it does not.
 */
function PageBand({ fallback }: { readonly fallback: ReactNode }): ReactNode {
  const table = useTableContext();

  if (table.getPageCount() <= 1) {
    return fallback;
  }

  const { pageIndex, pageSize } = table.state.pagination;
  const total = table.getRowCount();
  const first = pageIndex * pageSize + 1;
  const last = Math.min(first + pageSize - 1, total);

  return (
    <FrameBand className="px-5">
      <Pagination
        page={pageIndex + 1}
        perPage={pageSize}
        totalCount={total}
        setPage={(page) => {
          table.setPageIndex(page - 1);
        }}
      >
        <Pagination.Info className="text-sm">
          {() => `Showing ${first}–${last} of ${total}`}
        </Pagination.Info>
        <Pagination.Controls controls="simple" />
      </Pagination>
    </FrameBand>
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
        "group/sort inline-flex h-6 items-center gap-1 rounded-sm px-1.5 hover:bg-kumo-tint hover:text-kumo-default focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none",
        // The padding is pulled back off the cell's inset, so the label starts where a cell's text does.
        numeric ? "-mr-1.5" : "-ml-1.5",
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
 * The direction the column is sorted in. An unsorted column keeps the icon's room but only shows it
 * under the pointer or the keyboard, so a wide table's headers read as words rather than as
 * arrows.
 */
function SortIcon({ sorted }: { readonly sorted: false | SortDirection }): ReactElement {
  if (sorted === "asc") {
    return <ArrowUpIcon size={14} aria-label="sorted ascending" />;
  }

  if (sorted === "desc") {
    return <ArrowDownIcon size={14} aria-label="sorted descending" />;
  }

  return (
    <ArrowsDownUpIcon
      size={14}
      aria-hidden
      className="text-kumo-subtle opacity-0 group-hover/sort:opacity-100 group-focus-visible/sort:opacity-100"
    />
  );
}
