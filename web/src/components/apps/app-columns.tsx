import { Popover } from "@cloudflare/kumo/components/popover";
import { Link } from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";

import type { App, AppNode } from "~/api/queries.ts";
import { AppMenu } from "~/components/apps/app-menu.tsx";
import type { LearnedCount } from "~/components/apps/learned.ts";
import {
  connectorMachineCounts,
  everyConnector,
  machinesLabel,
  pendingSearch,
  totalPendingRoutes,
} from "~/components/apps/model.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { DomainList } from "~/components/ui/domain.tsx";
import { Status } from "~/components/ui/status.tsx";
import { TagList } from "~/components/ui/tag.tsx";

/** An app with what its connectors have learned for it, asked of the machines themselves. */
export type AppRow = App & { readonly learned: LearnedCount | null };

const helper = createAppColumnHelper<AppRow>();

/** How many values a cell shows before the rest become "+N more". */
const maxValues = 2;
const openDelay = 150;

export const appColumns = helper.columns([
  helper.accessor((app) => `${app.name} ${app.description}`, {
    id: "name",
    header: "Name",
    enableSorting: true,
    cell: ({ row }) => <NameCell app={row.original} />,
    meta: { className: "w-[24%] min-w-44 align-top" },
  }),
  helper.accessor((app) => app.domains.join(" "), {
    id: "domains",
    header: "Domains",
    enableSorting: false,
    cell: ({ row }) => (
      <DomainList domains={row.original.domains} max={maxValues} empty="No domain" />
    ),
    meta: { className: "min-w-44 align-top" },
  }),
  helper.accessor((app) => app.connectors.join(" "), {
    id: "connectors",
    header: "Connectors",
    enableSorting: false,
    cell: ({ row }) => <ConnectorsCell app={row.original} />,
    meta: { className: "hidden align-top md:table-cell" },
  }),
  helper.accessor((app) => app.nodes.map((node) => node.name).join(" "), {
    id: "machines",
    header: "Machines",
    enableSorting: false,
    cell: ({ row }) => <MachinesCell app={row.original} />,
    meta: { className: "hidden align-top whitespace-nowrap lg:table-cell" },
  }),
  helper.accessor((app) => app.learned?.count ?? 0, {
    id: "learned",
    header: "Learned routes",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <LearnedCell learned={row.original.learned} />,
    // The whole row fits a 1280px screen without Learned routes, and xl starts at 1280.
    meta: { className: "hidden align-top 2xl:table-cell", numeric: true },
  }),
  helper.accessor((app) => totalPendingRoutes(app.nodes), {
    id: "pending",
    header: "Pending",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <PendingCell app={row.original} />,
    meta: { className: "align-top whitespace-nowrap", numeric: true },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const me = table.options.meta?.me;

      return me === undefined ? null : <AppMenu app={row.original} me={me} />;
    },
    meta: { className: "w-12 text-right", sticky: "right" },
  }),
]);

function NameCell({ app }: { readonly app: App }): ReactElement {
  return (
    <div className="flex max-w-72 min-w-0 flex-col gap-0.5">
      <span className="truncate font-medium text-kumo-default">{app.name}</span>
      {app.description === "" ? null : (
        <span className="truncate text-xs text-kumo-subtle">{app.description}</span>
      )}
      {/* The connectors column is hidden on a small screen, so the name carries it there. */}
      <span className="truncate text-xs text-kumo-subtle md:hidden">
        {everyConnector(app.connectors) ? "Every connector" : app.connectors.join(", ")}
      </span>
    </div>
  );
}

function ConnectorsCell({ app }: { readonly app: App }): ReactElement {
  if (everyConnector(app.connectors)) {
    return <span className="text-kumo-subtle">Every connector</span>;
  }

  return <TagList tags={app.connectors} size="sm" max={maxValues} empty="Every connector" />;
}

/**
 * How many of the machines the selectors pick are connected, with the roll call one hover away: a
 * machine that carries the tag but does not run the connector serves nothing, and only the list can
 * say which one that is.
 */
function MachinesCell({ app }: { readonly app: App }): ReactElement {
  const counts = connectorMachineCounts(app.nodes);

  if (counts.total === 0) {
    return <span className="text-kumo-subtle">None</span>;
  }

  return (
    <Popover>
      <Popover.Trigger
        openOnHover
        delay={openDelay}
        className="inline-flex cursor-pointer items-center outline-none focus-visible:ring-2 focus-visible:ring-kumo-focus"
      >
        <Status tone={counts.online === 0 ? "warning" : "success"}>{machinesLabel(counts)}</Status>
      </Popover.Trigger>
      <Popover.Content side="top" className="max-w-72 gap-2 p-3">
        <Popover.Title className="text-sm leading-5 font-medium">Connector machines</Popover.Title>
        <ul className="flex flex-col gap-1.5">
          {app.nodes.map((node) => (
            <MachineRow key={node.nodeId} node={node} />
          ))}
        </ul>
      </Popover.Content>
    </Popover>
  );
}

function MachineRow({ node }: { readonly node: AppNode }): ReactElement {
  return (
    <li className="flex min-w-0 flex-col gap-0.5">
      <span className="flex min-w-0 items-center justify-between gap-3">
        <span className="truncate text-sm text-kumo-default">{node.name}</span>
        <Status tone={node.online ? "success" : "neutral"} className="text-sm text-kumo-subtle">
          {node.online ? "Online" : "Offline"}
        </Status>
      </span>
      {node.connector ? null : (
        <span className="text-xs text-kumo-subtle">Not running the connector</span>
      )}
    </li>
  );
}

/**
 * How many routes the app's connectors learned for it, or why there is no figure: the caller cannot
 * ask machines, none of the app's connectors is connected, or none has answered yet. A figure from
 * some of several connectors says so, since the rest may know more.
 */
function LearnedCell({ learned }: { readonly learned: LearnedCount | null }): ReactElement {
  if (learned === null) {
    return <Muted title="Asking the connectors needs devices:core:read.">{"\u2014"}</Muted>;
  }

  if (learned.connected === 0) {
    return <Muted title="No connector is connected to ask.">{"\u2014"}</Muted>;
  }

  if (learned.answered === 0) {
    return <Muted title="Asking the connectors\u2026">{"\u2026"}</Muted>;
  }

  const partial = learned.answered < learned.connected;

  return (
    <Muted
      title={
        partial
          ? `From ${learned.answered} of ${learned.connected} connected connectors.`
          : undefined
      }
    >
      {partial ? `${learned.count}+` : learned.count}
    </Muted>
  );
}

function Muted({
  title,
  children,
}: {
  readonly title?: string | undefined;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <span className="text-kumo-subtle" {...(title === undefined ? {} : { title })}>
      {children}
    </span>
  );
}

/**
 * Routes the connectors advertise that nobody has approved yet. Until they are approved the app's
 * addresses are learned but unreachable, so the count is a link straight to them.
 */
function PendingCell({ app }: { readonly app: App }): ReactElement {
  const pending = totalPendingRoutes(app.nodes);

  if (pending === 0) {
    return <span className="text-kumo-subtle">0</span>;
  }

  return (
    <Link
      to="/routes"
      search={pendingSearch(app.nodes)}
      className="rounded-sm outline-none hover:underline focus-visible:ring-2 focus-visible:ring-kumo-focus"
    >
      <Status tone="warning">{pending}</Status>
    </Link>
  );
}
