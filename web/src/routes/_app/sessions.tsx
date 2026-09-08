import { useInfiniteQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { settingsQuery, sshRecordingsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { SessionsTable } from "~/components/sessions/table.tsx";
import { tablePageSize } from "~/components/table/app-table.tsx";
import { usePageWindow } from "~/components/table/page-window.ts";
import { Frame } from "~/components/ui/frame.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/sessions")({
  // The page is nothing but the recordings, so a caller without the scope has no reason to be here.
  beforeLoad: ({ context }) => {
    if (!can(context.me, "logs:configuration:read")) {
      throw redirect({ to: "/" });
    }
  },
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.infiniteQuery(sshRecordingsQuery),
      context.queryClient.query(settingsQuery),
    ]);
  },
  component: SessionsPage,
});

function SessionsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const settings = useSuspenseQuery(settingsQuery).data;
  const recordings = useInfiniteQuery(sshRecordingsQuery);
  const rows = recordings.data?.pages.flatMap((page) => page.recordings) ?? [];
  const window = usePageWindow({
    rows,
    pageSize: tablePageSize,
    hasMore: recordings.hasNextPage,
    fetching: recordings.isFetchingNextPage,
    failed: recordings.isFetchNextPageError,
    fetchMore: () => {
      void recordings.fetchNextPage();
    },
  });

  return (
    <>
      <PageHeader
        title="SSH sessions"
        description="Terminal recordings of SSH sessions, one asciinema file each. Play a download with asciinema play."
      />
      <Frame>
        <SessionsTable
          recordings={window.rows}
          writable={can(me, "logs:configuration")}
          embeddedRecorder={settings.embeddedRecorder}
          paging={window}
        />
      </Frame>
    </>
  );
}
