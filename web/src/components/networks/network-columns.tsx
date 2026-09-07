import { Badge } from "@cloudflare/kumo/components/badge";
import { Switch } from "@cloudflare/kumo/components/switch";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { GlobeIcon, PathIcon, WarningIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Group, Network } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { GroupChips } from "~/components/access/group-chips.tsx";
import { groupName, protocolSummary } from "~/components/access/model.ts";
import { prefixesSummary } from "~/components/networks/model.ts";
import { useNetworkMutations } from "~/components/networks/mutations.ts";
import { NetworkMenu } from "~/components/networks/network-menu.tsx";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { StatusDot } from "~/components/ui/status-dot.tsx";
import { toast } from "~/components/ui/toast.ts";

/** A network with its group names spelled out, so the global filter can match them. */
export interface NetworkRow extends Network {
  readonly groupNames: string;
}

export function toNetworkRows(
  networks: readonly Network[],
  groups: readonly Group[],
): NetworkRow[] {
  return networks.map((network) => ({
    ...network,
    groupNames: network.groupIds.map((id) => groupName(groups, id)).join(", "),
  }));
}

const helper = createAppColumnHelper<NetworkRow>();
const iconSize = 14;

export const networkColumns = helper.columns([
  helper.accessor((network) => `${network.name} ${network.description}`, {
    id: "name",
    header: "Network",
    enableSorting: true,
    cell: ({ row }) => <NameCell network={row.original} />,
    meta: { className: "w-[24%] min-w-44" },
  }),
  helper.accessor((network) => network.prefixes.join(" "), {
    id: "prefixes",
    header: "Prefixes",
    enableSorting: false,
    cell: ({ row }) => <PrefixesCell network={row.original} />,
    meta: { className: "min-w-36" },
  }),
  helper.accessor((network) => network.routers.map((router) => router.name).join(" "), {
    id: "routers",
    header: "Routers",
    enableSorting: false,
    cell: ({ row }) => <RoutersCell network={row.original} />,
    meta: { className: "min-w-36" },
  }),
  helper.accessor((network) => network.groupNames, {
    id: "groups",
    header: "Handed to",
    enableSorting: false,
    cell: ({ row, table }) => (
      <GroupChips ids={row.original.groupIds} groups={table.options.meta?.groups ?? []} />
    ),
    meta: { className: "hidden min-w-32 md:table-cell" },
  }),
  helper.accessor((network) => (network.enabled ? 1 : 0), {
    id: "enabled",
    header: "Enabled",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row, table }) => {
      const { me } = table.options.meta ?? {};

      return me === undefined ? null : <EnabledCell network={row.original} me={me} />;
    },
    meta: { className: "whitespace-nowrap" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const { me, groups, nodes } = table.options.meta ?? {};

      return me === undefined ? null : (
        <NetworkMenu network={row.original} groups={groups ?? []} nodes={nodes ?? []} me={me} />
      );
    },
    meta: { className: "w-12 text-right" },
  }),
]);

function NameCell({ network }: { readonly network: NetworkRow }): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="flex items-center gap-1.5">
        <span className="flex h-lh items-center text-kumo-subtle">
          {network.exitNode ? <GlobeIcon size={iconSize} /> : <PathIcon size={iconSize} />}
        </span>
        <span className="truncate font-medium text-kumo-default">{network.name}</span>
      </span>
      {network.description === "" ? null : (
        <span className="truncate text-xs text-kumo-subtle">{network.description}</span>
      )}
      {/* The "Handed to" column is hidden on phones; say it here so a reader
          without edit rights still sees who gets the network. */}
      <span className="truncate text-xs text-kumo-subtle md:hidden">
        {network.groupNames === "" ? "Handed to no group" : `Handed to ${network.groupNames}`}
      </span>
    </div>
  );
}

/** The prefixes, and the protocol and ports when the network narrows them. */
function PrefixesCell({ network }: { readonly network: Network }): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="font-mono text-[0.9em]">{prefixesSummary(network.prefixes)}</span>
      {network.protocol === "all" || network.protocol === "" ? null : (
        <span className="truncate text-xs text-kumo-subtle">{protocolSummary(network)}</span>
      )}
    </div>
  );
}

/** Each router with its liveness; one that stopped advertising a prefix gets a warning. */
function RoutersCell({ network }: { readonly network: Network }): ReactElement {
  if (network.routers.length === 0) {
    return <span className="text-kumo-inactive">No routers</span>;
  }

  return (
    <div className="flex flex-col gap-1">
      {network.routers.map((router) => (
        <span key={router.nodeId} className="flex items-center gap-2">
          <StatusDot status={router.online ? "online" : "offline"} />
          <span className="truncate">{router.name}</span>
          {router.missingPrefixes.length === 0 ? null : (
            <Tooltip content={`Not advertising ${router.missingPrefixes.join(", ")}`}>
              <Badge variant="warning" className="gap-1">
                <WarningIcon size={iconSize} />
                Missing
              </Badge>
            </Tooltip>
          )}
        </span>
      ))}
    </div>
  );
}

/** An inline switch on PATCH; off withdraws the approvals the network made. */
function EnabledCell({
  network,
  me,
}: {
  readonly network: Network;
  readonly me: Me;
}): ReactElement {
  const { setEnabled } = useNetworkMutations();

  return (
    <Switch
      size="sm"
      aria-label={`${network.name} enabled`}
      checked={network.enabled}
      disabled={!can(me, "devices:routes") || setEnabled.isPending}
      transitioning={setEnabled.isPending}
      onCheckedChange={(enabled) => {
        setEnabled.mutate(
          { params: { path: { id: network.id } }, body: { enabled } },
          {
            onSuccess: () => {
              toast.success(enabled ? "Network enabled" : "Network disabled");
            },
            onError: (error) => {
              toast.error(errorMessage(error));
            },
          },
        );
      }}
    />
  );
}
