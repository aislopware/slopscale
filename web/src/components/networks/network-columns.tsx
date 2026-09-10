import { Switch } from "@cloudflare/kumo/components/switch";
import { GlobeIcon, PathIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Group, Network } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { GroupChips } from "~/components/access/group-chips.tsx";
import { groupName, isNarrowed, protocolSummary } from "~/components/access/model.ts";
import { prefixesSummary } from "~/components/networks/model.ts";
import { useNetworkMutations } from "~/components/networks/mutations.ts";
import { NetworkMenu } from "~/components/networks/network-menu.tsx";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { Code } from "~/components/ui/code.tsx";
import { Flagged } from "~/components/ui/flagged.tsx";
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
    cell: ({ row, table }) => (
      <PrefixesCell network={row.original} enforcing={table.options.meta?.enforcing === true} />
    ),
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
    header: "Groups",
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
      const { me, groups, nodes, enforcing } = table.options.meta ?? {};

      return me === undefined ? null : (
        <NetworkMenu
          network={row.original}
          groups={groups ?? []}
          nodes={nodes ?? []}
          enforcing={enforcing === true}
          me={me}
        />
      );
    },
    meta: { className: "w-12 text-right", sticky: "right" },
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
      {/* The "Groups" column is hidden on phones; say it here so a reader
          without edit rights still sees who gets the network. */}
      <span className="truncate text-xs text-kumo-subtle md:hidden">
        {network.groupNames === "" ? "No groups" : network.groupNames}
      </span>
    </div>
  );
}

/**
 * The prefixes, and the protocol and ports when the network narrows them. The narrowing only
 * applies once the tailnet has a packet filter, so on an open tailnet the cell says so.
 */
function PrefixesCell({
  network,
  enforcing,
}: {
  readonly network: Network;
  readonly enforcing: boolean;
}): ReactElement {
  const narrowed = isNarrowed(network);

  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="font-mono text-[0.9em]">{prefixesSummary(network.prefixes)}</span>
      {narrowed ? (
        <span className="truncate text-xs text-kumo-subtle">
          {protocolSummary(network)}
          {enforcing ? null : <span className="text-kumo-warning"> · not enforced</span>}
        </span>
      ) : null}
    </div>
  );
}

/** Each router with its liveness; one that stopped advertising a prefix is flagged by name. */
function RoutersCell({ network }: { readonly network: Network }): ReactElement {
  if (network.routers.length === 0) {
    return <span className="text-kumo-subtle">No routers</span>;
  }

  return (
    <div className="flex flex-col items-start gap-1">
      {network.routers.map((router) =>
        router.missingPrefixes.length === 0 ? (
          <span key={router.nodeId} className="flex max-w-full items-center gap-2">
            <span className="truncate">{router.name}</span>
            {router.online ? null : <span className="text-xs text-kumo-subtle">offline</span>}
          </span>
        ) : (
          <Flagged
            key={router.nodeId}
            title="Not advertising every prefix"
            detail={
              <>
                <Code className="whitespace-normal">{router.missingPrefixes.join(", ")}</Code> is
                approved for this network, but the machine stopped advertising it, so nothing
                reaches it through this router.
                {router.online ? "" : " The machine is offline."}
              </>
            }
          >
            {router.name}
          </Flagged>
        ),
      )}
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
