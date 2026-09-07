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
import { TableFooter } from "~/components/table/toolbar.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";

const emptyIconSize = 32;
/** The table already draws the card edge, so the empty panel drops its own. */
const emptyClass = "border-none bg-kumo-base";
/** Every row spans this many columns when it opens. */
const columnCount = 7;
/** The console's top bar is `h-12`, so the sticky header parks under it instead of behind it. */
const stickyHeader = "[&_th]:top-12";

/** A click on a control inside the row belongs to the control, not to the row. */
function onControl(event: MouseEvent<HTMLElement>): boolean {
  return (
    event.target instanceof Element &&
    event.target.closest("button, a, input, [role=menu]") !== null
  );
}

function EventRows({
  event,
  striped,
  open,
  onToggle,
  onOpen,
}: {
  readonly event: AuditEvent;
  /** Zebra: the row is drawn on the elevated surface while it is closed. */
  readonly striped: boolean;
  readonly open: boolean;
  readonly onToggle: () => void;
  readonly onOpen: () => void;
}): ReactElement {
  return (
    <>
      <Table.Row
        variant={open ? "selected" : "default"}
        className={cn("cursor-pointer align-top", !open && striped && "bg-kumo-elevated")}
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
        <Table.Cell className="hidden w-full max-w-0 lg:table-cell">
          <DetailCell event={event} onOpen={onOpen} />
        </Table.Cell>
        <Table.Cell className="w-10 text-right">
          <Button
            variant="ghost"
            shape="square"
            size="sm"
            icon={open ? CaretDownIcon : CaretRightIcon}
            aria-expanded={open}
            aria-label={open ? "Hide details" : "Show details"}
            onClick={onToggle}
          />
        </Table.Cell>
      </Table.Row>
      <Table.Row variant="selected" className={open ? undefined : "hidden"}>
        <Table.Cell colSpan={columnCount} className="p-0">
          {open ? <EventDetail event={event} /> : null}
        </Table.Cell>
      </Table.Row>
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
    <Table>
      <Table.Header variant="compact" sticky className={stickyHeader}>
        <Table.Row>
          <Table.Head>Time</Table.Head>
          <Table.Head>Actor</Table.Head>
          <Table.Head>Action</Table.Head>
          <Table.Head>Target</Table.Head>
          <Table.Head>Result</Table.Head>
          <Table.Head className="hidden w-full lg:table-cell">Detail</Table.Head>
          <Table.Head className="w-10">
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
          events.map((event, index) => (
            <EventRows
              key={event.id}
              event={event}
              striped={index % 2 === 1}
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
      <Table.Footer>
        <Table.Row>
          <Table.Cell colSpan={columnCount} className="p-0">
            <Paging
              count={events.length}
              hasMore={hasMore}
              loadingMore={loadingMore}
              onLoadMore={onLoadMore}
            />
          </Table.Cell>
        </Table.Row>
      </Table.Footer>
    </Table>
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
      <Button
        variant="ghost"
        className="h-10 w-full rounded-none"
        loading={loadingMore}
        onClick={onLoadMore}
      >
        Load more
      </Button>
    );
  }

  return <TableFooter>{`Showing all ${count} events in this range`}</TableFooter>;
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
        className={emptyClass}
        size="sm"
        title="No events match"
        description="Nothing was recorded in this range for these filters."
        contents={
          <Button variant="secondary" size="sm" onClick={onClearFilters}>
            Clear filters
          </Button>
        }
      />
    );
  }

  return (
    <Empty
      className={emptyClass}
      size="sm"
      icon={<ClockCounterClockwiseIcon size={emptyIconSize} />}
      title="No events yet"
      description="Every change made through the API or this console is recorded here."
    />
  );
}
