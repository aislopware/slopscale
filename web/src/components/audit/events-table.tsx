import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Table } from "@cloudflare/kumo/components/table";
import { cn } from "@cloudflare/kumo/utils";
import { CaretDownIcon, CaretRightIcon, ClockCounterClockwiseIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { MouseEvent, ReactElement, ReactNode } from "react";

import type { AuditEvent } from "~/api/queries.ts";
import {
  ActionCell,
  ActorCell,
  DetailCell,
  EventDetail,
  ResultCell,
  TargetCell,
} from "~/components/audit/cells.tsx";
import { plural } from "~/components/overview/plural.ts";
import { emptyIconSize, tableEmptyClass } from "~/components/table/empty.ts";
import { TableScroll } from "~/components/table/scroll-panel.tsx";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { frameTableClass, frameTableRowClass, pinnedEdgeClass } from "~/components/ui/frame.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";

/** Every row spans this many columns when it opens. */
const columnCount = 7;

/** A click on a control inside the row belongs to the control, not to the row. */
function onControl(event: MouseEvent<HTMLElement>): boolean {
  return (
    event.target instanceof Element &&
    event.target.closest("button, a, input, [role=menu]") !== null
  );
}

function EventRows({
  event,
  overflowing,
  open,
  onToggle,
  onOpen,
}: {
  readonly event: AuditEvent;
  /** Whether the columns run past the panel, so the pinned last column draws its edge. */
  readonly overflowing: boolean;
  readonly open: boolean;
  readonly onToggle: () => void;
  readonly onOpen: () => void;
}): ReactElement {
  return (
    <>
      <Table.Row
        variant={open ? "selected" : "default"}
        className={cn("cursor-pointer align-top", !open && frameTableRowClass)}
        onClick={(click) => {
          if (!onControl(click)) {
            onToggle();
          }
        }}
      >
        <Table.Cell className="whitespace-nowrap text-kumo-subtle">
          <RelativeTime value={event.createdAt} />
        </Table.Cell>
        <Table.Cell>
          <ActorCell event={event} />
        </Table.Cell>
        <Table.Cell>
          <ActionCell event={event} />
        </Table.Cell>
        <Table.Cell>
          <TargetCell event={event} />
        </Table.Cell>
        <Table.Cell>
          <ResultCell event={event} />
        </Table.Cell>
        <Table.Cell className="hidden lg:table-cell">
          <DetailCell event={event} onOpen={onOpen} />
        </Table.Cell>
        <Table.Cell sticky="right" className={cn("text-right", overflowing && pinnedEdgeClass)}>
          {/* The chevron is the row's own control, not a second "Show details" button beside it:
              it says the row opens and gives the keyboard the same reach as the click. */}
          <Button
            variant="ghost"
            shape="square"
            size="sm"
            icon={open ? CaretDownIcon : CaretRightIcon}
            aria-expanded={open}
            aria-label={open ? "Collapse row" : "Expand row"}
            onClick={onToggle}
          />
        </Table.Cell>
      </Table.Row>
      {open ? (
        <Table.Row variant="selected">
          <Table.Cell colSpan={columnCount} className="p-0">
            <EventDetail event={event} />
          </Table.Cell>
        </Table.Row>
      ) : null}
    </>
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
  /** The server has at least one more page for these filters. */
  readonly hasMore: boolean;
  readonly loadingMore: boolean;
  readonly onLoadMore: () => void;
}

/**
 * The audit list: server-ordered (newest first) and server-paged, so the table neither sorts nor
 * filters. A row opens in place to show every recorded field; "Load more" appends the next page.
 */
export function EventsTable({
  events,
  filtered,
  onClearFilters,
  hasMore,
  loadingMore,
  onLoadMore,
}: EventsTableProps): ReactElement {
  const [openId, setOpenId] = useState<string | null>(null);

  return (
    <>
      <TableScroll pinnedRight>
        {(overflowing) => (
          // Fixed layout: every column but Summary has a set width, so Summary takes what is left
          // of the panel and its chips wrap inside it instead of stretching the table past the
          // panel. The minimum keeps the columns readable on a phone, where the panel scrolls.
          <Table className={cn(frameTableClass, "min-w-[800px] table-fixed")}>
            <Table.Header variant="compact" sticky>
              <Table.Row>
                <Table.Head className="w-32">Time</Table.Head>
                <Table.Head className="w-40">Actor</Table.Head>
                <Table.Head className="w-48">Action</Table.Head>
                <Table.Head className="w-44">Target</Table.Head>
                <Table.Head className="w-28">Result</Table.Head>
                <Table.Head className="hidden lg:table-cell">Summary</Table.Head>
                <Table.Head sticky="right" className={cn("w-12", overflowing && pinnedEdgeClass)}>
                  <span className="sr-only">Details</span>
                </Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {events.length === 0 ? (
                <Table.Row>
                  <Table.Cell colSpan={columnCount} className="p-0">
                    <EmptyEvents filtered={filtered} onClearFilters={onClearFilters} />
                  </Table.Cell>
                </Table.Row>
              ) : (
                events.map((event) => (
                  <EventRows
                    key={event.id}
                    event={event}
                    overflowing={overflowing}
                    open={openId === event.id}
                    onToggle={() => {
                      setOpenId((current) => (current === event.id ? null : event.id));
                    }}
                    onOpen={() => {
                      setOpenId(event.id);
                    }}
                  />
                ))
              )}
            </Table.Body>
          </Table>
        )}
      </TableScroll>
      <Paging
        count={events.length}
        hasMore={hasMore}
        loadingMore={loadingMore}
        onLoadMore={onLoadMore}
      />
    </>
  );
}

function Paging({
  count,
  hasMore,
  loadingMore,
  onLoadMore,
}: {
  readonly count: number;
  readonly hasMore: boolean;
  readonly loadingMore: boolean;
  readonly onLoadMore: () => void;
}): ReactNode {
  if (count === 0) {
    return null;
  }

  if (hasMore) {
    return (
      <TableFooter
        actions={
          <Button variant="secondary" size="xs" loading={loadingMore} onClick={onLoadMore}>
            Load more
          </Button>
        }
      >
        {`Showing ${plural(count, "event")}`}
      </TableFooter>
    );
  }

  return <TableFooter>{`Showing all ${plural(count, "event")} in this range`}</TableFooter>;
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
        description="Nothing was recorded in this range for these filters."
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
      icon={<ClockCounterClockwiseIcon size={emptyIconSize} />}
      title="No events yet"
      description="Every change made through the API or this console is recorded here."
    />
  );
}
