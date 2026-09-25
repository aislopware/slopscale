import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import {
  ArrowSquareOutIcon,
  ChartLineIcon,
  CheckCircleIcon,
  TrashIcon,
  XCircleIcon,
} from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { TrafficReporter } from "~/api/traffic.ts";
import { plural } from "~/components/overview/plural.ts";
import { createAppColumnHelper, useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { formatCount } from "~/components/traffic/format.ts";
import { trafficNodeName } from "~/components/traffic/machines-table.tsx";
import {
  useForgetGatewayMutation,
  useResolverApprovalMutation,
} from "~/components/traffic/mutations.ts";
import { defaultTrafficRange } from "~/components/traffic/range.ts";
import { Badge } from "~/components/ui/badge.tsx";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";
import type { Tone } from "~/components/ui/status.tsx";
import { Status, StatusDetail } from "~/components/ui/status.tsx";
import { ValueList } from "~/components/ui/value-list.tsx";

export type GatewayState = "refused" | "reporting" | "silent" | "offline";

/**
 * A gateway reports every minute while its agent runs. Silent while the machine is connected means
 * the agent stopped, which is worth a look; silent while it is offline is the machine being away. A
 * gateway the server no longer takes reports from is refused whatever its agent does.
 */
export function gatewayState(reporter: TrafficReporter): GatewayState {
  if (reporter.refused !== "") {
    return "refused";
  }

  if (!reporter.stale) {
    return "reporting";
  }

  return reporter.online ? "silent" : "offline";
}

const stateLabels: Record<GatewayState, string> = {
  refused: "Refused",
  reporting: "Reporting",
  silent: "Not reporting",
  offline: "Offline",
};

const stateTones: Record<GatewayState, Tone> = {
  refused: "danger",
  reporting: "success",
  silent: "warning",
  offline: "neutral",
};

const collectorNames = {
  conntrack: "Connections",
  sni: "Handshakes",
  dns: "Resolver",
  appConnector: "App connector",
} as const;

type CollectorKey = keyof typeof collectorNames;

const collectorKeys: readonly CollectorKey[] = ["conntrack", "sni", "dns", "appConnector"];

/** What each collector does, for the popover of a failing one. */
const collectorPurpose: Record<CollectorKey, string> = {
  conntrack: "Counts what each machine sends through the gateway.",
  sni: "Names destinations from TLS and QUIC handshakes.",
  dns: "Answers the machines' lookups and names destinations from the answers.",
  appConnector: "Names destinations from the app connector's domains.",
};

/** The collectors the agent runs, each a word; a failing one carries its error one hover away. */
function Collectors({ reporter }: { readonly reporter: TrafficReporter }): ReactElement {
  const running = collectorKeys.filter((key) => reporter.collectors[key].enabled);

  if (running.length === 0) {
    return <span className="text-kumo-subtle">None</span>;
  }

  return (
    <span className="flex flex-col gap-0.5">
      {running.map((key) => {
        const { error } = reporter.collectors[key];

        return error === "" ? (
          <span key={key}>{collectorNames[key]}</span>
        ) : (
          <StatusDetail
            key={key}
            tone="danger"
            label={`${collectorNames[key]} failed`}
            title={collectorPurpose[key]}
            detail={<span className="font-mono text-[0.9em] break-words">{error}</span>}
          />
        );
      })}
    </span>
  );
}

/** The state in a word, and a refusal's reason under it, since that reason is what to fix. */
function StateCell({ reporter }: { readonly reporter: TrafficReporter }): ReactElement {
  const state = gatewayState(reporter);

  return (
    <span className="flex max-w-60 flex-col items-start gap-1">
      <Badge tone={stateTones[state]}>{stateLabels[state]}</Badge>
      {reporter.refused === "" ? null : (
        <span className="text-xs text-pretty text-kumo-subtle">{sentence(reporter.refused)}</span>
      )}
    </span>
  );
}

function sentence(reason: string): string {
  return `${reason.charAt(0).toUpperCase()}${reason.slice(1)}.`;
}

/** What the clients do with the gateway's resolver: use it, wait for an approval, or nothing. */
function ResolverUse({ reporter }: { readonly reporter: TrafficReporter }): ReactElement {
  if (reporter.resolverActive) {
    return <ValueList items={reporter.dnsListen} mono />;
  }

  if (reporter.resolverApprovedAt === undefined) {
    return reporter.collectors.dns.enabled ? (
      <Status tone="warning">Not approved</Status>
    ) : (
      <span className="text-kumo-subtle">Off</span>
    );
  }

  return (
    <StatusDetail
      tone="neutral"
      label="Not in use"
      title="Approved, not in use"
      detail="The clients use an approved resolver while DNS logging is on, the gateway reports it answering, and the gateway may report."
    />
  );
}

function ResolverCell({ reporter }: { readonly reporter: TrafficReporter }): ReactElement {
  return (
    <span className="flex flex-col items-start gap-0.5">
      <ResolverUse reporter={reporter} />
      {reporter.resolverApprovedAt === undefined ? null : (
        <span className="text-xs whitespace-nowrap text-kumo-subtle">
          Approved <RelativeTime value={reporter.resolverApprovedAt} />
        </span>
      )}
    </span>
  );
}

/**
 * Approving a resolver hands every client's DNS to it, so it asks first and says what changes; so
 * does taking it back.
 */
function ResolverDialog({
  reporter,
  open,
  onOpenChange,
}: {
  readonly reporter: TrafficReporter;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  const approval = useResolverApprovalMutation();
  const name = trafficNodeName(reporter);
  const approve = reporter.resolverApprovedAt === undefined;

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      destructive={!approve}
      title={approve ? `Use ${name}'s resolver for DNS?` : `Stop using ${name}'s resolver?`}
      description={
        approve
          ? "While DNS logging is on and the gateway keeps reporting, each machine that accepts the tailnet's DNS is given one approved gateway resolver next to the global nameservers, in place of its local DNS. Every lookup then goes through a gateway, so the DNS page shows it."
          : "The machines using it move to another approved gateway's resolver, or back to the global nameservers alone, with their next update."
      }
      confirmLabel={approve ? "Approve resolver" : "Stop using it"}
      loading={approval.isPending}
      error={approval.isError ? errorMessage(approval.error) : undefined}
      onConfirm={() => {
        approval.mutate(
          { params: { path: { nodeId: reporter.nodeId } }, body: { resolver: approve } },
          {
            onSuccess: () => {
              onOpenChange(false);
            },
          },
        );
      }}
    />
  );
}

function GatewayMenu({
  reporter,
  writable,
  canApprove,
}: {
  readonly reporter: TrafficReporter;
  readonly writable: boolean;
  /** Approving a resolver moves the clients' DNS, so it takes the DNS scope as well. */
  readonly canApprove: boolean;
}): ReactElement {
  const [forgetting, setForgetting] = useState(false);
  const [approving, setApproving] = useState(false);
  const forget = useForgetGatewayMutation();
  const name = trafficNodeName(reporter);

  return (
    <>
      <RowMenu label={`Actions for gateway ${name}`}>
        <DropdownMenu.Item
          render={
            <Link
              to="/traffic/overview"
              search={{ range: defaultTrafficRange, from: "", to: "", gateway: reporter.nodeId }}
            >
              <ChartLineIcon className="mr-2 size-4" />
              Traffic through it
            </Link>
          }
        />
        <DropdownMenu.Item
          render={
            <Link to="/machines/$nodeId" params={{ nodeId: reporter.nodeId }}>
              <ArrowSquareOutIcon className="mr-2 size-4" />
              Machine
            </Link>
          }
        />
        <DropdownMenu.Separator />
        <DisabledReason
          reason={
            canApprove ? undefined : "Approving a resolver takes the traffic and DNS permissions"
          }
        >
          <DropdownMenu.Item
            icon={reporter.resolverApprovedAt === undefined ? CheckCircleIcon : XCircleIcon}
            disabled={!canApprove}
            onClick={() => {
              setApproving(true);
            }}
          >
            {reporter.resolverApprovedAt === undefined
              ? "Use its resolver for DNS…"
              : "Stop using its resolver…"}
          </DropdownMenu.Item>
        </DisabledReason>
        <DisabledReason reason={writable ? undefined : "Your credentials may not forget gateways"}>
          <DropdownMenu.Item
            icon={TrashIcon}
            variant="danger"
            disabled={!writable}
            onClick={() => {
              setForgetting(true);
            }}
          >
            Forget…
          </DropdownMenu.Item>
        </DisabledReason>
      </RowMenu>
      <ResolverDialog reporter={reporter} open={approving} onOpenChange={setApproving} />
      <ConfirmDialog
        open={forgetting}
        onOpenChange={setForgetting}
        title={`Forget ${name}?`}
        description="The gateway leaves this list and its resolver leaves the machines' DNS. What it reported stays until the retention removes it. An agent that is still running comes back with its next report, so stop it on the machine first."
        confirmLabel="Forget gateway"
        loading={forget.isPending}
        error={forget.isError ? errorMessage(forget.error) : undefined}
        onConfirm={() => {
          forget.mutate(
            { params: { path: { nodeId: reporter.nodeId } } },
            {
              onSuccess: () => {
                setForgetting(false);
              },
            },
          );
        }}
      />
    </>
  );
}

const helper = createAppColumnHelper<TrafficReporter>();

function columns(writable: boolean, canApprove: boolean): ReturnType<typeof helper.columns> {
  return helper.columns([
    helper.accessor((reporter) => trafficNodeName(reporter), {
      id: "gateway",
      header: "Gateway",
      enableSorting: true,
      cell: ({ row }) => (
        <span className="flex min-w-0 flex-col">
          <Link
            to="/machines/$nodeId"
            params={{ nodeId: row.original.nodeId }}
            className="truncate font-medium text-kumo-default hover:underline"
          >
            {trafficNodeName(row.original)}
          </Link>
          <span className="truncate text-xs text-kumo-subtle">
            {row.original.version === ""
              ? "Unknown version"
              : `slopscale-flowd ${row.original.version}`}
          </span>
        </span>
      ),
      meta: { className: "w-[26%] max-w-0 min-w-44" },
    }),
    helper.accessor((reporter) => gatewayState(reporter), {
      id: "state",
      header: "State",
      enableSorting: true,
      cell: ({ row }) => <StateCell reporter={row.original} />,
    }),
    helper.accessor((reporter) => reporter.lastReportAt, {
      id: "lastReport",
      header: "Last report",
      enableSorting: true,
      sortDescFirst: true,
      cell: ({ row }) => (
        <span className="whitespace-nowrap text-kumo-subtle">
          <RelativeTime value={row.original.lastReportAt} />
        </span>
      ),
      meta: { className: "hidden md:table-cell" },
    }),
    helper.display({
      id: "collectors",
      header: "Collectors",
      cell: ({ row }) => <Collectors reporter={row.original} />,
      meta: { className: "hidden lg:table-cell" },
    }),
    helper.display({
      id: "resolver",
      header: "Resolver",
      cell: ({ row }) => <ResolverCell reporter={row.original} />,
      meta: { className: "hidden md:table-cell" },
    }),
    helper.accessor((reporter) => reporter.dropped + reporter.unattributed, {
      id: "lost",
      header: "Dropped",
      enableSorting: true,
      sortDescFirst: true,
      cell: ({ row }) => <LostCell reporter={row.original} />,
      meta: { numeric: true, className: "hidden xl:table-cell" },
    }),
    helper.display({
      id: "actions",
      header: "",
      cell: ({ row }) => (
        <GatewayMenu reporter={row.original} writable={writable} canApprove={canApprove} />
      ),
      meta: { sticky: "right", className: "w-12 text-right" },
    }),
  ]);
}

/**
 * What a gateway could not account for: rows its spool dropped while the server was out of reach,
 * and traffic from addresses that are no machine of the tailnet.
 */
function LostCell({ reporter }: { readonly reporter: TrafficReporter }): ReactElement {
  if (reporter.dropped === 0 && reporter.unattributed === 0) {
    return <span className="text-kumo-subtle">0</span>;
  }

  return (
    <StatusDetail
      tone={reporter.dropped > 0 ? "warning" : "neutral"}
      label={formatCount(reporter.dropped + reporter.unattributed)}
      title="Rows the server could not keep"
      detail={
        <span className="flex flex-col gap-1">
          <span>{`${formatCount(reporter.dropped)} dropped by the agent while it could not reach the server.`}</span>
          <span>{`${formatCount(reporter.unattributed)} from addresses that are no machine of the tailnet.`}</span>
        </span>
      }
    />
  );
}

/** Every gateway that ever reported: whether it still does, what it collects, and its resolver. */
export function GatewaysTable({
  reporters,
  writable,
  canApprove,
}: {
  readonly reporters: readonly TrafficReporter[];
  readonly writable: boolean;
  readonly canApprove: boolean;
}): ReactElement {
  const table = useAppTable({
    data: reporters,
    columns: columns(writable, canApprove),
    getRowId: (reporter) => reporter.nodeId,
    initialState: { sorting: [{ id: "gateway", desc: false }] },
  });

  return (
    <table.AppTable>
      <DataTable
        empty={
          <SectionEmpty
            title="No gateway reports yet"
            description="A gateway shows up here with its first report, a minute after the agent starts."
          />
        }
        footer={
          reporters.length === 0 ? undefined : (
            <TableFooter>{`Showing ${plural(reporters.length, "gateway")}`}</TableFooter>
          )
        }
      />
    </table.AppTable>
  );
}
