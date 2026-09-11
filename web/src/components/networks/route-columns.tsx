import { Button } from "@cloudflare/kumo/components/button";
import { cn } from "@cloudflare/kumo/utils";
import { CaretDownIcon, CaretRightIcon, GlobeIcon, PathIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Node } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { withRouteApproved } from "~/components/networks/model.ts";
import type { RouteStatus } from "~/components/networks/model.ts";
import { useNetworkMutations } from "~/components/networks/mutations.ts";
import {
  approvableAdvertisers,
  groupStatus,
  isRouteGroup,
} from "~/components/networks/routes-model.ts";
import type { RouteAdvertiser, RouteGroup, RoutesRow } from "~/components/networks/routes-model.ts";
import { plural } from "~/components/overview/plural.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { Status } from "~/components/ui/status.tsx";
import { toast } from "~/components/ui/toast.ts";
import { ValueList } from "~/components/ui/value-list.tsx";
import { nodeName } from "~/lib/node.ts";

const helper = createAppColumnHelper<RoutesRow>();
const iconSize = 14;
const caretSize = 12;
const statusOrder: Record<RouteStatus, number> = { pending: 0, approved: 1, stale: 2 };

/** The machines a row stands for: the whole group's for a prefix, its own for one advertiser. */
function advertisersOf(row: RoutesRow): readonly RouteAdvertiser[] {
  return isRouteGroup(row) ? row.advertisers : [row];
}

function machineNames(row: RoutesRow): string {
  return advertisersOf(row)
    .map((advertiser) => nodeName(advertiser.node))
    .join(" ");
}

function networkNames(row: RoutesRow): string {
  return row.networks.map((network) => network.name).join(" ");
}

function statusRank(row: RoutesRow): number {
  return statusOrder[isRouteGroup(row) ? groupStatus(row) : row.status];
}

export const routeColumns = helper.columns([
  helper.accessor((row) => (row.exit ? "exit node" : row.route), {
    id: "route",
    header: "Route",
    enableSorting: true,
    cell: ({ row }) =>
      isRouteGroup(row.original) ? (
        <RouteCell
          group={row.original}
          expandable={row.getCanExpand()}
          expanded={row.getIsExpanded()}
          onToggle={row.getToggleExpandedHandler()}
        />
      ) : null,
    meta: { className: "w-[28%] min-w-44" },
  }),
  helper.accessor(machineNames, {
    id: "machine",
    header: "Machines",
    enableSorting: true,
    cell: ({ row }) => <MachineCell row={row.original} />,
    meta: { className: "min-w-48" },
  }),
  helper.accessor(networkNames, {
    id: "networks",
    header: "Network",
    enableSorting: false,
    cell: ({ row }) => <NetworksCell row={row.original} />,
    meta: { className: "hidden md:table-cell" },
  }),
  helper.accessor(statusRank, {
    id: "status",
    header: "Status",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <StatusCell row={row.original} />,
    meta: { className: "whitespace-nowrap" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const { me } = table.options.meta ?? {};

      return me === undefined ? null : <ActionsCell row={row.original} me={me} />;
    },
    meta: { className: "w-28 text-right", sticky: "right" },
  }),
]);

/**
 * The prefix, with a caret in front while several machines advertise it. A prefix only one machine
 * advertises stays a flat row, so the tree is there for the routes that have something to unfold.
 */
function RouteCell({
  group,
  expandable,
  expanded,
  onToggle,
}: {
  readonly group: RouteGroup;
  readonly expandable: boolean;
  readonly expanded: boolean;
  readonly onToggle: () => void;
}): ReactElement {
  const label = group.exit ? "the exit routes" : group.route;

  return (
    <span className="flex items-center gap-2">
      {expandable ? (
        <button
          type="button"
          aria-expanded={expanded}
          aria-label={`${expanded ? "Hide" : "Show"} the machines advertising ${label}`}
          className="flex h-lh items-center rounded-sm text-kumo-subtle hover:text-kumo-default focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none"
          onClick={onToggle}
        >
          {expanded ? <CaretDownIcon size={caretSize} /> : <CaretRightIcon size={caretSize} />}
        </button>
      ) : (
        <span aria-hidden className="w-3" />
      )}
      <span className="flex h-lh items-center text-kumo-subtle">
        {group.exit ? <GlobeIcon size={iconSize} /> : <PathIcon size={iconSize} />}
      </span>
      {group.exit ? (
        <span className="font-medium text-kumo-default">Exit node</span>
      ) : (
        <span className="truncate font-mono text-[0.9em]">{group.route}</span>
      )}
    </span>
  );
}

/**
 * Who advertises the prefix: the machine itself while there is one, the count while there are
 * several, and the machine again on each row the group unfolds into.
 */
function MachineCell({ row }: { readonly row: RoutesRow }): ReactElement {
  if (!isRouteGroup(row)) {
    return <MachineLink node={row.node} className="pl-5" />;
  }

  const [only, ...rest] = row.advertisers;

  if (only === undefined) {
    return <span className="text-kumo-subtle">None</span>;
  }

  if (rest.length === 0) {
    return <MachineLink node={only.node} />;
  }

  const online = row.advertisers.filter((advertiser) => advertiser.node.online).length;

  return (
    <span className="flex items-center gap-2 text-kumo-subtle">
      {plural(row.advertisers.length, "machine")}
      <Status tone={online === 0 ? "warning" : "success"}>{`${online} online`}</Status>
      {row.stale > 0 && row.stale < row.advertisers.length ? (
        <Status tone="warning">{`${row.stale} stale`}</Status>
      ) : null}
    </span>
  );
}

function MachineLink({
  node,
  className,
}: {
  readonly node: Node;
  readonly className?: string;
}): ReactElement {
  return (
    <span className={cn("flex min-w-0 items-center gap-2", className)}>
      <Link
        to="/machines/$nodeId"
        params={{ nodeId: node.id }}
        className="truncate hover:underline"
      >
        {nodeName(node)}
      </Link>
      <Status tone={node.online ? "success" : "neutral"} className="text-kumo-subtle">
        {node.online ? "Online" : "Offline"}
      </Status>
    </span>
  );
}

function NetworksCell({ row }: { readonly row: RoutesRow }): ReactElement | null {
  if (row.networks.length === 0) {
    // The group's row already says where the approval comes from; repeating it under each machine
    // it unfolds into only fills the column.
    return isRouteGroup(row) ? <span className="text-kumo-subtle">Approved by hand</span> : null;
  }

  return (
    <ValueList
      items={row.networks.map((network) => ({ value: network.name, muted: !network.enabled }))}
    />
  );
}

/**
 * A group counts what waits, because that is what the operator acts on; one machine's row says
 * where the prefix is served from, which only means something where several offer it.
 */
function StatusCell({ row }: { readonly row: RoutesRow }): ReactElement {
  if (!isRouteGroup(row)) {
    return (
      <span className="flex items-center gap-2">
        <RouteStatusText status={row.status} />
        {row.primary ? <span className="text-xs text-kumo-subtle">Primary</span> : null}
      </span>
    );
  }

  // A flat row reads as the one machine's state; a group counts how many of them are waiting.
  if (row.advertisers.length > 1 && row.pending > 0) {
    return <Status tone="warning">{`${row.pending} pending`}</Status>;
  }

  return <RouteStatusText status={groupStatus(row)} />;
}

function RouteStatusText({ status }: { readonly status: RouteStatus }): ReactElement {
  if (status === "stale") {
    return <Status tone="warning">No longer advertised</Status>;
  }

  return (
    <Status tone={status === "approved" ? "success" : "warning"}>
      {status === "approved" ? "Approved" : "Pending"}
    </Status>
  );
}

function ActionsCell({
  row,
  me,
}: {
  readonly row: RoutesRow;
  readonly me: Me;
}): ReactElement | null {
  if (!isRouteGroup(row)) {
    return <ApproveCell advertiser={row} me={me} />;
  }

  const [only, ...rest] = row.advertisers;

  if (only === undefined) {
    return null;
  }

  return rest.length === 0 ? (
    <ApproveCell advertiser={only} me={me} />
  ) : (
    <GroupApproveCell group={row} me={me} />
  );
}

/**
 * What taking an approval back is called, as the machine page calls it: a route the machine still
 * advertises is revoked, one it stopped advertising is only rejected, since it carries nothing.
 */
function withdrawal(advertiser: RouteAdvertiser): {
  readonly label: string;
  readonly done: string;
  readonly description: string;
} {
  const route = advertiser.exit ? "the exit routes" : advertiser.route;
  const machine = nodeName(advertiser.node);

  return advertiser.status === "stale"
    ? {
        label: "Reject",
        done: "Route rejected",
        description: `${machine} no longer advertises ${route}. Rejecting drops the approval it still carries.`,
      }
    : {
        label: "Revoke",
        done: "Route revoked",
        description: `${machine} stops carrying ${route} for the rest of the tailnet.`,
      };
}

/**
 * Approve a route, or take the approval back through the machine's route endpoint. Taking it back
 * asks first, because it puts a prefix out of service on a machine this row only names. A route a
 * network owns is approved by the network, so its button is off; disable or edit the network.
 */
function ApproveCell({
  advertiser,
  me,
}: {
  readonly advertiser: RouteAdvertiser;
  readonly me: Me;
}): ReactElement {
  const { setRoutes } = useNetworkMutations();
  const [confirming, setConfirming] = useState(false);
  const approved = advertiser.status !== "pending";
  const owned = advertiser.networks.some((network) => network.enabled);
  const withdraw = withdrawal(advertiser);

  const set = (next: boolean): void => {
    setRoutes.mutate(
      {
        params: { path: { nodeId: advertiser.node.id } },
        body: { routes: withRouteApproved(advertiser.node, advertiser.route, next) },
      },
      {
        onSuccess: () => {
          setConfirming(false);
          toast.success(next ? "Route approved" : withdraw.done);
        },
        onError: (error) => {
          setConfirming(false);
          toast.error(errorMessage(error));
        },
      },
    );
  };

  return (
    <>
      <Button
        variant={approved ? "ghost" : "secondary"}
        size="sm"
        disabled={!can(me, "devices:routes") || owned || setRoutes.isPending}
        title={owned ? "Approved by its network" : undefined}
        onClick={() => {
          if (approved) {
            setConfirming(true);
          } else {
            set(true);
          }
        }}
      >
        {approved ? withdraw.label : "Approve"}
      </Button>
      {approved ? (
        <ConfirmDialog
          open={confirming}
          onOpenChange={setConfirming}
          title={`${withdraw.label} this route?`}
          description={withdraw.description}
          confirmLabel={`${withdraw.label} route`}
          loading={setRoutes.isPending}
          onConfirm={() => {
            set(false);
          }}
        />
      ) : null}
    </>
  );
}

/** One line for the whole run: what went through, and what the first refusal said. */
function report(done: number, failures: readonly string[]): void {
  if (failures.length === 0) {
    toast.success(`Route approved on ${plural(done, "machine")}`);

    return;
  }

  toast.error(
    done === 0
      ? `Could not approve the route on ${plural(failures.length, "machine")}`
      : `Route approved on ${plural(done, "machine")}, ${failures.length} failed`,
    failures[0],
  );
}

/**
 * Approving a prefix on every machine still waiting for it, which is how a pair of subnet routers
 * is put into service: one confirmation, then the same per-machine call the single row makes, one
 * machine at a time so the server is not asked to do it all at once.
 */
function GroupApproveCell({
  group,
  me,
}: {
  readonly group: RouteGroup;
  readonly me: Me;
}): ReactElement | null {
  const { setRoutes } = useNetworkMutations();
  const [confirming, setConfirming] = useState(false);
  const waiting = approvableAdvertisers(group);

  if (waiting.length === 0 || !can(me, "devices:routes")) {
    return null;
  }

  const approveOne = async (advertiser: RouteAdvertiser): Promise<void> => {
    await setRoutes.mutateAsync({
      params: { path: { nodeId: advertiser.node.id } },
      body: { routes: withRouteApproved(advertiser.node, advertiser.route, true) },
    });
  };

  const approveAll = async (): Promise<void> => {
    const failures: string[] = [];

    // Chained rather than looped, so a prefix on a dozen routers does not open a dozen requests at
    // once and one machine's failure does not stop the ones after it.
    await waiting.reduce(
      (chain, advertiser) =>
        chain
          .then(() => approveOne(advertiser))
          .catch((error: unknown) => {
            failures.push(errorMessage(error));
          }),
      Promise.resolve(),
    );

    setConfirming(false);
    report(waiting.length - failures.length, failures);
  };

  return (
    <>
      <Button
        variant="secondary"
        size="sm"
        disabled={setRoutes.isPending}
        onClick={() => {
          setConfirming(true);
        }}
      >
        Approve all
      </Button>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={`Approve ${plural(waiting.length, "machine")}?`}
        description={`${group.exit ? "The exit routes" : group.route} start reaching the rest of the tailnet through ${waiting.map((advertiser) => nodeName(advertiser.node)).join(", ")}.`}
        confirmLabel="Approve routes"
        destructive={false}
        loading={setRoutes.isPending}
        onConfirm={() => {
          void approveAll();
        }}
      />
    </>
  );
}
