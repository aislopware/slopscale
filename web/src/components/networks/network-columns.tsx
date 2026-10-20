import { Switch } from "@cloudflare/kumo/components/switch";
import { cn } from "@cloudflare/kumo/utils";
import { GlobeIcon, PathIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Group, Network } from "~/api/queries.ts";
import type { MachineOwner } from "~/api/schema.gen.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { GroupNames } from "~/components/access/group-names.tsx";
import { groupName, isNarrowed, protocolSummary } from "~/components/access/model.ts";
import { statusLabel } from "~/components/machines/status-badge.tsx";
import { prefixesSummary } from "~/components/networks/model.ts";
import { useNetworkMutations } from "~/components/networks/mutations.ts";
import { NetworkMenu } from "~/components/networks/network-menu.tsx";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { narrowOnlyClass } from "~/components/table/styles.ts";
import { Avatar } from "~/components/ui/avatar.tsx";
import { Code } from "~/components/ui/code.tsx";
import { Flagged } from "~/components/ui/flagged.tsx";
import { Status } from "~/components/ui/status.tsx";
import { TagList } from "~/components/ui/tag.tsx";
import { toast } from "~/components/ui/toast.ts";
import { machineLabel, machineOwnerLabel } from "~/lib/node.ts";

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
    cell: ({ row, table }) => {
      const { me } = table.options.meta ?? {};

      return (
        <NameCell
          network={row.original}
          state={me === undefined ? null : <EnabledCell network={row.original} me={me} labelled />}
        />
      );
    },
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
  helper.accessor(
    (network) => network.routers.map((router) => machineLabel(router.name, router.owner)).join(" "),
    {
      id: "routers",
      header: "Routers",
      enableSorting: false,
      cell: ({ row }) => <RoutersCell network={row.original} />,
      meta: { className: "min-w-36" },
    },
  ),
  helper.accessor((network) => network.groupNames, {
    id: "groups",
    header: "Groups",
    enableSorting: false,
    cell: ({ row, table }) => (
      <GroupNames ids={row.original.groupIds} groups={table.options.meta?.groups ?? []} max={2} />
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
    meta: { className: "whitespace-nowrap", hideBelow: "sm" },
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

/** The network's name and who gets it; on a phone, which hides the Enabled column, its switch too. */
function NameCell({
  network,
  state,
}: {
  readonly network: NetworkRow;
  readonly state: ReactNode;
}): ReactElement {
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
      {state === null ? null : <span className={cn("pt-0.5", narrowOnlyClass)}>{state}</span>}
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
    <div className="flex flex-col items-start gap-1.5">
      {network.routers.map((router) => (
        // One line per router, the owner beside the name rather than under it, so a network
        // with one router keeps the 44px row.
        <span key={router.nodeId} className="flex max-w-full min-w-0 items-center gap-2">
          <RouterName router={router} />
          <RouterOwner owner={router.owner} />
        </span>
      ))}
    </div>
  );
}

/** Whose the router is, compact: its first tag, or the person's avatar with the name on hover. */
function RouterOwner({ owner }: { readonly owner: MachineOwner | undefined }): ReactElement | null {
  if (owner === undefined) {
    return null;
  }

  if (owner.tags.length > 0) {
    return <TagList tags={owner.tags} size="sm" max={1} className="shrink-0 flex-nowrap" />;
  }

  const label = machineOwnerLabel(owner);

  return label === "" ? null : (
    <span title={label} className="flex shrink-0">
      <Avatar name={label} id={owner.userId} src={owner.profilePicUrl} size="sm" />
    </span>
  );
}

function RouterName({ router }: { readonly router: Network["routers"][number] }): ReactElement {
  if (router.missingPrefixes.length === 0) {
    return (
      <span className="flex max-w-full items-center gap-2">
        <span className="truncate">{router.name}</span>
        {router.online ? null : (
          <Status tone="neutral" className="text-xs">
            {statusLabel("offline")}
          </Status>
        )}
      </span>
    );
  }

  return (
    <Flagged
      title="Not advertising every prefix"
      detail={
        <>
          <Code className="whitespace-normal">{router.missingPrefixes.join(", ")}</Code> is approved
          for this network, but the machine stopped advertising it, so nothing reaches it through
          this router.
          {router.online ? "" : " The machine is disconnected."}
        </>
      }
    >
      {router.name}
    </Flagged>
  );
}

/** An inline switch on PATCH; off withdraws the approvals the network made. */
function EnabledCell({
  network,
  me,
  labelled = false,
}: {
  readonly network: Network;
  readonly me: Me;
  /** Says "Enabled" or "Disabled" beside the switch, where no column heading names it. */
  readonly labelled?: boolean;
}): ReactElement {
  const { setEnabled } = useNetworkMutations();
  const toggle = (
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

  return labelled ? (
    <span className="flex items-center gap-2">
      {toggle}
      <span className="text-sm text-kumo-subtle">{network.enabled ? "Enabled" : "Disabled"}</span>
    </span>
  ) : (
    toggle
  );
}
