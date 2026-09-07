import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { GlobeIcon, PathIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { withRouteApproved } from "~/components/networks/model.ts";
import type { RouteRow, RouteStatus } from "~/components/networks/model.ts";
import { useNetworkMutations } from "~/components/networks/mutations.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { StatusDot } from "~/components/ui/status-dot.tsx";
import { toast } from "~/components/ui/toast.ts";
import { nodeName, nodeStatus } from "~/lib/node.ts";

const helper = createAppColumnHelper<RouteRow>();
const iconSize = 14;
const statusOrder: Record<RouteStatus, number> = { pending: 0, approved: 1, stale: 2 };

export const routeColumns = helper.columns([
  helper.accessor((row) => (row.exit ? "exit node" : row.route), {
    id: "route",
    header: "Route",
    enableSorting: true,
    cell: ({ row }) => <RouteCell row={row.original} />,
    meta: { className: "w-[28%] min-w-44" },
  }),
  helper.accessor((row) => nodeName(row.node), {
    id: "machine",
    header: "Machine",
    enableSorting: true,
    cell: ({ row }) => <MachineCell row={row.original} />,
    meta: { className: "min-w-40" },
  }),
  helper.accessor((row) => row.networks.map((network) => network.name).join(" "), {
    id: "networks",
    header: "Network",
    enableSorting: false,
    cell: ({ row }) => <NetworksCell row={row.original} />,
    meta: { className: "hidden md:table-cell" },
  }),
  helper.accessor((row) => statusOrder[row.status], {
    id: "status",
    header: "Status",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <StatusCell status={row.original.status} />,
    meta: { className: "whitespace-nowrap" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const { me } = table.options.meta ?? {};

      return me === undefined ? null : <ApproveCell row={row.original} me={me} />;
    },
    meta: { className: "w-28 text-right" },
  }),
]);

function RouteCell({ row }: { readonly row: RouteRow }): ReactElement {
  return (
    <span className="flex items-center gap-2">
      <span className="flex h-lh items-center text-kumo-subtle">
        {row.exit ? <GlobeIcon size={iconSize} /> : <PathIcon size={iconSize} />}
      </span>
      {row.exit ? (
        <span className="font-medium text-kumo-default">Exit node</span>
      ) : (
        <span className="truncate font-mono text-[0.9em]">{row.route}</span>
      )}
    </span>
  );
}

function MachineCell({ row }: { readonly row: RouteRow }): ReactElement {
  return (
    <Link
      to="/machines/$nodeId"
      params={{ nodeId: row.node.id }}
      className="flex items-center gap-2 hover:underline"
    >
      <StatusDot status={nodeStatus(row.node)} />
      <span className="truncate">{nodeName(row.node)}</span>
    </Link>
  );
}

function NetworksCell({ row }: { readonly row: RouteRow }): ReactElement {
  if (row.networks.length === 0) {
    return <span className="text-kumo-inactive">Manual</span>;
  }

  return (
    <div className="flex flex-wrap gap-1">
      {row.networks.map((network) => (
        <Badge key={network.id} variant={network.enabled ? "secondary" : "outline"}>
          {network.name}
        </Badge>
      ))}
    </div>
  );
}

function StatusCell({ status }: { readonly status: RouteStatus }): ReactElement {
  if (status === "stale") {
    return (
      <Badge appearance="dot" variant="warning">
        No longer advertised
      </Badge>
    );
  }

  return (
    <Badge appearance="dot" variant={status === "approved" ? "success" : "warning"}>
      {status === "approved" ? "Approved" : "Pending"}
    </Badge>
  );
}

/**
 * Approve or reject through the machine's route endpoint. A route a network owns is approved by the
 * network, so its button is off; disable or edit the network instead.
 */
function ApproveCell({ row, me }: { readonly row: RouteRow; readonly me: Me }): ReactElement {
  const { setRoutes } = useNetworkMutations();
  const approved = row.status !== "pending";
  const owned = row.networks.some((network) => network.enabled);

  return (
    <Button
      variant={approved ? "ghost" : "secondary"}
      size="sm"
      disabled={!can(me, "devices:routes") || owned || setRoutes.isPending}
      title={owned ? "Approved by its network" : undefined}
      onClick={() => {
        setRoutes.mutate(
          {
            params: { path: { nodeId: row.node.id } },
            body: { routes: withRouteApproved(row.node, row.route, !approved) },
          },
          {
            onSuccess: () => {
              toast.success(approved ? "Route rejected" : "Route approved");
            },
            onError: (error) => {
              toast.error(errorMessage(error));
            },
          },
        );
      }}
    >
      {approved ? "Reject" : "Approve"}
    </Button>
  );
}
