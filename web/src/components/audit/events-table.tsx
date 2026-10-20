import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Table } from "@cloudflare/kumo/components/table";
import { cn } from "@cloudflare/kumo/utils";
import { DotsSixVerticalIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { DragEvent, KeyboardEvent, MouseEvent, ReactElement, ReactNode } from "react";

import type { AuditEvent } from "~/api/queries.ts";
import {
  ActionCell,
  ActorCell,
  DetailCell,
  ResultCell,
  TargetCell,
  TimeCell,
} from "~/components/audit/cells.tsx";
import { auditColumnLabels, auditColumns } from "~/components/audit/columns.tsx";
import type { AuditColumn, AuditColumnId } from "~/components/audit/columns.tsx";
import { EventSheet } from "~/components/audit/event-sheet.tsx";
import { tableEmptyClass } from "~/components/table/empty.ts";
import type { CursorPaging } from "~/components/table/page-window.ts";
import { CursorBand } from "~/components/table/paging.tsx";
import { TableScroll } from "~/components/table/scroll-panel.tsx";
import { tableClass, tableRowClass } from "~/components/table/styles.ts";

/** A click on a control inside the row belongs to the control, not to the row. */
function onControl(event: MouseEvent<HTMLElement>): boolean {
  return (
    event.target instanceof Element &&
    event.target.closest("button, a, input, [role=menu]") !== null
  );
}

const noneHidden: ReadonlySet<AuditColumn> = new Set();

/**
 * Each column's width. The table is fixed-layout, so every column but Details has one and Details
 * takes what is left; a cell in a fixed column truncates or wraps, never pushes the next one.
 */
const columnClass: Record<AuditColumnId, string> = {
  time: "w-60",
  action: "w-52",
  target: "w-40",
  actor: "w-44",
  result: "w-24",
  details: "hidden lg:table-cell",
};

type CellOf = (event: AuditEvent, onOpen: () => void) => ReactNode;

/** What each column shows of an event; `onOpen` opens the event's sheet. */
const cells: Record<AuditColumnId, CellOf> = {
  time: (event, onOpen) => <TimeCell event={event} onOpen={onOpen} />,
  action: (event) => <ActionCell event={event} />,
  target: (event) => <TargetCell event={event} />,
  actor: (event) => <ActorCell event={event} />,
  result: (event) => <ResultCell event={event} />,
  details: (event, onOpen) => <DetailCell event={event} onOpen={onOpen} />,
};

function EventRow({
  event,
  columns,
  open,
  onOpen,
}: {
  readonly event: AuditEvent;
  readonly columns: readonly AuditColumnId[];
  /** Whether this event is the one in the sheet. */
  readonly open: boolean;
  readonly onOpen: () => void;
}): ReactElement {
  return (
    <Table.Row
      variant={open ? "selected" : "default"}
      className={cn("cursor-pointer align-top", !open && tableRowClass)}
      onClick={(click) => {
        if (!onControl(click)) {
          onOpen();
        }
      }}
    >
      {columns.map((column) => (
        <Table.Cell key={column} className={columnClass[column]}>
          {cells[column](event, onOpen)}
        </Table.Cell>
      ))}
    </Table.Row>
  );
}

const dragType = "application/x-slopscale-audit-column";

/** A shown column's place in the full order, or nothing past either end. */
function neighbour(
  order: readonly AuditColumnId[],
  column: AuditColumnId | undefined,
): number | undefined {
  return column === undefined ? undefined : order.indexOf(column);
}

/**
 * A header that can be dragged to another place, the Zero Trust log's six-dot grip in front of its
 * name. The grip is also a button: with it focused, the arrow keys move the column a place left or
 * right, so the order is not the mouse's alone.
 */
function ColumnHead({
  column,
  index,
  left,
  right,
  onMove,
}: {
  readonly column: AuditColumnId;
  /** The column's place in the full order, hidden columns included: where a drop puts a column. */
  readonly index: number;
  /** Where the arrow keys move it: the places of the shown columns either side, if any. */
  readonly left: number | undefined;
  readonly right: number | undefined;
  readonly onMove: (column: AuditColumnId, to: number) => void;
}): ReactElement {
  const [over, setOver] = useState(false);
  const label = auditColumnLabels[column];

  return (
    <Table.Head
      className={cn(columnClass[column], over && "bg-kumo-tint")}
      draggable
      onDragStart={(drag: DragEvent<HTMLTableCellElement>) => {
        drag.dataTransfer.setData(dragType, column);
        drag.dataTransfer.effectAllowed = "move";
      }}
      onDragOver={(drag: DragEvent<HTMLTableCellElement>) => {
        if (drag.dataTransfer.types.includes(dragType)) {
          drag.preventDefault();
          setOver(true);
        }
      }}
      onDragLeave={() => {
        setOver(false);
      }}
      onDrop={(drag: DragEvent<HTMLTableCellElement>) => {
        drag.preventDefault();
        setOver(false);

        const moved = drag.dataTransfer.getData(dragType);
        const known = auditColumns.find((candidate) => candidate === moved);

        if (known !== undefined && known !== column) {
          onMove(known, index);
        }
      }}
    >
      <span className="flex items-center gap-1">
        <button
          type="button"
          aria-label={`Move the ${label} column`}
          aria-keyshortcuts="ArrowLeft ArrowRight"
          className="-ml-1.5 flex size-5 shrink-0 cursor-grab items-center justify-center rounded-sm text-kumo-subtle hover:text-kumo-default focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none"
          onKeyDown={(key: KeyboardEvent<HTMLButtonElement>) => {
            if (key.key === "ArrowLeft" && left !== undefined) {
              key.preventDefault();
              onMove(column, left);
            } else if (key.key === "ArrowRight" && right !== undefined) {
              key.preventDefault();
              onMove(column, right);
            }
          }}
        >
          <DotsSixVerticalIcon aria-hidden className="size-3.5" />
        </button>
        <span className="truncate">{label}</span>
      </span>
    </Table.Head>
  );
}

export interface EventsTableProps {
  readonly events: readonly AuditEvent[];
  /**
   * True when a filter is set: the list is empty because of the filters, not because nothing
   * happened.
   */
  readonly filtered: boolean;
  /** Puts the page back to the default range with no action and any user. */
  readonly onClearFilters: () => void;
  readonly paging: CursorPaging;
  /** The columns the reader switched off in the Columns menu. */
  readonly hidden?: ReadonlySet<AuditColumn>;
  /** Every column in the reader's order, hidden ones included. */
  readonly order?: readonly AuditColumnId[];
  /** Moves a column to another place in the order; without it the headers do not move. */
  readonly onMove?: (column: AuditColumnId, to: number) => void;
}

/**
 * The audit list, as the Zero Trust admin activity log: server-ordered (newest first) and
 * server-paged, so the table neither sorts nor filters. The date is the row's link and opens a
 * sheet at the right edge with every recorded field; a click anywhere else on the row does the
 * same. The headers can be dragged into another order, and the band inside the frame walks the
 * pages.
 */
export function EventsTable({
  events,
  filtered,
  onClearFilters,
  paging,
  hidden = noneHidden,
  order = auditColumns,
  onMove,
}: EventsTableProps): ReactElement {
  const [openId, setOpenId] = useState<string | null>(null);
  const open = events.find((event) => event.id === openId) ?? null;
  const shown = order.filter((column) => !(hidden as ReadonlySet<AuditColumnId>).has(column));

  return (
    <>
      <TableScroll
        below={
          events.length === 0 ? (
            <EmptyEvents filtered={filtered} onClearFilters={onClearFilters} />
          ) : null
        }
        footer={
          paging.loaded === 0 ? null : (
            // The page size is the toolbar's "Limit", so the band leaves its own out.
            <CursorBand paging={paging} hidePageSize />
          )
        }
      >
        {() => (
          // The minimum keeps the columns readable on a phone, where the panel scrolls sideways.
          <Table className={cn(tableClass, "min-w-[880px] table-fixed")}>
            <Table.Header>
              <Table.Row>
                {shown.map((column, at) =>
                  onMove === undefined ? (
                    <Table.Head key={column} className={columnClass[column]}>
                      {auditColumnLabels[column]}
                    </Table.Head>
                  ) : (
                    <ColumnHead
                      key={column}
                      column={column}
                      index={order.indexOf(column)}
                      left={neighbour(order, shown[at - 1])}
                      right={neighbour(order, shown[at + 1])}
                      onMove={onMove}
                    />
                  ),
                )}
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {events.map((event) => (
                <EventRow
                  key={event.id}
                  event={event}
                  columns={shown}
                  open={openId === event.id}
                  onOpen={() => {
                    setOpenId(event.id);
                  }}
                />
              ))}
            </Table.Body>
          </Table>
        )}
      </TableScroll>
      <EventSheet
        event={open}
        onClose={() => {
          setOpenId(null);
        }}
      />
    </>
  );
}

/**
 * An empty log is nearly always the filters: the default range is seven days, so "nothing in range"
 * and "nothing matching" both end here with the one control that widens the list again.
 */
function EmptyEvents({
  filtered,
  onClearFilters,
}: {
  readonly filtered: boolean;
  readonly onClearFilters: () => void;
}): ReactElement {
  if (filtered) {
    return (
      <Empty
        className={tableEmptyClass}
        size="sm"
        title="No events match"
        description="No events in this range match these filters."
        contents={
          <Button variant="secondary" onClick={onClearFilters}>
            Clear filters
          </Button>
        }
      />
    );
  }

  return (
    <Empty
      className={tableEmptyClass}
      size="sm"
      title="No events yet"
      description="Every change made through the API or this console is recorded here."
    />
  );
}
