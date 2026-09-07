import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { ClockCounterClockwiseIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { AuditEvent } from "~/api/queries.ts";
import { columns } from "~/components/audit/columns.tsx";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";

const emptyIconSize = 40;
/** The table already draws the card edge, so the empty panel drops its own. */
const emptyClass = "border-none bg-kumo-base";

export interface EventsTableProps {
  readonly events: readonly AuditEvent[];
  /**
   * True when a filter is set: the list is empty because of the filters, not because nothing
   * happened.
   */
  readonly filtered: boolean;
  /** The server has at least one more page for these filters. */
  readonly hasMore: boolean;
  readonly loadingMore: boolean;
  readonly onLoadMore: () => void;
}

/**
 * The audit list: server-ordered (newest first) and server-paged, so the table neither sorts nor
 * filters; "Load more" appends the next page.
 */
export function EventsTable({
  events,
  filtered,
  hasMore,
  loadingMore,
  onLoadMore,
}: EventsTableProps): ReactElement {
  const table = useAppTable({
    data: events,
    columns,
    getRowId: (event) => event.id,
  });

  return (
    <>
      <table.AppTable>
        <DataTable empty={<EmptyEvents filtered={filtered} />} rowClassName="align-top" />
      </table.AppTable>
      {hasMore ? (
        <div className="flex justify-center border-t border-kumo-line px-5 py-3">
          <Button variant="secondary" loading={loadingMore} onClick={onLoadMore}>
            Load more
          </Button>
        </div>
      ) : null}
    </>
  );
}

function EmptyEvents({ filtered }: { readonly filtered: boolean }): ReactElement {
  if (filtered) {
    return (
      <Empty
        className={emptyClass}
        title="No events match"
        description="Try a wider time range or a different action."
        size="sm"
      />
    );
  }

  return (
    <Empty
      className={emptyClass}
      icon={<ClockCounterClockwiseIcon size={emptyIconSize} />}
      title="No events yet"
      description="Every change made through the API or this console is recorded here."
    />
  );
}
