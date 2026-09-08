import { Select } from "@cloudflare/kumo/components/select";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import type { TabsItem } from "@cloudflare/kumo/components/tabs";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";
import { fallback, object, optional, picklist, pipe, transform, unknown } from "valibot";

import { auditQuery, auditRanges, usersQuery } from "~/api/queries.ts";
import type { AuditFilters, AuditRange, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { EventsTable } from "~/components/audit/events-table.tsx";
import { ExportMenu } from "~/components/audit/export-menu.tsx";
import { AuditStats } from "~/components/audit/stats.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableToolbar } from "~/components/table/toolbar.tsx";
import { Frame } from "~/components/ui/frame.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { userLabel } from "~/lib/node.ts";

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
  // The page is nothing but the log, so a caller without the scope has no reason to be here.
  beforeLoad: ({ context }) => {
    if (!can(context.me, "logs:configuration:read")) {
      throw redirect({ to: "/" });
    }
  },
  loader: async ({ context, deps }) => {
    const events = auditQuery(filtersOf(deps));

    await Promise.all([
      context.queryClient.infiniteQuery(events),
      can(context.me, "users:read") ? context.queryClient.query(usersQuery) : Promise.resolve(),
    ]);
  },
  component: AuditPage,
});

const rangeTabs: TabsItem[] = [
  { value: "1h", label: "1h" },
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
  { value: "all", label: "All" },
];

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

function userOptions(users: readonly User[]): { value: string; label: string }[] {
  return [
    { value: "", label: "Any user" },
    ...users.map((user) => ({ value: user.id, label: userLabel(user) })),
  ];
}

function AuditPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const events = useInfiniteQuery(auditQuery(filtersOf(search)));
  const rows = events.data?.pages.flatMap((page) => page.events) ?? [];

  return (
    <>
      <PageHeader
        title="Audit log"
        description="Who changed what through the API and the console."
      />
      <AuditStats events={rows} />
      <Frame>
        <TableToolbar actions={<ExportMenu filters={filtersOf(search)} />}>
          <SearchInput
            value={search.action}
            placeholder="Filter by action"
            onValueChange={(value) => {
              void navigate({
                search: (previous) => ({ ...previous, action: value }),
                replace: true,
              });
            }}
          />
          <Tabs
            variant="segmented"
            aria-label="Time range"
            tabs={rangeTabs}
            value={search.since}
            onValueChange={(value) => {
              void navigate({
                search: (previous) => ({
                  ...previous,
                  since: isRange(value) ? value : defaultRange,
                }),
              });
            }}
          />
          {users.data === undefined ? null : (
            <Select
              aria-label="Filter by user"
              className="w-44"
              value={search.actor}
              items={userOptions(users.data.users)}
              onValueChange={(value) => {
                void navigate({ search: (previous) => ({ ...previous, actor: value ?? "" }) });
              }}
            />
          )}
        </TableToolbar>
        <EventsTable
          events={rows}
          filtered={isFiltered(search)}
          onClearFilters={() => {
            void navigate({ search: () => ({ ...clearedSearch }) });
          }}
          hasMore={events.hasNextPage}
          loadingMore={events.isFetchingNextPage}
          onLoadMore={() => {
            void events.fetchNextPage();
          }}
        />
      </Frame>
    </>
  );
}
