import { DeleteResource } from "@cloudflare/kumo";
import { Badge } from "@cloudflare/kumo/components/badge";
import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Table } from "@cloudflare/kumo/components/table";
import { cn } from "@cloudflare/kumo/utils";
import { DownloadSimpleIcon, TerminalWindowIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import { errorMessage } from "~/api/error.ts";
import type { SSHRecording } from "~/api/queries.ts";
import { sshRecordingCastUrl } from "~/api/queries.ts";
import { plural } from "~/components/overview/plural.ts";
import {
  castFileName,
  formatBytes,
  recordingState,
  sessionSource,
  sessionTarget,
} from "~/components/sessions/model.ts";
import type { RecordingState } from "~/components/sessions/model.ts";
import { useDeleteRecording } from "~/components/sessions/mutations.ts";
import { emptyIconSize, tableEmptyClass } from "~/components/table/empty.ts";
import { TableScroll } from "~/components/table/scroll-panel.tsx";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { frameTableClass, frameTableRowClass, pinnedEdgeClass } from "~/components/ui/frame.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";

const columnCount = 7;

const stateBadges: Record<
  RecordingState,
  { variant: "success" | "warning" | "error"; label: string }
> = {
  recording: { variant: "warning", label: "Recording" },
  complete: { variant: "success", label: "Complete" },
  interrupted: { variant: "error", label: "Interrupted" },
};

function StateBadge({ recording }: { readonly recording: SSHRecording }): ReactElement {
  const badge = stateBadges[recordingState(recording)];

  return <Badge variant={badge.variant}>{badge.label}</Badge>;
}

function RecordingRow({
  recording,
  writable,
  overflowing,
}: {
  readonly recording: SSHRecording;
  readonly writable: boolean;
  /** Whether the columns run past the panel, so the pinned last column draws its edge. */
  readonly overflowing: boolean;
}): ReactElement {
  const [deleting, setDeleting] = useState(false);
  const remove = useDeleteRecording();

  return (
    <Table.Row className={cn("align-top", frameTableRowClass)}>
      <Table.Cell className="whitespace-nowrap text-kumo-subtle">
        <RelativeTime value={recording.startedAt} />
      </Table.Cell>
      <Table.Cell>{sessionSource(recording)}</Table.Cell>
      <Table.Cell className="font-mono text-sm">{sessionTarget(recording)}</Table.Cell>
      <Table.Cell className="max-w-64 truncate font-mono text-sm text-kumo-subtle">
        {recording.command === "" ? "shell" : recording.command}
      </Table.Cell>
      <Table.Cell className="whitespace-nowrap text-kumo-subtle">
        {formatBytes(recording.size)}
      </Table.Cell>
      <Table.Cell>
        <StateBadge recording={recording} />
      </Table.Cell>
      <Table.Cell
        sticky="right"
        className={cn("w-24 text-right whitespace-nowrap", overflowing && pinnedEdgeClass)}
      >
        <LinkButton
          variant="ghost"
          shape="square"
          size="sm"
          icon={DownloadSimpleIcon}
          aria-label={`Download ${castFileName(recording)}`}
          href={sshRecordingCastUrl(recording.id)}
          download={castFileName(recording)}
          linksExternal
        />
        <Button
          variant="ghost"
          shape="square"
          size="sm"
          icon={TrashIcon}
          aria-label="Delete recording"
          disabled={!writable}
          onClick={() => {
            setDeleting(true);
          }}
        />
        <DeleteResource
          open={deleting}
          onOpenChange={setDeleting}
          resourceType="recording"
          resourceName={`${sessionTarget(recording)} from ${sessionSource(recording)}`}
          deleteButtonText="Delete recording"
          isDeleting={remove.isPending}
          {...(remove.isError ? { errorMessage: errorMessage(remove.error) } : {})}
          onDelete={() => {
            remove.mutate(
              { params: { path: { id: recording.id } } },
              {
                onSuccess: () => {
                  setDeleting(false);
                },
              },
            );
          }}
        />
      </Table.Cell>
    </Table.Row>
  );
}

export interface SessionsTableProps {
  readonly recordings: readonly SSHRecording[];
  readonly writable: boolean;
  /** Whether the server runs the embedded recorder, which changes what an empty list means. */
  readonly embeddedRecorder: boolean;
  readonly hasMore: boolean;
  readonly loadingMore: boolean;
  readonly onLoadMore: () => void;
}

/** Recorded SSH sessions, newest first and server-paged; each row downloads or deletes its file. */
export function SessionsTable({
  recordings,
  writable,
  embeddedRecorder,
  hasMore,
  loadingMore,
  onLoadMore,
}: SessionsTableProps): ReactElement {
  return (
    <>
      <TableScroll pinnedRight>
        {(overflowing) => (
          <Table className={frameTableClass}>
            <Table.Header variant="compact" sticky>
              <Table.Row>
                <Table.Head>Started</Table.Head>
                <Table.Head>From</Table.Head>
                <Table.Head>Session</Table.Head>
                <Table.Head>Command</Table.Head>
                <Table.Head>Size</Table.Head>
                <Table.Head>State</Table.Head>
                <Table.Head sticky="right" className={cn("w-24", overflowing && pinnedEdgeClass)}>
                  <span className="sr-only">Actions</span>
                </Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {recordings.length === 0 ? (
                <Table.Row>
                  <Table.Cell colSpan={columnCount} className="p-0">
                    <EmptySessions embeddedRecorder={embeddedRecorder} />
                  </Table.Cell>
                </Table.Row>
              ) : (
                recordings.map((recording) => (
                  <RecordingRow
                    key={recording.id}
                    recording={recording}
                    writable={writable}
                    overflowing={overflowing}
                  />
                ))
              )}
            </Table.Body>
          </Table>
        )}
      </TableScroll>
      <Paging
        count={recordings.length}
        hasMore={hasMore}
        loadingMore={loadingMore}
        onLoadMore={onLoadMore}
      />
    </>
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
      <TableFooter
        actions={
          <Button variant="secondary" size="xs" loading={loadingMore} onClick={onLoadMore}>
            Load more
          </Button>
        }
      >
        {`Showing ${plural(count, "recording")}`}
      </TableFooter>
    );
  }

  return <TableFooter>{`Showing all ${plural(count, "recording")}`}</TableFooter>;
}

function EmptySessions({ embeddedRecorder }: { readonly embeddedRecorder: boolean }): ReactElement {
  return (
    <Empty
      className={tableEmptyClass}
      size="sm"
      icon={<TerminalWindowIcon size={emptyIconSize} />}
      title="No recorded sessions"
      description={
        embeddedRecorder
          ? "Sessions on machines with an SSH rule land here once the recorder receives them."
          : "Turn on ssh_recording in the server config, or name a recorder under Settings, and sessions land here."
      }
    />
  );
}
