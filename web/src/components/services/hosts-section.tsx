import { Switch } from "@cloudflare/kumo/components/switch";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Node, Service, ServiceHost } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { approvedServicesOf, withServiceApproved } from "~/components/services/model.ts";
import { useServiceMutations } from "~/components/services/mutations.ts";
import { Badge } from "~/components/ui/badge.tsx";
import { Code } from "~/components/ui/code.tsx";
import { CommandBox } from "~/components/ui/command-text.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { Section, SectionEmpty, SectionRow } from "~/components/ui/section.tsx";
import { Status } from "~/components/ui/status.tsx";
import { toast } from "~/components/ui/toast.ts";
import { isTagged } from "~/lib/node.ts";

/** Why a machine cannot be approved to host a service; the server refuses it as well. */
export const untaggedReason = "Only tagged machines can host a service";

/**
 * The machines that announce the service or may host it, each with its approval. A client reaches
 * the service only through one that announces it, advertises it and is approved, so the row says
 * which of the three is missing.
 */
export function HostsSection({
  service,
  services,
  nodes,
  me,
}: {
  readonly service: Service;
  /** Every service, to read back what else each machine is approved for. */
  readonly services: readonly Service[];
  /** The machines, when the caller may read them; without them a switch trusts the server. */
  readonly nodes: readonly Node[];
  readonly me: Me;
}): ReactElement {
  const { setServices } = useServiceMutations();
  const canEdit = can(me, "services");

  function set(host: ServiceHost, approved: boolean): void {
    const next = withServiceApproved(
      approvedServicesOf(services, host.nodeId),
      service.name,
      approved,
    );

    setServices.mutate(
      { params: { path: { nodeId: host.nodeId } }, body: { services: next } },
      {
        onSuccess: () => {
          toast.success(approved ? "Machine approved to host it" : "Approval withdrawn");
        },
        onError: (error) => {
          toast.error(errorMessage(error));
        },
      },
    );
  }

  return (
    <Section
      title="Hosts"
      description="A machine hosts the service once it announces it, advertises it and you approve it."
    >
      {service.hosts.length === 0 ? (
        <SectionEmpty
          title="No machine announces it"
          description="Run these on a tagged machine, then approve it here."
          contents={
            <div className="flex w-full max-w-md flex-col items-center gap-2">
              <CommandBox
                size="sm"
                wrap
                className="w-full"
                command={`tailscale serve --service=${service.name} --https=443 localhost:8080`}
              />
              <CommandBox
                size="sm"
                wrap
                className="w-full"
                command={`tailscale serve advertise ${service.name}`}
              />
            </div>
          }
        />
      ) : (
        service.hosts.map((host) => (
          <HostRow
            key={host.nodeId}
            host={host}
            node={nodes.find((node) => node.id === host.nodeId)}
            disabled={!canEdit || setServices.isPending}
            onSet={set}
          />
        ))
      )}
    </Section>
  );
}

function HostRow({
  host,
  node,
  disabled,
  onSet,
}: {
  readonly host: ServiceHost;
  readonly node: Node | undefined;
  readonly disabled: boolean;
  readonly onSet: (host: ServiceHost, approved: boolean) => void;
}): ReactElement {
  const untagged = node !== undefined && !isTagged(node);

  return (
    <SectionRow className="flex flex-wrap items-start justify-between gap-3 py-3">
      <div className="flex min-w-0 flex-col gap-1">
        <Link
          to="/machines/$nodeId"
          params={{ nodeId: host.nodeId }}
          className="flex min-w-0 items-center gap-2 font-medium text-kumo-default hover:text-kumo-link hover:underline"
        >
          <span className="truncate">{host.name}</span>
        </Link>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-kumo-subtle">
          <AnnouncementStatus announced={host.announced} active={host.active} />
          {host.ports.length === 0 ? null : <Code>{host.ports.join(", ")}</Code>}
          {host.primary ? <Status tone="success">Serving</Status> : null}
        </div>
      </div>
      <DisabledReason reason={untagged ? untaggedReason : undefined}>
        <Switch
          size="sm"
          aria-label={`${host.name} approved`}
          checked={host.approved}
          disabled={disabled || untagged}
          onCheckedChange={(approved) => {
            onSet(host, approved);
          }}
        />
      </DisabledReason>
    </SectionRow>
  );
}

/** What the machine itself reports: nothing, a serve configuration, or one it advertises. */
export function AnnouncementStatus({
  announced,
  active,
}: {
  readonly announced: boolean;
  readonly active: boolean;
}): ReactElement {
  if (!announced) {
    return <Badge tone="neutral">Not announced</Badge>;
  }

  return (
    <Badge tone={active ? "success" : "warning"}>
      {active ? "Advertising" : "Not advertising"}
    </Badge>
  );
}
