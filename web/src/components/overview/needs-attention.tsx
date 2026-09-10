import { Button } from "@cloudflare/kumo/components/button";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import type { Node, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { allUsers } from "~/components/overview/links.ts";
import { plural } from "~/components/overview/plural.ts";
import { Frame, FramePanel } from "~/components/ui/frame.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";
import { nodeName, ownerLabel, userLabel } from "~/lib/node.ts";
import { parseTime } from "~/lib/time.ts";

/** Enough to see what is waiting without turning the overview into a list page. */
const maxRows = 6;

interface PendingRow {
  readonly key: string;
  readonly id: string;
  readonly kind: "node" | "user";
  readonly title: string;
  readonly subtitle: string;
  readonly createdAt: string | null;
}

interface Approvals {
  readonly node: Mutation<"post", "/api/v1/node/{nodeId}/approve">;
  readonly user: Mutation<"post", "/api/v1/user/{id}/approve">;
}

function useApprovals(): Approvals {
  const queryClient = useQueryClient();

  return {
    node: api.useMutation("post", "/api/v1/node/{nodeId}/approve", {
      onSuccess: async () => {
        toast.success("Machine approved");
        await invalidate(queryClient, "/api/v1/node");
      },
      onError: (error) => {
        toast.error("Could not approve the machine", error);
      },
    }),
    user: api.useMutation("post", "/api/v1/user/{id}/approve", {
      onSuccess: async () => {
        toast.success("User approved");
        await invalidate(queryClient, "/api/v1/user", "/api/v1/node");
      },
      onError: (error) => {
        toast.error("Could not approve the user", error);
      },
    }),
  };
}

function at(value: string | null): number {
  return parseTime(value)?.getTime() ?? 0;
}

/** Everything waiting for an administrator, oldest first: it has been blocked the longest. */
export function pendingRows(nodes: readonly Node[], users: readonly User[]): PendingRow[] {
  const rows: PendingRow[] = [];

  for (const node of nodes.filter((candidate) => !candidate.approved)) {
    rows.push({
      key: `node-${node.id}`,
      id: node.id,
      kind: "node",
      title: nodeName(node),
      subtitle: ownerLabel(node),
      createdAt: node.createdAt,
    });
  }

  for (const user of users.filter((candidate) => !candidate.approved)) {
    rows.push({
      key: `user-${user.id}`,
      id: user.id,
      kind: "user",
      title: userLabel(user),
      subtitle: user.email === "" ? "No email address" : user.email,
      createdAt: user.createdAt,
    });
  }

  return rows.toSorted((left, right) => at(left.createdAt) - at(right.createdAt));
}

function RowTitle({ row }: { readonly row: PendingRow }): ReactElement {
  if (row.kind === "user") {
    return (
      <Link
        to="/users"
        search={allUsers}
        className="truncate font-medium text-kumo-default hover:text-kumo-link hover:underline focus-visible:underline"
      >
        {row.title}
      </Link>
    );
  }

  return (
    <Link
      to="/machines/$nodeId"
      params={{ nodeId: row.id }}
      className="truncate font-medium text-kumo-default hover:text-kumo-link hover:underline focus-visible:underline"
    >
      {row.title}
    </Link>
  );
}

function PendingItem({
  row,
  me,
  onApprove,
  pending,
}: {
  readonly row: PendingRow;
  readonly me: Me;
  readonly onApprove: (row: PendingRow) => void;
  readonly pending: boolean;
}): ReactElement {
  const allowed = row.kind === "node" ? can(me, "devices:core") : can(me, "users");

  return (
    <SectionRow className="flex items-center justify-between gap-4 py-3">
      <div className="flex min-w-0 flex-col gap-0.5">
        <RowTitle row={row} />
        {/* The kind is said, not drawn: a boxed icon beside every row is the generated-UI template. */}
        <p className="truncate text-xs text-kumo-subtle">
          {row.kind === "node" ? "Machine" : "User"} · {row.subtitle} · added{" "}
          <RelativeTime value={row.createdAt} />
        </p>
      </div>
      {allowed ? (
        <Button
          size="sm"
          variant="primary"
          loading={pending}
          onClick={() => {
            onApprove(row);
          }}
        >
          Approve
        </Button>
      ) : null}
    </SectionRow>
  );
}

/** The link to the switches that decide what waits here, shown to whoever may read them. */
function ApprovalSettingsLink({ me }: { readonly me: Me }): ReactElement | null {
  return can(me, "feature_settings:read") ? (
    <Link to="/settings/tailnet" className="text-kumo-link hover:underline">
      Approval settings
    </Link>
  ) : null;
}

/**
 * Nothing is waiting: one quiet row in a frame of its own, no heading. It keeps the shape of the
 * other frames on the page, band and panel, so it reads as a state of the same list rather than a
 * loose line between two cards.
 */
function AllApproved({ me }: { readonly me: Me }): ReactElement {
  return (
    <Frame>
      <FramePanel className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 px-5 py-3 text-kumo-subtle">
        <span>All machines and users are approved</span>
        <ApprovalSettingsLink me={me} />
      </FramePanel>
    </Frame>
  );
}

export interface NeedsAttentionProps {
  readonly nodes: readonly Node[];
  readonly users: readonly User[];
  readonly me: Me;
}

/**
 * What an administrator has to act on. With nothing waiting it drops the card and the heading for a
 * single quiet row: "nothing to do" should not be the loudest thing on the page.
 */
export function NeedsAttention({ nodes, users, me }: NeedsAttentionProps): ReactElement {
  const approve = useApprovals();
  const rows = pendingRows(nodes, users);
  const shown = rows.slice(0, maxRows);
  const busy = approve.node.isPending || approve.user.isPending;

  function submit(row: PendingRow): void {
    if (row.kind === "node") {
      approve.node.mutate({ params: { path: { nodeId: row.id } }, body: { approved: true } });

      return;
    }

    approve.user.mutate({ params: { path: { id: row.id } }, body: { approved: true } });
  }

  if (rows.length === 0) {
    return <AllApproved me={me} />;
  }

  return (
    <Section
      title="Needs attention"
      description="Nothing here can reach the tailnet until it is approved"
      bodyClassName="p-0"
      actions={<ApprovalSettingsLink me={me} />}
    >
      {shown.map((row) => (
        <PendingItem key={row.key} row={row} me={me} pending={busy} onApprove={submit} />
      ))}
      {rows.length > shown.length ? (
        <SectionRow className="py-2.5">
          <Link to="/machines" className="text-kumo-link hover:underline">
            {plural(rows.length - shown.length, "more request")} waiting
          </Link>
        </SectionRow>
      ) : null}
    </Section>
  );
}
