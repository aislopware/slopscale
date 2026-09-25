import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import type { TrafficReporter } from "~/api/traffic.ts";
import { plural } from "~/components/overview/plural.ts";
import { createAppColumnHelper, useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { formatCount } from "~/components/traffic/format.ts";
import { GatewayMenu } from "~/components/traffic/gateway-menu.tsx";
import { trafficNodeName } from "~/components/traffic/machines-table.tsx";
import { Badge } from "~/components/ui/badge.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";
import type { Tone } from "~/components/ui/status.tsx";
import { Status, StatusDetail } from "~/components/ui/status.tsx";
import { ValueList } from "~/components/ui/value-list.tsx";
import { useWidths } from "~/lib/breakpoint.ts";
import type { Widths } from "~/lib/breakpoint.ts";

export type GatewayState = "refused" | "reporting" | "degraded" | "silent" | "offline";

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
  dns: "Answers the lookups of the machines using the gateway as their exit node, and names their destinations from the answers.",
  appConnector: "Names destinations from the app connector's domains.",
};

/** The collectors the agent runs that report an error. */
function failingCollectors(reporter: TrafficReporter): CollectorKey[] {
  return collectorKeys.filter(
    (key) => reporter.collectors[key].enabled && reporter.collectors[key].error !== "",
  );
}

/**
 * A gateway reports every minute while its agent runs, and degraded while one of its collectors
 * fails. Silent while the machine is connected means the agent stopped, which is worth a look;
 * silent while it is offline is the machine being away. A gateway the server no longer takes
 * reports from is refused whatever its agent does.
 */
export function gatewayState(reporter: TrafficReporter): GatewayState {
  if (reporter.refused !== "") {
    return "refused";
  }

  if (reporter.stale) {
    return reporter.online ? "silent" : "offline";
  }

  return failingCollectors(reporter).length === 0 ? "reporting" : "degraded";
}

const stateLabels: Record<GatewayState, string> = {
  refused: "Refused",
  reporting: "Reporting",
  degraded: "Degraded",
  silent: "Not reporting",
  offline: "Offline",
};

const stateTones: Record<GatewayState, Tone> = {
  refused: "danger",
  reporting: "success",
  degraded: "warning",
  silent: "warning",
  offline: "neutral",
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

/**
 * The state in a word, and what to fix under it: a refusal's reason, or which collectors are
 * failing.
 */
function StateCell({ reporter }: { readonly reporter: TrafficReporter }): ReactElement {
  const state = gatewayState(reporter);
  let why = "";

  if (state === "refused") {
    why = sentence(reporter.refused);
  } else if (state === "degraded") {
    why = `${failingCollectors(reporter)
      .map((key) => collectorNames[key])
      .join(", ")} failing`;
  }

  return (
    <span className="flex max-w-60 flex-col items-start gap-1">
      <Badge tone={stateTones[state]}>{stateLabels[state]}</Badge>
      {why === "" ? null : <span className="text-xs text-pretty text-kumo-subtle">{why}</span>}
    </span>
  );
}

function sentence(reason: string): string {
  return `${reason.charAt(0).toUpperCase()}${reason.slice(1)}.`;
}

/** Whether the gateway's exit node users resolve through it: yes, not approved, or not now. */
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
      detail="The machines using the gateway as their exit node use its approved resolver while DNS logging is on, the gateway reports it answering, and the gateway may report."
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

const helper = createAppColumnHelper<TrafficReporter>();

function columns(
  writable: boolean,
  canApprove: boolean,
  widths: Widths,
): ReturnType<typeof helper.columns> {
  const lastReport = helper.accessor((reporter) => reporter.lastReportAt, {
    id: "lastReport",
    header: "Last report",
    enableSorting: true,
    sortDescFirst: true,
    cell: ({ row }) => (
      <span className="whitespace-nowrap text-kumo-subtle">
        <RelativeTime value={row.original.lastReportAt} />
      </span>
    ),
  });
  const collectors = helper.display({
    id: "collectors",
    header: "Collectors",
    cell: ({ row }) => <Collectors reporter={row.original} />,
  });
  const resolver = helper.display({
    id: "resolver",
    header: "Resolver",
    cell: ({ row }) => <ResolverCell reporter={row.original} />,
  });
  const dropped = helper.accessor((reporter) => reporter.dropped, {
    id: "dropped",
    header: "Dropped",
    enableSorting: true,
    sortDescFirst: true,
    cell: ({ row }) => <DroppedCell reporter={row.original} />,
    meta: { numeric: true },
  });

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
    ...(widths.md ? [lastReport] : []),
    ...(widths.lg ? [collectors] : []),
    ...(widths.md ? [resolver] : []),
    ...(widths.xl ? [dropped] : []),
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
 * The rows the agent's spool dropped while the server was out of reach. Traffic from addresses that
 * are no machine of the tailnet is not lost the same way, so it stays in the popover.
 */
function DroppedCell({ reporter }: { readonly reporter: TrafficReporter }): ReactElement {
  if (reporter.dropped === 0 && reporter.unattributed === 0) {
    return <span className="text-kumo-subtle">0</span>;
  }

  const dropped =
    reporter.dropped === 0
      ? "Nothing dropped by the agent."
      : `${formatCount(reporter.dropped)} dropped by the agent while it could not reach the server.`;

  return (
    <StatusDetail
      tone={reporter.dropped > 0 ? "warning" : "neutral"}
      label={formatCount(reporter.dropped)}
      title="Rows the server could not keep"
      detail={
        <span className="flex flex-col gap-1">
          <span>{dropped}</span>
          {reporter.unattributed === 0 ? null : (
            <span>{`${formatCount(reporter.unattributed)} flows and lookups from addresses no machine holds, which are not stored.`}</span>
          )}
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
  const widths = useWidths();
  const table = useAppTable({
    data: reporters,
    columns: columns(writable, canApprove, widths),
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
