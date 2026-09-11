import { DeleteResource } from "@cloudflare/kumo";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Table } from "@cloudflare/kumo/components/table";
import { cn } from "@cloudflare/kumo/utils";
import { DownloadSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { SSHRecording } from "~/api/queries.ts";
import { sshRecordingCastUrl } from "~/api/queries.ts";
import {
  castFileName,
  formatBytes,
  recordingState,
  sessionSource,
  sessionTarget,
} from "~/components/sessions/model.ts";
import type { RecordingState } from "~/components/sessions/model.ts";
import { useDeleteRecording } from "~/components/sessions/mutations.ts";
import { tableEmptyClass } from "~/components/table/empty.ts";
import type { CursorPaging } from "~/components/table/page-window.ts";
import { CursorBand } from "~/components/table/paging.tsx";
import { TableScroll } from "~/components/table/scroll-panel.tsx";
import { Badge } from "~/components/ui/badge.tsx";
import { Code } from "~/components/ui/code.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { frameTableClass, frameTableRowClass, pinnedEdgeClass } from "~/components/ui/frame.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import type { Tone } from "~/components/ui/status.tsx";

const stateTones: Record<RecordingState, { tone: Tone; label: string }> = {
  recording: { tone: "warning", label: "Recording" },
  complete: { tone: "success", label: "Complete" },
  interrupted: { tone: "danger", label: "Interrupted" },
};

function StateBadge({ recording }: { readonly recording: SSHRecording }): ReactElement {
  const badge = stateTones[recordingState(recording)];

  return <Badge tone={badge.tone}>{badge.label}</Badge>;
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
      <Table.Cell className="font-mono text-sm whitespace-nowrap">
        {sessionTarget(recording)}
      </Table.Cell>
      <Table.Cell className="max-w-64 truncate font-mono text-sm text-kumo-subtle">
        {recording.command === "" ? "shell" : recording.command}
      </Table.Cell>
      <Table.Cell className="whitespace-nowrap text-kumo-subtle">
        {formatBytes(recording.size)}
      </Table.Cell>
      <Table.Cell>
        <StateBadge recording={recording} />
      </Table.Cell>
      <Table.Cell sticky="right" className={cn("w-12 text-right", overflowing && pinnedEdgeClass)}>
        <RowMenu label={`Actions for the recording of ${sessionTarget(recording)}`}>
          <DropdownMenu.Item
            // A rendered item drops the item's own icon and children, so the link carries both.
            render={
              <a href={sshRecordingCastUrl(recording.id)} download={castFileName(recording)}>
                <DownloadSimpleIcon className="mr-2 size-4" />
                Download
              </a>
            }
          />
          <DropdownMenu.Separator />
          <DisabledReason
            reason={writable ? undefined : "Your credentials may not delete recordings"}
          >
            <DropdownMenu.Item
              icon={TrashIcon}
              variant="danger"
              disabled={!writable}
              onClick={() => {
                setDeleting(true);
              }}
            >
              Delete…
            </DropdownMenu.Item>
          </DisabledReason>
        </RowMenu>
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
  readonly paging: CursorPaging;
}

/** Recorded SSH sessions, newest first and server-paged; each row downloads or deletes its file. */
export function SessionsTable({
  recordings,
  writable,
  embeddedRecorder,
  paging,
}: SessionsTableProps): ReactElement {
  return (
    <>
      <TableScroll
        pinnedRight
        below={
          recordings.length === 0 ? <EmptySessions embeddedRecorder={embeddedRecorder} /> : null
        }
      >
        {(overflowing) => (
          <Table className={frameTableClass}>
            <Table.Header variant="compact">
              <Table.Row>
                <Table.Head>Started</Table.Head>
                <Table.Head>From</Table.Head>
                <Table.Head>To</Table.Head>
                <Table.Head>Command</Table.Head>
                <Table.Head>Size</Table.Head>
                <Table.Head>Status</Table.Head>
                <Table.Head sticky="right" className={cn("w-12", overflowing && pinnedEdgeClass)}>
                  <span className="sr-only">Actions</span>
                </Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {recordings.map((recording) => (
                <RecordingRow
                  key={recording.id}
                  recording={recording}
                  writable={writable}
                  overflowing={overflowing}
                />
              ))}
            </Table.Body>
          </Table>
        )}
      </TableScroll>
      <CursorBand paging={paging} noun="recording" />
    </>
  );
}

function EmptySessions({ embeddedRecorder }: { readonly embeddedRecorder: boolean }): ReactElement {
  return (
    <Empty
      className={tableEmptyClass}
      size="sm"
      title="No recorded sessions"
      contents={
        <p className="max-w-140 text-center text-kumo-subtle">
          {embeddedRecorder ? (
            "Sessions covered by an SSH rule appear here once the recorder has them."
          ) : (
            <>
              No recorder is set. Turn on <Code>ssh_recording</Code> in the server config, or name a
              recorder under Settings › Tailnet.
            </>
          )}
        </p>
      }
    />
  );
}
