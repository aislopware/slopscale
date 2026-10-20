import { Button } from "@cloudflare/kumo/components/button";
import { Select } from "@cloudflare/kumo/components/select";
import { Toolbar } from "@cloudflare/kumo/components/toolbar";
import { cn } from "@cloudflare/kumo/utils";
import { ArrowClockwiseIcon, CalendarBlankIcon, XIcon } from "@phosphor-icons/react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import type { ReactElement } from "react";
import { fallback, object, optional, picklist, pipe, transform, unknown } from "valibot";

import { auditQuery, auditRanges, usersQuery } from "~/api/queries.ts";
import type { AuditFilters, AuditRange, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { ColumnsMenu, useHiddenColumns } from "~/components/audit/columns.tsx";
import { EventsTable } from "~/components/audit/events-table.tsx";
import { ExportMenu } from "~/components/audit/export-menu.tsx";
import { AuditStats } from "~/components/audit/stats.tsx";
import { tablePageSize } from "~/components/table/app-table.tsx";
import { usePageWindow } from "~/components/table/page-window.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableGroup } from "~/components/table/toolbar.tsx";
import { Badge } from "~/components/ui/badge.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { userLabel } from "~/lib/node.ts";
import { useDebounced } from "~/lib/use-debounced.ts";

const defaultRange: AuditRange = "7d";

/** Clearing the filters puts every control back where the page starts. */
const clearedSearch = { action: "", actor: "", since: defaultRange } as const;

/**
 * The router parses a search value as JSON, so `?actor=2` arrives as the number 2 while the user id
 * it names is a string. Reading it through this keeps the digits instead of dropping the filter.
 */
function toText(value: unknown): string {
  if (typeof value === "string") {
    return value;
  }

  return typeof value === "number" ? String(value) : "";
}

const optionalText = optional(pipe(unknown(), transform(toText)), "");
const optionalRange = optional(picklist(auditRanges), defaultRange);

const searchSchema = object({
  action: fallback(optionalText, ""),
  actor: fallback(optionalText, ""),
  since: fallback(optionalRange, defaultRange),
});

interface AuditSearch {
  readonly action: string;
  readonly actor: string;
  readonly since: AuditRange;
}

export const Route = createFileRoute("/_app/audit")({
  validateSearch: searchSchema,
  loaderDeps: ({ search }) => search,
  loader: async ({ context, deps }) => {
    const events = auditQuery(filtersOf(deps));

    await Promise.all([
      context.queryClient.infiniteQuery(events),
      can(context.me, "users:read") ? context.queryClient.query(usersQuery) : Promise.resolve(),
    ]);
  },
  component: AuditPage,
});

/** The date-range button's choices, as the dashboard's audit log words them. */
const rangeLabels: Record<AuditRange, string> = {
  "1h": "Last hour",
  "24h": "Last 24 hours",
  "7d": "Last 7 days",
  "30d": "Last 30 days",
  all: "All time",
};

/**
 * A Kumo Select as a segment of a Toolbar group: the group draws the ring and the dividers, so the
 * select drops its own.
 */
const joinedSelectClass = "rounded-none bg-transparent shadow-none ring-0";

function isRange(value: string): value is AuditRange {
  return (auditRanges as readonly string[]).includes(value);
}

function filtersOf(search: AuditSearch): AuditFilters {
  return { action: search.action, actorUserId: search.actor, range: search.since };
}

/** Whether anything narrows the list, so an empty page means "nothing matched", not "nothing yet". */
function isFiltered(search: AuditSearch): boolean {
  return search.action !== "" || search.actor !== "" || search.since !== "all";
}

const actionSettleMs = 300;

/** The search box's own text, reset whenever the address changes it from outside. */
function useDraft(fromSearch: string): [string, (next: string) => void] {
  const [draft, setDraft] = useState(fromSearch);
  const [seen, setSeen] = useState(fromSearch);

  if (seen !== fromSearch) {
    setSeen(fromSearch);
    setDraft(fromSearch);
  }

  return [draft, setDraft];
}

function userOptions(users: readonly User[]): { value: string; label: string }[] {
  return [
    { value: "", label: "Any user" },
    ...users.map((user) => ({ value: user.id, label: userLabel(user) })),
  ];
}

/** The log toolbar's filter count: how many filters narrow the list, and the × that drops them all. */
function FilterCount({
  count,
  onClear,
}: {
  readonly count: number;
  readonly onClear: () => void;
}): ReactElement | null {
  if (count === 0) {
    return null;
  }

  return (
    <Button
      variant="ghost"
      size="sm"
      aria-label={`Clear ${String(count)} ${count === 1 ? "filter" : "filters"}`}
      onClick={onClear}
    >
      Filters
      <Badge tone="neutral">{count}</Badge>
      <XIcon aria-hidden className="size-3.5 text-kumo-subtle" />
    </Button>
  );
}

/** The date-range button with its calendar icon, the Columns chooser and the refresh. */
function RangeControls({
  since,
  refreshing,
  columns,
  onSinceChange,
  onRefresh,
}: {
  readonly since: AuditRange;
  readonly refreshing: boolean;
  readonly columns: ReactElement;
  readonly onSinceChange: (since: AuditRange) => void;
  readonly onRefresh: () => void;
}): ReactElement {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Select
        aria-label="Time range"
        className="w-44"
        value={since}
        renderValue={(value) => (
          <span className="flex items-center gap-2">
            <CalendarBlankIcon aria-hidden className="size-4 text-kumo-subtle" />
            {isRange(value) ? rangeLabels[value] : rangeLabels[defaultRange]}
          </span>
        )}
        onValueChange={(value) => {
          onSinceChange(typeof value === "string" && isRange(value) ? value : defaultRange);
        }}
      >
        {auditRanges.map((range) => (
          <Select.Option key={range} value={range}>
            {rangeLabels[range]}
          </Select.Option>
        ))}
      </Select>
      {columns}
      <Button
        variant="secondary"
        shape="square"
        icon={ArrowClockwiseIcon}
        aria-label="Refresh"
        loading={refreshing}
        onClick={onRefresh}
      />
    </div>
  );
}

function AuditPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const filters = filtersOf(search);
  const [actionDraft, setActionDraft] = useDraft(search.action);
  const settledAction = useDebounced(actionDraft, actionSettleMs);
  const events = useInfiniteQuery(auditQuery(filters));

  // The typed prefix reaches the address, and so the server, once it stops changing. A draft that
  // is still moving stays local, so clearing the filters elsewhere cannot be undone by a stale one.
  useEffect(() => {
    if (settledAction === actionDraft && settledAction !== search.action) {
      void navigate({
        search: (previous) => ({ ...previous, action: settledAction }),
        replace: true,
      });
    }
  }, [settledAction, actionDraft, search.action, navigate]);
  const rows = events.data?.pages.flatMap((page) => page.events) ?? [];
  const window = usePageWindow({
    rows,
    pageSize: tablePageSize,
    hasMore: events.hasNextPage,
    fetching: events.isFetchingNextPage,
    failed: events.isFetchNextPageError,
    fetchMore: () => {
      void events.fetchNextPage();
    },
    resetKey: JSON.stringify(filters),
  });

  const filterCount = (search.action === "" ? 0 : 1) + (search.actor === "" ? 0 : 1);
  const [hidden, toggleColumn] = useHiddenColumns();

  return (
    <>
      <PageHeader
        title="Audit log"
        docs="ref/audit"
        description="Who changed what through the API and the console."
        actions={<ExportMenu filters={filtersOf(search)} />}
      />
      <TableGroup className="gap-4">
        {/* The dashboard's audit filter row: the filters joined in one ringed group on the left
            with their count and the × that clears them; the date range, the Columns chooser and a refresh
            on the right. The per-page limit sits in the band under the table. */}
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-0 flex-1 basis-80 flex-wrap items-center gap-2">
            <Toolbar aria-label="Filters" className="w-full max-w-[560px]">
              <SearchInput
                joined
                value={actionDraft}
                placeholder="Filter by action prefix"
                onValueChange={setActionDraft}
              />
              {users.data === undefined ? null : (
                <Select
                  aria-label="Filter by user"
                  className={cn(joinedSelectClass, "w-44")}
                  value={search.actor}
                  items={userOptions(users.data.users)}
                  onValueChange={(value) => {
                    void navigate({ search: (previous) => ({ ...previous, actor: value ?? "" }) });
                  }}
                />
              )}
            </Toolbar>
            <FilterCount
              count={filterCount}
              onClear={() => {
                void navigate({ search: (previous) => ({ ...previous, action: "", actor: "" }) });
              }}
            />
          </div>
          <RangeControls
            since={search.since}
            refreshing={events.isRefetching && !events.isFetchingNextPage}
            columns={<ColumnsMenu hidden={hidden} onToggle={toggleColumn} />}
            onSinceChange={(since) => {
              void navigate({ search: (previous) => ({ ...previous, since }) });
            }}
            onRefresh={() => {
              void events.refetch();
            }}
          />
        </div>
        <AuditStats events={rows} />
      </TableGroup>
      <TableGroup>
        <EventsTable
          events={window.rows}
          filtered={isFiltered(search)}
          onClearFilters={() => {
            void navigate({ search: () => ({ ...clearedSearch }) });
          }}
          paging={window}
          hidden={hidden}
        />
      </TableGroup>
    </>
  );
}
