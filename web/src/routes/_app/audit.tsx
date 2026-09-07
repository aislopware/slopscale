import { Select } from "@cloudflare/kumo/components/select";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";
import { fallback, object, optional, picklist, string } from "valibot";

import { auditQuery, auditRanges, usersQuery } from "~/api/queries.ts";
import type { AuditFilters, AuditRange, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { EventsTable } from "~/components/audit/events-table.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { Card } from "~/components/ui/card.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { userLabel } from "~/lib/node.ts";

const defaultRange: AuditRange = "7d";

const optionalText = optional(string(), "");
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

const rangeOptions: readonly { value: AuditRange; label: string }[] = [
  { value: "1h", label: "Last hour" },
  { value: "24h", label: "Last 24 hours" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "all", label: "All time" },
];

function filtersOf(search: AuditSearch): AuditFilters {
  return { action: search.action, actorUserId: search.actor, range: search.since };
}

/** Whether anything narrows the list, so an empty page means "nothing matched", not "nothing yet". */
function isFiltered(search: AuditSearch): boolean {
  return search.action !== "" || search.actor !== "" || search.since !== "all";
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
      <Card>
        <div className="flex flex-wrap items-center gap-2 border-b border-kumo-line px-5 py-3">
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
          {users.data === undefined ? null : (
            <Select
              aria-label="Filter by user"
              className="w-48"
              value={search.actor}
              items={userOptions(users.data.users)}
              onValueChange={(value) => {
                void navigate({ search: (previous) => ({ ...previous, actor: value ?? "" }) });
              }}
            />
          )}
          <Select
            aria-label="Filter by time"
            className="w-40"
            value={search.since}
            items={rangeOptions}
            onValueChange={(value) => {
              void navigate({
                search: (previous) => ({ ...previous, since: value ?? defaultRange }),
              });
            }}
          />
          <span className="text-sm text-kumo-subtle">
            An action ending in a dot is a prefix:{" "}
            <span className="font-mono text-[0.9em]">node.</span> keeps every node action.
          </span>
        </div>
        <EventsTable
          events={rows}
          filtered={isFiltered(search)}
          hasMore={events.hasNextPage}
          loadingMore={events.isFetchingNextPage}
          onLoadMore={() => {
            void events.fetchNextPage();
          }}
        />
      </Card>
    </>
  );
}

function userOptions(users: readonly User[]): { value: string; label: string }[] {
  return [
    { value: "", label: "Any user" },
    ...users.map((user) => ({ value: user.id, label: userLabel(user) })),
  ];
}
