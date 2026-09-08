import { Table } from "@cloudflare/kumo/components/table";
import { cn } from "@cloudflare/kumo/utils";
import { ArrowDownIcon, ArrowsDownUpIcon, ArrowUpIcon } from "@phosphor-icons/react";
import type { SortDirection } from "@tanstack/react-table";
import type { ReactElement, ReactNode } from "react";

import { useTableContext } from "~/components/table/app-table.tsx";
import { FramePanel } from "~/components/ui/frame.tsx";

export interface DataTableProps {
  /** Rendered in place of the body when the (filtered) model is empty. */
  readonly empty: ReactNode;
  /** Rendered on the band under the panel: "Showing N of M", paging. */
  readonly footer?: ReactNode;
  /** Adds a click handler and pointer cursor to every row. */
  readonly onRowClick?: ((rowId: string) => void) | undefined;
}

/**
 * Renders the table from the nearest `AppTable` provider with Kumo's table parts, as the panel of
 * the Frame it sits in, with the footer on the band below. Columns declare `meta.className` for
 * cell widths and alignment and `enableSorting` for a sortable header; everything else is the
 * column's `cell` renderer.
 */
export function DataTable({ empty, footer, onRowClick }: DataTableProps): ReactElement {
  const table = useTableContext();
  const { rows } = table.getRowModel();

  return (
    <>
      <FramePanel>
        {/* Only the table scrolls sideways. The empty state sits outside that scroll
            area so the columns' minimum widths cannot push it off a phone screen. */}
        <div className="overflow-x-auto">
          <Table>
            <Table.Header variant="compact">
              {table.getHeaderGroups().map((group) => (
                <Table.Row key={group.id}>
                  {group.headers.map((header) => {
                    const sortable = header.column.getCanSort();
                    const sorted = header.column.getIsSorted();
                    const className = header.column.columnDef.meta?.className;

                    return (
                      <Table.Head
                        key={header.id}
                        {...(className === undefined ? {} : { className })}
                      >
                        {header.isPlaceholder ? null : (
                          <HeaderContent
                            sortable={sortable}
                            sorted={sorted}
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
              {rows.length === 0
                ? null
                : rows.map((row) => (
                    <Table.Row
                      key={row.id}
                      className={cn(onRowClick !== undefined && "cursor-pointer")}
                      onClick={
                        onRowClick === undefined
                          ? undefined
                          : (event) => {
                              // Clicks on controls inside a row belong to the control.
                              if (
                                !(event.target instanceof Element) ||
                                event.target.closest("button, a, input, [role=menu]") === null
                              ) {
                                onRowClick(row.id);
                              }
                            }
                      }
                    >
                      {row.getAllCells().map((cell) => {
                        const className = cell.column.columnDef.meta?.className;

                        return (
                          <Table.Cell
                            key={cell.id}
                            {...(className === undefined ? {} : { className })}
                          >
                            <table.FlexRender cell={cell} />
                          </Table.Cell>
                        );
                      })}
                    </Table.Row>
                  ))}
            </Table.Body>
          </Table>
        </div>
        {rows.length === 0 ? empty : null}
      </FramePanel>
      {footer}
    </>
  );
}

function HeaderContent({
  sortable,
  sorted,
  onToggle,
  children,
}: {
  readonly sortable: boolean;
  readonly sorted: false | SortDirection;
  readonly onToggle: ((event: unknown) => void) | undefined;
  readonly children: ReactNode;
}): ReactNode {
  if (!sortable) {
    return children;
  }

  return (
    <button
      type="button"
      onClick={onToggle}
      className={cn(
        "-ml-1.5 inline-flex h-6 items-center gap-1 rounded-sm px-1.5 hover:bg-kumo-tint hover:text-kumo-default focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none",
        sorted !== false && "text-kumo-default",
      )}
    >
      {children}
      <SortIcon sorted={sorted} />
    </button>
  );
}

function SortIcon({ sorted }: { readonly sorted: false | SortDirection }): ReactElement {
  if (sorted === "asc") {
    return <ArrowUpIcon size={14} aria-label="sorted ascending" />;
  }

  if (sorted === "desc") {
    return <ArrowDownIcon size={14} aria-label="sorted descending" />;
  }

  return <ArrowsDownUpIcon size={14} aria-hidden className="text-kumo-subtle" />;
}
