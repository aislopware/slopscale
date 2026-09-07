import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { CheckCircleIcon } from "@phosphor-icons/react";
import { useQueryClient } from "@tanstack/react-query";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import type { Node, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { Card, CardBody, CardDescription, CardHeader, CardTitle } from "~/components/ui/card.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { toast } from "~/components/ui/toast.ts";
import { nodeName, ownerLabel, userLabel } from "~/lib/node.ts";
import { parseTime } from "~/lib/time.ts";

/** Enough to see what is waiting without turning the overview into a list page. */
const maxRows = 8;

/** The empty state's icon, sized for a panel rather than a full page. */
const emptyIconSize = 32;

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

function pendingRows(nodes: readonly Node[], users: readonly User[]): PendingRow[] {
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

  return rows.toSorted((left, right) => at(left.createdAt) - at(right.createdAt)).slice(0, maxRows);
}

function at(value: string | null): number {
  return parseTime(value)?.getTime() ?? 0;
}

export interface PendingCardProps {
  readonly nodes: readonly Node[];
  readonly users: readonly User[];
  readonly me: Me;
}

/** Machines and users waiting for an administrator, with the one action each of them needs. */
export function PendingCard({ nodes, users, me }: PendingCardProps): ReactElement {
  const approve = useApprovals();
  const rows = pendingRows(nodes, users);

  function allowed(row: PendingRow): boolean {
    return row.kind === "node" ? can(me, "devices:core") : can(me, "users");
  }

  function submit(row: PendingRow): void {
    if (row.kind === "node") {
      approve.node.mutate({ params: { path: { nodeId: row.id } }, body: { approved: true } });

      return;
    }

    approve.user.mutate({ params: { path: { id: row.id } }, body: { approved: true } });
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-col gap-1">
          <CardTitle>Waiting for approval</CardTitle>
          <CardDescription>
            These machines and users cannot reach the tailnet until they are approved.
          </CardDescription>
        </div>
      </CardHeader>
      {rows.length === 0 ? (
        <CardBody>
          <Empty
            size="sm"
            icon={<CheckCircleIcon size={emptyIconSize} className="text-kumo-success" />}
            title="Nothing waiting"
          />
        </CardBody>
      ) : (
        <div className="flex flex-col divide-y divide-kumo-line">
          {rows.map((row) => (
            <div key={row.key} className="flex items-center justify-between gap-4 px-5 py-3">
              <div className="flex min-w-0 flex-col gap-0.5">
                <p className="truncate font-medium text-kumo-default">{row.title}</p>
                <p className="truncate text-sm text-kumo-subtle">
                  {row.kind === "node" ? "Machine" : "User"} · {row.subtitle} ·{" "}
                  <RelativeTime value={row.createdAt} />
                </p>
              </div>
              {allowed(row) ? (
                <Button
                  size="sm"
                  variant="primary"
                  onClick={() => {
                    submit(row);
                  }}
                >
                  Approve
                </Button>
              ) : null}
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}
