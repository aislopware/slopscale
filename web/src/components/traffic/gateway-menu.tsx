import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import {
  ChartLineIcon,
  CheckCircleIcon,
  DesktopIcon,
  TrashIcon,
  XCircleIcon,
} from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { TrafficReporter } from "~/api/traffic.ts";
import { trafficNodeName } from "~/components/traffic/machines-table.tsx";
import {
  useForgetGatewayMutation,
  useResolverApprovalMutation,
} from "~/components/traffic/mutations.ts";
import { defaultTrafficRange } from "~/components/traffic/range.ts";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";

/**
 * Approving a resolver hands the DNS of the gateway's exit node users to it, so it asks first and
 * says what changes and whom it covers; so does taking it back.
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
          ? `While DNS logging is on and ${name} keeps reporting, machines using ${name} as their exit node resolve through its resolver, and their lookups are logged only while they use it. Machines on another exit node or none, and any on Tailscale older than 1.86, keep their usual DNS and are never logged.`
          : `The machines using ${name} as their exit node go back to its usual DNS with their next update, and their lookups are no longer logged.`
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

/**
 * Why the resolver item is off, or undefined when it is on. Taking an approval back is always open
 * to whoever may approve; approving waits for a resolver there is to use.
 */
function resolverBlocker(reporter: TrafficReporter, canApprove: boolean): string | undefined {
  if (!canApprove) {
    return "Approving a resolver takes the traffic and DNS permissions";
  }

  if (reporter.resolverApprovedAt !== undefined) {
    return undefined;
  }

  if (reporter.refused !== "") {
    return "The server refuses this gateway's reports, so its resolver would not be used";
  }

  if (!reporter.collectors.dns.enabled) {
    return "The gateway runs its resolver only while DNS logging is on";
  }

  return undefined;
}

export function GatewayMenu({
  reporter,
  writable,
  canApprove,
}: {
  readonly reporter: TrafficReporter;
  readonly writable: boolean;
  /** Approving a resolver moves its exit node users' DNS, so it takes the DNS scope as well. */
  readonly canApprove: boolean;
}): ReactElement {
  const [forgetting, setForgetting] = useState(false);
  const [approving, setApproving] = useState(false);
  const forget = useForgetGatewayMutation();
  const name = trafficNodeName(reporter);
  const approve = reporter.resolverApprovedAt === undefined;

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
              <DesktopIcon className="mr-2 size-4" />
              Machine
            </Link>
          }
        />
        <DropdownMenu.Separator />
        <DisabledReason reason={resolverBlocker(reporter, canApprove)}>
          <DropdownMenu.Item
            icon={approve ? CheckCircleIcon : XCircleIcon}
            disabled={resolverBlocker(reporter, canApprove) !== undefined}
            onClick={() => {
              setApproving(true);
            }}
          >
            {approve ? "Use its resolver for DNS…" : "Stop using its resolver…"}
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
        description="The gateway leaves this list and its resolver leaves the DNS of the machines using it as their exit node. What it reported stays until the retention removes it. An agent that is still running comes back with its next report, so stop it on the machine first."
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
