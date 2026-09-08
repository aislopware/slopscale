import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { logStreamsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { streamState } from "~/components/logstreams/model.ts";
import { LogStreamsTab, countStreams } from "~/components/logstreams/tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { textSearchSchema } from "~/lib/search-text.ts";

export const Route = createFileRoute("/_app/integrations/log-streams")({
  validateSearch: textSearchSchema,
  beforeLoad: ({ context }) => {
    if (!can(context.me, "logs:configuration:read")) {
      throw redirect({ to: "/integrations/webhooks", replace: true });
    }
  },
  loader: async ({ context }) => {
    await context.queryClient.query(logStreamsQuery);
  },
  component: LogStreamsPage,
});

function LogStreamsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { logStreams: streams } = useSuspenseQuery(logStreamsQuery).data;

  const setSearch = (value: string): void => {
    void navigate({
      search: (current) => ({ ...current, q: value === "" ? undefined : value }),
      replace: true,
    });
  };

  const failing = streams.filter((stream) => streamState(stream) === "failed").length;
  const meta =
    failing > 0
      ? `${countStreams(streams.length)} · ${failing} failing`
      : countStreams(streams.length);

  return (
    <>
      <PageHeader
        title="Log streams"
        description="Every audit log entry shipped to a SIEM or log store in batches."
        meta={meta}
      />
      <LogStreamsTab me={me} streams={streams} search={search.q ?? ""} onSearchChange={setSearch} />
    </>
  );
}
