import { Popover } from "@cloudflare/kumo/components/popover";
import type { ReactElement } from "react";

import type { PostureIntegration } from "~/api/queries.ts";
import {
  integrationStatus,
  providerLabel,
  statusLabel,
  statusTone,
} from "~/components/posture-integrations/model.ts";
import { PostureIntegrationMenu } from "~/components/posture-integrations/posture-integration-menu.tsx";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Status } from "~/components/ui/status.tsx";

const helper = createAppColumnHelper<PostureIntegration>();
const openDelay = 150;

export const postureIntegrationColumns = helper.columns([
  helper.accessor((integration) => `${integration.name} ${integration.prefix}`, {
    id: "name",
    header: "Name",
    enableSorting: true,
    cell: ({ row }) => <NameCell integration={row.original} />,
    meta: { className: "w-[28%] min-w-44" },
  }),
  helper.accessor((integration) => integration.provider, {
    id: "provider",
    header: "Provider",
    enableSorting: true,
    cell: ({ row, table }) => (
      <span className="whitespace-nowrap">
        {providerLabel(table.options.meta?.postureProviders ?? [], row.original.provider)}
      </span>
    ),
    meta: { className: "w-[22%] whitespace-nowrap" },
  }),
  helper.accessor((integration) => integrationStatus(integration), {
    id: "status",
    header: "Status",
    enableSorting: true,
    cell: ({ row }) => <StatusCell integration={row.original} />,
    meta: { className: "w-[20%]" },
  }),
  helper.accessor((integration) => integration.lastSyncAt ?? "", {
    id: "lastSyncAt",
    header: "Last sync",
    enableSorting: true,
    cell: ({ row }) => <LastSyncCell integration={row.original} />,
    meta: { className: "w-[15%] whitespace-nowrap" },
  }),
  helper.accessor((integration) => integration.lastMatched, {
    id: "lastMatched",
    header: "Machines matched",
    enableSorting: true,
    cell: ({ getValue }) => <span>{getValue().toLocaleString()}</span>,
    meta: { className: "w-[15%]" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const { me, postureProviders } = table.options.meta ?? {};

      return me === undefined ? null : (
        <PostureIntegrationMenu
          integration={row.original}
          providers={postureProviders ?? []}
          me={me}
        />
      );
    },
    meta: { className: "w-12 text-right", sticky: "right" },
  }),
]);

function NameCell({ integration }: { readonly integration: PostureIntegration }): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="truncate font-medium text-kumo-default">{integration.name}</span>
      <span className="truncate font-mono text-xs text-kumo-subtle">{integration.prefix}</span>
    </div>
  );
}

function StatusCell({ integration }: { readonly integration: PostureIntegration }): ReactElement {
  const status = integrationStatus(integration);
  const label = statusLabel(status);
  const tone = statusTone(status);

  if (status === "failed" && integration.lastError !== "") {
    return (
      <Popover>
        <Popover.Trigger
          openOnHover
          delay={openDelay}
          className="inline-flex cursor-pointer items-center outline-none focus-visible:ring-2 focus-visible:ring-kumo-focus"
        >
          <Status tone={tone}>{label}</Status>
        </Popover.Trigger>
        <Popover.Content side="top" className="max-w-72 gap-1 p-3">
          <Popover.Title className="text-sm leading-5 font-medium">Sync failed</Popover.Title>
          <Popover.Description className="text-sm text-kumo-subtle">
            {integration.lastError}
          </Popover.Description>
        </Popover.Content>
      </Popover>
    );
  }

  return <Status tone={tone}>{label}</Status>;
}

function LastSyncCell({ integration }: { readonly integration: PostureIntegration }): ReactElement {
  if (
    integration.lastSyncAt === null ||
    integration.lastSyncAt === undefined ||
    integration.lastSyncAt === ""
  ) {
    return <span className="text-kumo-subtle">Never</span>;
  }

  return (
    <span className="text-kumo-subtle">
      <RelativeTime value={integration.lastSyncAt} />
    </span>
  );
}
