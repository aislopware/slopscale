import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { useInfiniteQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { settingsQuery, sshRecordingsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { SessionsTable } from "~/components/sessions/table.tsx";
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

  return (
    <>
      <PageHeader
        title="SSH sessions"
        description="Terminal recordings of SSH sessions, one asciinema file each. Play a download with asciinema play."
      />
      <LayerCard className="overflow-clip p-0">
        <SessionsTable
          recordings={rows}
          writable={can(me, "logs:configuration")}
          embeddedRecorder={settings.embeddedRecorder}
          hasMore={recordings.hasNextPage}
          loadingMore={recordings.isFetchingNextPage}
          onLoadMore={() => {
            void recordings.fetchNextPage();
          }}
        />
      </LayerCard>
    </>
  );
}
