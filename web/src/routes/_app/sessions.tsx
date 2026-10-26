import { LinkButton } from "@cloudflare/kumo/components/button";
import { GearSixIcon } from "@phosphor-icons/react";
import { useInfiniteQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { settingsQuery, sshRecordingsQuery } from "~/api/queries.ts";
import type { SSHRecording } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { recordingState } from "~/components/sessions/model.ts";
import { SessionsTable } from "~/components/sessions/table.tsx";
import { tablePageSize } from "~/components/table/app-table.tsx";
import { usePageWindow } from "~/components/table/page-window.ts";
import { TableGroup } from "~/components/table/toolbar.tsx";
import { Code } from "~/components/ui/code.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/sessions")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.infiniteQuery(sshRecordingsQuery),
      context.queryClient.query(settingsQuery),
    ]);
  },
  component: SessionsPage,
});

/**
 * The loaded sessions in a line for the header, the ones still recording and the broken ones in
 * their tone; "or more" while the server has another page.
 */
function Tally({
  recordings,
  more,
}: {
  readonly recordings: readonly SSHRecording[];
  readonly more: boolean;
}): ReactElement {
  const live = recordings.filter((recording) => recordingState(recording) === "recording").length;
  const broken = recordings.filter(
    (recording) => recordingState(recording) === "interrupted",
  ).length;
  const count = `${recordings.length}${more ? "+" : ""}`;

  return (
    <>
      <span>{`${count} ${recordings.length === 1 && !more ? "session" : "sessions"}`}</span>
      {live === 0 ? null : (
        <>
          <span aria-hidden>·</span>
          <span className="text-kumo-warning">{`${live} recording now`}</span>
        </>
      )}
      {broken === 0 ? null : (
        <>
          <span aria-hidden>·</span>
          <span className="text-kumo-danger">{`${broken} interrupted`}</span>
        </>
      )}
    </>
  );
}

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
        description={
          <>
            Terminal recordings of SSH sessions, one asciinema file each. Play a download with{" "}
            <Code>asciinema play</Code>.
          </>
        }
        {...(rows.length === 0
          ? {}
          : { meta: <Tally recordings={rows} more={recordings.hasNextPage} /> })}
        {...(can(me, "feature_settings:read")
          ? {
              actions: (
                <LinkButton href="/settings/tailnet" variant="secondary" icon={GearSixIcon}>
                  Recorder settings
                </LinkButton>
              ),
            }
          : {})}
      />
      <TableGroup>
        <SessionsTable
          recordings={window.rows}
          writable={can(me, "logs:configuration")}
          embeddedRecorder={settings.embeddedRecorder}
          paging={window}
        />
      </TableGroup>
    </>
  );
}
