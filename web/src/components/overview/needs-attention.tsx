import { Button } from "@cloudflare/kumo/components/button";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import type { AccessRequest, Group, Node, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { groupName } from "~/components/access/model.ts";
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

/** What a row is, said rather than drawn. */
const kindLabels = {
  node: "Machine",
  user: "User",
  request: "Access request",
} as const;

/** The scope each kind of row needs before its Approve button is offered. */
const approveScopes = {
  node: "devices:core",
  user: "users",
  request: "policy_file",
} as const;

interface PendingRow {
  readonly key: string;
  readonly id: string;
  readonly kind: "node" | "user" | "request";
  readonly title: string;
  readonly subtitle: string;
  readonly createdAt: string | null;
}

interface Approvals {
  readonly node: Mutation<"post", "/api/v1/node/{nodeId}/approve">;
  readonly user: Mutation<"post", "/api/v1/user/{id}/approve">;
  readonly request: Mutation<"post", "/api/v1/access-request/{id}/approve">;
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
    // Approved here for the duration the requester asked for; the requests page has the form
    // that grants a different one, with a note.
    request: api.useMutation("post", "/api/v1/access-request/{id}/approve", {
      onSuccess: async () => {
        toast.success("Request approved");
        await invalidate(queryClient, "/api/v1/access-request", "/api/v1/group");
      },
      onError: (error) => {
        toast.error("Could not approve the request", error);
      },
    }),
  };
}

function at(value: string | null): number {
  return parseTime(value)?.getTime() ?? 0;
}

/** What the overview asks about; a caller without a scope passes an empty list. */
export interface Waiting {
  readonly nodes: readonly Node[];
  readonly users: readonly User[];
  readonly requests: readonly AccessRequest[];
  readonly groups: readonly Group[];
}

/** Everything waiting for an administrator, oldest first: it has been blocked the longest. */
export function pendingRows({ nodes, users, requests, groups }: Waiting): PendingRow[] {
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

  for (const request of requests.filter((candidate) => candidate.status === "pending")) {
    rows.push({
      key: `request-${request.id}`,
      id: request.id,
      kind: "request",
      title: `Access to ${groupName(groups, request.groupId)}`,
      subtitle: request.reason === "" ? "No reason given" : request.reason,
      createdAt: request.createdAt,
    });
  }

  return rows.toSorted((left, right) => at(left.createdAt) - at(right.createdAt));
}

function RowTitle({ row }: { readonly row: PendingRow }): ReactElement {
  if (row.kind === "request") {
    return (
      <Link
        to="/policy/requests"
        className="truncate font-medium text-kumo-default hover:text-kumo-link hover:underline focus-visible:underline"
      >
        {row.title}
      </Link>
    );
  }

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
  const allowed = can(me, approveScopes[row.kind]);

  return (
    <SectionRow className="flex items-center justify-between gap-4 py-3">
      <div className="flex min-w-0 flex-col gap-0.5">
        <RowTitle row={row} />
        {/* The kind is said, not drawn: a boxed icon beside every row is the generated-UI template. */}
        <p className="truncate text-xs text-kumo-subtle">
          {kindLabels[row.kind]} · {row.subtitle} · {row.kind === "request" ? "asked" : "added"}{" "}
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
        <span>Nothing is waiting for a decision</span>
        <ApprovalSettingsLink me={me} />
      </FramePanel>
    </Frame>
  );
}

export interface NeedsAttentionProps {
  readonly nodes: readonly Node[];
  readonly users: readonly User[];
  readonly requests: readonly AccessRequest[];
  readonly groups: readonly Group[];
  readonly me: Me;
}

/**
 * What an administrator has to act on. With nothing waiting it drops the card and the heading for a
 * single quiet row: "nothing to do" should not be the loudest thing on the page.
 */
export function NeedsAttention({
  nodes,
  users,
  requests,
  groups,
  me,
}: NeedsAttentionProps): ReactElement {
  const approve = useApprovals();
  const rows = pendingRows({ nodes, users, requests, groups });
  const shown = rows.slice(0, maxRows);
  const busy = approve.node.isPending || approve.user.isPending || approve.request.isPending;

  function submit(row: PendingRow): void {
    if (row.kind === "node") {
      approve.node.mutate({ params: { path: { nodeId: row.id } }, body: { approved: true } });

      return;
    }

    if (row.kind === "user") {
      approve.user.mutate({ params: { path: { id: row.id } }, body: { approved: true } });

      return;
    }

    approve.request.mutate({ params: { path: { id: row.id } }, body: {} });
  }

  if (rows.length === 0) {
    return <AllApproved me={me} />;
  }

  return (
    <Section
      title="Needs attention"
      description="Machines and users cannot reach the tailnet, and requesters cannot reach what they asked for, until these are decided"
      bodyClassName="p-0"
      actions={<ApprovalSettingsLink me={me} />}
    >
      {shown.map((row) => (
        <PendingItem key={row.key} row={row} me={me} pending={busy} onApprove={submit} />
      ))}
      {rows.length > shown.length ? (
        <SectionRow className="py-2.5">
          {/* The overflow is whatever the first six left behind, so it points at the page that
              holds most of it rather than always at the machines. */}
          <Link
            to={rows[maxRows]?.kind === "request" ? "/policy/requests" : "/machines"}
            className="text-kumo-link hover:underline"
          >
            {plural(rows.length - shown.length, "more")} waiting
          </Link>
        </SectionRow>
      ) : null}
    </Section>
  );
}
