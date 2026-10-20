import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Table } from "@cloudflare/kumo/components/table";
import { cn } from "@cloudflare/kumo/utils";
import { SidebarSimpleIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { MouseEvent, ReactElement } from "react";

import type { AuditEvent } from "~/api/queries.ts";
import {
  ActionCell,
  ActorCell,
  DetailCell,
  ResultCell,
  TargetCell,
  TimeCell,
} from "~/components/audit/cells.tsx";
import { EventSheet } from "~/components/audit/detail-sheet.tsx";
import { tableEmptyClass } from "~/components/table/empty.ts";
import type { CursorPaging } from "~/components/table/page-window.ts";
import { CursorBand } from "~/components/table/paging.tsx";
import { TableScroll } from "~/components/table/scroll-panel.tsx";
import {
  actionsColumnClass,
  pinnedEdgeClass,
  tableClass,
  tableRowClass,
} from "~/components/table/styles.ts";

/** A click on a control inside the row belongs to the control, not to the row. */
function onControl(event: MouseEvent<HTMLElement>): boolean {
  return (
    event.target instanceof Element &&
    event.target.closest("button, a, input, [role=menu]") !== null
  );
}

function EventRow({
  event,
  overflowing,
  open,
  onOpen,
}: {
  readonly event: AuditEvent;
  /** Whether the columns run past the panel, so the pinned last column draws its edge. */
  readonly overflowing: boolean;
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
      <Table.Cell className="whitespace-nowrap">
        <TimeCell event={event} />
      </Table.Cell>
      <Table.Cell>
        <ActionCell event={event} />
      </Table.Cell>
      <Table.Cell>
        <TargetCell event={event} />
      </Table.Cell>
      <Table.Cell>
        <ActorCell event={event} />
      </Table.Cell>
      <Table.Cell>
        <ResultCell event={event} />
      </Table.Cell>
      <Table.Cell className="hidden lg:table-cell">
        <DetailCell event={event} onOpen={onOpen} />
      </Table.Cell>
      <Table.Cell sticky="right" className={cn(actionsColumnClass, overflowing && pinnedEdgeClass)}>
        {/* The row's own control: it says the row opens a sheet, and gives the keyboard the same
            reach as the click on the row. */}
        <Button
          variant="ghost"
          shape="square"
          size="sm"
          icon={SidebarSimpleIcon}
          aria-haspopup="dialog"
          aria-label="Show details"
          onClick={onOpen}
        />
      </Table.Cell>
    </Table.Row>
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
}

/**
 * The audit list, as the dashboard's audit logs table: server-ordered (newest first) and
 * server-paged, so the table neither sorts nor filters. A row opens a sheet at the right edge with
 * every recorded field, as the Zero Trust admin log does; the band below walks the pages.
 */
export function EventsTable({
  events,
  filtered,
  onClearFilters,
  paging,
}: EventsTableProps): ReactElement {
  const [openId, setOpenId] = useState<string | null>(null);
  const open = events.find((event) => event.id === openId) ?? null;

  return (
    <>
      <TableScroll
        pinnedRight
        below={
          events.length === 0 ? (
            <EmptyEvents filtered={filtered} onClearFilters={onClearFilters} />
          ) : null
        }
      >
        {(overflowing) => (
          // Fixed layout: every column but Details has a set width, so Details takes what is left
          // of the panel and its lines cut at that width instead of stretching the table past the
          // panel. A fixed column does not grow for its content, so every cell in one either
          // truncates or wraps; a value that does neither runs into the next column. The minimum
          // keeps the columns readable on a phone, where the panel scrolls.
          <Table className={cn(tableClass, "min-w-[800px] table-fixed")}>
            <Table.Header>
              <Table.Row>
                <Table.Head className="w-32">Time</Table.Head>
                <Table.Head className="w-56">Action</Table.Head>
                <Table.Head className="w-40">Target</Table.Head>
                <Table.Head className="w-44">Actor</Table.Head>
                <Table.Head className="w-24">Result</Table.Head>
                <Table.Head className="hidden lg:table-cell">Details</Table.Head>
                <Table.Head
                  sticky="right"
                  className={cn(actionsColumnClass, overflowing && pinnedEdgeClass)}
                >
                  <span className="sr-only">Details</span>
                </Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {events.map((event) => (
                <EventRow
                  key={event.id}
                  event={event}
                  overflowing={overflowing}
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
      <CursorBand paging={paging} noun="event" />
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
