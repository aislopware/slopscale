import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Table } from "@cloudflare/kumo/components/table";
import { SignOutIcon } from "@phosphor-icons/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import { api } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import { invalidate, sessionsQuery } from "~/api/queries.ts";
import type { ConsoleSession } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { signOut } from "~/auth/session.ts";
import { describeUserAgent } from "~/components/settings/user-agent.ts";
import { TableScroll } from "~/components/table/scroll-panel.tsx";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { frameTableClass, frameTableRowClass, pinnedEdgeClass } from "~/components/ui/frame.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";
import { userLabel } from "~/lib/node.ts";

const actionsIconSize = 18;

/**
 * Every console sign-in that has not expired. The server decides the scope: a caller who may manage
 * users sees the whole tailnet's sessions, a member only their own, so the console shows whatever
 * comes back rather than filtering again.
 */
export function ConsoleSessionsSection({ me }: { readonly me: Me }): ReactElement {
  const sessions = useQuery(sessionsQuery);
  const rows = sessions.data?.sessions ?? [];

  return (
    <Section
      title="Console sessions"
      description="Browsers signed in to this console. Ending a session signs its browser out on its next request."
      actions={<SignOutEverywhere me={me} count={rows.length} />}
      bodyClassName="p-0"
    >
      <SessionsBody
        sessions={sessions.isPending ? undefined : rows}
        error={sessions.isError ? sessions.error : undefined}
      />
    </Section>
  );
}

/**
 * The list itself, separated from the fetch so it can be rendered from fixtures: the rows, or the
 * one line that says why there are none.
 */
export function SessionsBody({
  sessions,
  error,
}: {
  /** Undefined while the list is still on its way. */
  readonly sessions: readonly ConsoleSession[] | undefined;
  /** Whatever the request rejected with, read through {@link errorMessage}. */
  readonly error: unknown;
}): ReactElement {
  if (error !== undefined) {
    return (
      <SectionRow>
        <p className="text-kumo-danger">{errorMessage(error)}</p>
      </SectionRow>
    );
  }

  if (sessions === undefined) {
    return (
      <SectionRow>
        <p className="text-kumo-subtle">Loading sessions…</p>
      </SectionRow>
    );
  }

  if (sessions.length === 0) {
    return (
      <SectionRow>
        <p className="text-kumo-subtle">No console session is open.</p>
      </SectionRow>
    );
  }

  return (
    <TableScroll pinnedRight>
      {(overflowing) => (
        <Table className={frameTableClass}>
          <Table.Header variant="compact" sticky>
            <Table.Row>
              <Table.Head>User</Table.Head>
              <Table.Head>Opened</Table.Head>
              <Table.Head>Last seen</Table.Head>
              <Table.Head>Address</Table.Head>
              <Table.Head>Browser</Table.Head>
              <Table.Head sticky="right" className={cellEdge("w-12", overflowing)}>
                <span className="sr-only">Actions</span>
              </Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {sessions.map((session) => (
              <SessionRow key={session.id} session={session} overflowing={overflowing} />
            ))}
          </Table.Body>
        </Table>
      )}
    </TableScroll>
  );
}

/** The pinned column draws its own edge, but only while something is scrolled behind it. */
function cellEdge(base: string, overflowing: boolean): string {
  return overflowing ? `${base} ${pinnedEdgeClass}` : base;
}

function SessionRow({
  session,
  overflowing,
}: {
  readonly session: ConsoleSession;
  readonly overflowing: boolean;
}): ReactElement {
  const [ending, setEnding] = useState(false);
  const queryClient = useQueryClient();
  // Ending your own session leaves this browser holding a cookie the server no longer knows, so
  // the console signs out for real instead of waiting for the next request to fail.
  const end = api.useMutation("delete", "/api/v1/auth/sessions/{id}", {
    onSuccess: async () => {
      if (session.current) {
        await signOut();

        return;
      }

      toast.success("Session ended");
      await invalidate(queryClient, "/api/v1/auth/sessions");
    },
  });

  return (
    <Table.Row className={frameTableRowClass}>
      <Table.Cell className="text-kumo-default">{userLabel(session.user)}</Table.Cell>
      <Table.Cell className="whitespace-nowrap text-kumo-subtle">
        <RelativeTime value={session.createdAt} />
      </Table.Cell>
      <Table.Cell className="whitespace-nowrap text-kumo-subtle">
        <RelativeTime value={session.lastSeenAt} never="Not since it opened" />
      </Table.Cell>
      <Table.Cell className="font-mono text-[0.9em] text-kumo-subtle">
        {session.remoteAddr === "" ? "Not recorded" : session.remoteAddr}
      </Table.Cell>
      <Table.Cell>
        <span className="flex min-w-0 flex-wrap items-center gap-1.5">
          <span className="truncate text-kumo-default">{describeUserAgent(session.userAgent)}</span>
          {session.current ? <Badge variant="info">This browser</Badge> : null}
        </span>
      </Table.Cell>
      <Table.Cell sticky="right" className={cellEdge("w-12 text-right", overflowing)}>
        <Button
          variant="ghost"
          shape="square"
          size="sm"
          icon={<SignOutIcon size={actionsIconSize} />}
          aria-label={`End the session of ${userLabel(session.user)}`}
          onClick={() => {
            setEnding(true);
          }}
        />
        <ConfirmDialog
          open={ending}
          onOpenChange={setEnding}
          title={session.current ? "End this session?" : "End session?"}
          description={
            session.current
              ? "This browser is signed out and lands back on the sign-in page. Machines and keys are unaffected."
              : `The browser holding this session is signed out on its next request. ${userLabel(session.user)} can sign in again.`
          }
          confirmLabel="End session"
          loading={end.isPending}
          error={end.isError ? errorMessage(end.error) : undefined}
          onConfirm={() => {
            end.mutate(
              { params: { path: { id: session.id } } },
              {
                onSuccess: () => {
                  setEnding(false);
                },
              },
            );
          }}
        />
      </Table.Cell>
    </Table.Row>
  );
}

/**
 * Ends every session of the signed-in user, including this one, so the console signs out with it.
 * The server asks for the `users` scope, so a credential without it is told why rather than left
 * with a button that fails.
 */
function SignOutEverywhere({ me, count }: { readonly me: Me; readonly count: number }): ReactNode {
  const [confirming, setConfirming] = useState(false);
  const end = api.useMutation("delete", "/api/v1/user/{id}/sessions");
  const userId = me.user?.id;

  if (userId === undefined) {
    return null;
  }

  const reason = can(me, "users") ? undefined : "Your credentials may not end sessions";

  return (
    <>
      <DisabledReason reason={reason}>
        <Button
          variant="secondary"
          size="sm"
          icon={SignOutIcon}
          disabled={reason !== undefined || count === 0}
          onClick={() => {
            setConfirming(true);
          }}
        >
          Sign out everywhere
        </Button>
      </DisabledReason>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Sign out everywhere?"
        description="Every browser signed in as you is signed out, this one included. Machines and keys are unaffected."
        confirmLabel="Sign out everywhere"
        loading={end.isPending}
        error={end.isError ? errorMessage(end.error) : undefined}
        onConfirm={() => {
          end.mutate(
            { params: { path: { id: userId } } },
            {
              onSuccess: () => {
                void signOut();
              },
            },
          );
        }}
      />
    </>
  );
}
