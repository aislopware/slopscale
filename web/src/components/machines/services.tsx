import { Switch } from "@cloudflare/kumo/components/switch";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Node, Service } from "~/api/queries.ts";
import { useNodeMutations } from "~/components/machines/mutations.ts";
import { AnnouncementStatus, untaggedReason } from "~/components/services/hosts-section.tsx";
import { machineServices, serviceLabel, withServiceApproved } from "~/components/services/model.ts";
import type { MachineService } from "~/components/services/model.ts";
import { Code } from "~/components/ui/code.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";
import { isTagged } from "~/lib/node.ts";

/**
 * The services the machine announces or may host, approved one at a time. It stays off the page
 * while the tailnet has no services and this machine has nothing to say about them; once services
 * exist, a machine that announces none says so, because that is where the operator looks first.
 */
export function ServicesSection({
  node,
  services,
  canEdit,
}: {
  readonly node: Node;
  /** Every service the tailnet has, to tell an announcement of a known service from a stray one. */
  readonly services: readonly Service[];
  readonly canEdit: boolean;
}): ReactElement | null {
  const { setServices } = useNodeMutations();
  const rows = machineServices(node, services);
  const tagged = isTagged(node);

  if (rows.length === 0 && services.length === 0) {
    return null;
  }

  function set(name: string, approved: boolean): void {
    setServices.mutate(
      {
        params: { path: { nodeId: node.id } },
        body: { services: withServiceApproved(node.approvedServices, name, approved) },
      },
      {
        onError: (error) => {
          toast.error(errorMessage(error));
        },
      },
    );
  }

  return (
    <Section
      title="Services"
      description="Only an approved machine hosts a service, and only a tagged machine may be approved."
    >
      {rows.length === 0 ? (
        <SectionRow className="text-kumo-subtle">
          Nothing announced. Run <Code>tailscale serve --service=svc:name</Code> on the machine,
          then <Code>tailscale serve advertise</Code>.
        </SectionRow>
      ) : (
        rows.map((service) => (
          <ServiceRow
            key={service.name}
            service={service}
            tagged={tagged}
            disabled={!canEdit || setServices.isPending}
            onSet={set}
          />
        ))
      )}
    </Section>
  );
}

/** Why the machine cannot be approved for this service, or undefined when it can. */
function approvalReason(service: MachineService, tagged: boolean): string | undefined {
  if (!tagged) {
    return untaggedReason;
  }

  return service.known ? undefined : "No service goes by this name";
}

function ServiceRow({
  service,
  tagged,
  disabled,
  onSet,
}: {
  readonly service: MachineService;
  readonly tagged: boolean;
  readonly disabled: boolean;
  readonly onSet: (name: string, approved: boolean) => void;
}): ReactElement {
  const reason = approvalReason(service, tagged);

  return (
    <SectionRow className="flex flex-wrap items-start justify-between gap-3 py-3">
      <div className="flex min-w-0 flex-col gap-1">
        <ServiceName service={service} />
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-kumo-subtle">
          <AnnouncementStatus announced={service.announced} active={service.active} />
          {service.ports.length === 0 ? null : <Code>{service.ports.join(", ")}</Code>}
        </div>
      </div>
      <DisabledReason reason={reason}>
        <Switch
          size="sm"
          aria-label={`${service.name} approved`}
          checked={service.approved}
          disabled={disabled || reason !== undefined}
          onCheckedChange={(approved) => {
            onSet(service.name, approved);
          }}
        />
      </DisabledReason>
    </SectionRow>
  );
}

/** A service the tailnet knows links to its page; one nobody created is only a name. */
function ServiceName({ service }: { readonly service: MachineService }): ReactElement {
  const name = <span className="truncate font-mono text-[0.9em]">{service.name}</span>;

  if (!service.known) {
    return name;
  }

  return (
    <Link
      to="/services/$label"
      params={{ label: serviceLabel(service.name) }}
      className="flex min-w-0 font-medium text-kumo-default hover:text-kumo-link hover:underline"
    >
      {name}
    </Link>
  );
}
