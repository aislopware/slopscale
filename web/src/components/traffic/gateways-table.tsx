import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { ArrowSquareOutIcon, ChartLineIcon, TrashIcon } from "@phosphor-icons/react";
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
import { useForgetGatewayMutation } from "~/components/traffic/mutations.ts";
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

export type GatewayState = "reporting" | "silent" | "offline";

/**
 * A gateway reports every minute while its agent runs. Silent while the machine is connected means
 * the agent stopped, which is worth a look; silent while it is offline is the machine being away.
 */
export function gatewayState(reporter: TrafficReporter): GatewayState {
  if (!reporter.stale) {
    return "reporting";
  }

  return reporter.online ? "silent" : "offline";
}

const stateLabels: Record<GatewayState, string> = {
  reporting: "Reporting",
  silent: "Not reporting",
  offline: "Offline",
};

const stateTones: Record<GatewayState, Tone> = {
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

function ResolverCell({ reporter }: { readonly reporter: TrafficReporter }): ReactElement {
  if (reporter.resolverActive) {
    return <ValueList items={reporter.dnsListen} mono />;
  }

  if (reporter.collectors.dns.enabled) {
    return <Status tone="neutral">Not in use</Status>;
  }

  return <span className="text-kumo-subtle">Off</span>;
}

function GatewayMenu({
  reporter,
  writable,
}: {
  readonly reporter: TrafficReporter;
  readonly writable: boolean;
}): ReactElement {
  const [forgetting, setForgetting] = useState(false);
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

function columns(writable: boolean): ReturnType<typeof helper.columns> {
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
      cell: ({ row }) => {
        const state = gatewayState(row.original);

        return <Badge tone={stateTones[state]}>{stateLabels[state]}</Badge>;
      },
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
      meta: { className: "hidden lg:table-cell" },
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
      cell: ({ row }) => <GatewayMenu reporter={row.original} writable={writable} />,
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
}: {
  readonly reporters: readonly TrafficReporter[];
  readonly writable: boolean;
}): ReactElement {
  const table = useAppTable({
    data: reporters,
    columns: columns(writable),
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
