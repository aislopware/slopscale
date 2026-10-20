import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { ArrowClockwiseIcon, ClockIcon } from "@phosphor-icons/react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import type { ReactElement } from "react";
import { fallback, object, optional, picklist, pipe, transform, unknown } from "valibot";

import { auditPageSize, auditQuery, auditRanges, usersQuery } from "~/api/queries.ts";
import type { AuditFilters, AuditRange, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { ColumnsMenu, useAuditColumns } from "~/components/audit/columns.tsx";
import { EventsTable } from "~/components/audit/events-table.tsx";
import { ExportMenu } from "~/components/audit/export-menu.tsx";
import { AuditStats } from "~/components/audit/stats.tsx";
import { tablePageSize } from "~/components/table/app-table.tsx";
import { LogFilters } from "~/components/table/log-filters.tsx";
import type { LogFilter } from "~/components/table/log-filters.tsx";
import { usePageWindow } from "~/components/table/page-window.ts";
import { smallestPageSize } from "~/components/table/paging.tsx";
import { TableGroup } from "~/components/table/toolbar.tsx";
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

/** The time range button's choices, as the Zero Trust logs word them. */
const rangeLabels: Record<AuditRange, string> = {
  "1h": "Last hour",
  "24h": "Last 24 hours",
  "7d": "Last 7 days",
  "30d": "Last 30 days",
  all: "All time",
};

/**
 * The rows a page of the table holds, the log toolbar's "Limit": up to one page of what the server
 * sends at a time, so stepping on always shows a whole page.
 */
const limits: readonly number[] = [smallestPageSize, tablePageSize, auditPageSize];

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

/** The action box's own text, reset whenever the address changes it from outside. */
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

/** The time range as the log toolbar's clock button. */
function RangeSelect({
  since,
  onChange,
}: {
  readonly since: AuditRange;
  readonly onChange: (since: AuditRange) => void;
}): ReactElement {
  return (
    <Select
      aria-label="Time range"
      className="w-40"
      value={since}
      renderValue={(value) => (
        <span className="flex items-center gap-2">
          <ClockIcon aria-hidden className="size-4 text-kumo-subtle" />
          {isRange(value) ? rangeLabels[value] : rangeLabels[defaultRange]}
        </span>
      )}
      onValueChange={(value) => {
        onChange(typeof value === "string" && isRange(value) ? value : defaultRange);
      }}
    >
      {auditRanges.map((range) => (
        <Select.Option key={range} value={range}>
          {rangeLabels[range]}
        </Select.Option>
      ))}
    </Select>
  );
}

/** "Limit: 50", the rows one page of the table holds. */
function LimitSelect({
  value,
  onChange,
}: {
  readonly value: number;
  readonly onChange: (size: number) => void;
}): ReactElement {
  return (
    <Select
      aria-label="Rows per page"
      className="w-20 sm:w-28"
      value={value}
      renderValue={(size) => (
        <span>
          <span className="max-sm:hidden">Limit: </span>
          {String(size)}
        </span>
      )}
      onValueChange={(size) => {
        if (typeof size === "number") {
          onChange(size);
        }
      }}
    >
      {limits.map((size) => (
        <Select.Option key={size} value={size}>
          {size}
        </Select.Option>
      ))}
    </Select>
  );
}

interface AuditFilterRows {
  /** The rows of the Filters popover. */
  readonly filters: LogFilter[];
  /** Drops the action and the user, keeping the range. */
  readonly onClear: () => void;
  /** Puts every control back where the page starts, the range included. */
  readonly onReset: () => void;
}

/**
 * The audit log's filters as the popover's rows: the action prefix, typed, and the user, picked
 * when the caller may list users. The typed prefix reaches the address, and so the server, once it
 * stops changing.
 */
function useAuditFilters(users: readonly User[] | undefined): AuditFilterRows {
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const [actionDraft, setActionDraft] = useDraft(search.action);
  const settledAction = useDebounced(actionDraft, actionSettleMs);

  // A draft that is still moving stays local, so clearing the filters elsewhere cannot be undone by
  // a stale one.
  useEffect(() => {
    if (settledAction === actionDraft && settledAction !== search.action) {
      void navigate({
        search: (previous) => ({ ...previous, action: settledAction }),
        replace: true,
      });
    }
  }, [settledAction, actionDraft, search.action, navigate]);

  const filters: LogFilter[] = [
    {
      id: "action",
      label: "Action",
      operator: "starts with",
      value: (
        <Input
          aria-label="Action starts with"
          placeholder="node. or node.delete"
          value={actionDraft}
          onChange={(event) => {
            setActionDraft(event.target.value);
          }}
        />
      ),
      active: search.action !== "",
      addable: true,
      onRemove: () => {
        setActionDraft("");
        void navigate({ search: (previous) => ({ ...previous, action: "" }) });
      },
    },
  ];

  if (users !== undefined || search.actor !== "") {
    filters.push({
      id: "actor",
      label: "User",
      operator: "is",
      value: (
        <Select
          aria-label="User is"
          className="w-full min-w-0"
          value={search.actor}
          items={userOptions(users ?? [])}
          onValueChange={(value) => {
            void navigate({ search: (previous) => ({ ...previous, actor: value ?? "" }) });
          }}
        />
      ),
      active: search.actor !== "",
      addable: users !== undefined,
      onRemove: () => {
        void navigate({ search: (previous) => ({ ...previous, actor: "" }) });
      },
    });
  }

  return {
    filters,
    onClear: () => {
      setActionDraft("");
      void navigate({ search: (previous) => ({ ...previous, action: "", actor: "" }) });
    },
    onReset: () => {
      setActionDraft("");
      void navigate({ search: () => ({ ...clearedSearch }) });
    },
  };
}

function AuditPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const filters = filtersOf(search);
  const events = useInfiniteQuery(auditQuery(filters));
  const columns = useAuditColumns();
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
  const logFilters = useAuditFilters(users.data?.users);

  return (
    <>
      <PageHeader
        title="Audit log"
        docs="ref/audit"
        description="Who changed what through the API and the console."
        actions={<ExportMenu filters={filtersOf(search)} />}
      />
      <AuditStats events={rows} />
      <TableGroup>
        {/* The Zero Trust log toolbar, right-aligned over the table it narrows: the filters with
            their count and the × that clears them, the time range, the page size and the columns,
            then a refresh. */}
        <div className="flex flex-wrap items-center justify-end gap-2">
          <LogFilters
            filters={logFilters.filters}
            onClear={() => {
              logFilters.onClear();
            }}
          />
          <RangeSelect
            since={search.since}
            onChange={(since) => {
              void navigate({ search: (previous) => ({ ...previous, since }) });
            }}
          />
          <LimitSelect
            value={window.pageSize}
            onChange={(size) => {
              window.setPageSize(size);
            }}
          />
          <ColumnsMenu columns={columns} />
          {/* A phone keeps the toolbar on one line without it; the list refetches when the tab
              comes back into focus anyway. */}
          <Button
            className="max-sm:hidden"
            variant="secondary"
            shape="square"
            icon={ArrowClockwiseIcon}
            aria-label="Refresh"
            loading={events.isRefetching && !events.isFetchingNextPage}
            onClick={() => {
              void events.refetch();
            }}
          />
        </div>
        <EventsTable
          events={window.rows}
          filtered={isFiltered(search)}
          onClearFilters={() => {
            logFilters.onReset();
          }}
          paging={window}
          hidden={columns.hidden}
          order={columns.order}
          onMove={(column, to) => {
            columns.move(column, to);
          }}
        />
      </TableGroup>
    </>
  );
}
