import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { PencilSimpleIcon } from "@phosphor-icons/react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { nodesQuery, servicesQuery } from "~/api/queries.ts";
import type { Node, Service } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { HostsSection } from "~/components/services/hosts-section.tsx";
import {
  reachStates,
  serviceLabel,
  serviceReach,
  serviceTitle,
} from "~/components/services/model.ts";
import { ServiceMenu } from "~/components/services/service-menu.tsx";
import { Badge } from "~/components/ui/badge.tsx";
import { KeyValueList } from "~/components/ui/definition-list.tsx";
import type { KeyValue } from "~/components/ui/definition-list.tsx";
import { RouteNotFound } from "~/components/ui/error-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";
import { isIpv4 } from "~/lib/ip.ts";

const emptyNodes: readonly Node[] = [];

export const Route = createFileRoute("/_app/services/$label")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(servicesQuery),
      can(context.me, "devices:core:read")
        ? context.queryClient.query(nodesQuery)
        : Promise.resolve(),
    ]);
  },
  component: ServicePage,
});

function ServicePage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { label } = Route.useParams();
  const { services } = useSuspenseQuery(servicesQuery).data;
  const nodes = useQuery({ ...nodesQuery, enabled: can(me, "devices:core:read") });
  const service = services.find((candidate) => serviceLabel(candidate.name) === label);

  useBreadcrumb(service === undefined ? null : serviceTitle(service));

  if (service === undefined) {
    return <RouteNotFound />;
  }

  return (
    <>
      <ServiceHeader service={service} me={me} />
      {/* Zero Trust's detail layout: the cards in the main column, a 260px details rail beside them
          that stays in view while the hosts scroll. */}
      <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-[minmax(0,1fr)_260px] xl:gap-6">
        <HostsSection
          service={service}
          services={services}
          nodes={nodes.data?.nodes ?? emptyNodes}
          me={me}
        />
        <aside className="flex flex-col gap-2 lg:sticky lg:top-[calc(var(--app-header-height,0px)+1rem)]">
          <h2 className="text-base font-medium text-kumo-default">Details</h2>
          <KeyValueList items={details(service)} className="border-t border-kumo-line px-0" />
        </aside>
      </div>
    </>
  );
}

function ServiceHeader({
  service,
  me,
}: {
  readonly service: Service;
  readonly me: Me;
}): ReactElement {
  const navigate = useNavigate();
  const { tone, label } = reachStates[serviceReach(service)];

  return (
    <PageHeader
      title={serviceTitle(service)}
      docs="ref/services"
      meta={
        <>
          <Badge tone={tone}>{label}</Badge>
          <span aria-hidden>·</span>
          <span className="font-mono">{service.name}</span>
          {service.comment === "" ? null : (
            <>
              <span aria-hidden>·</span>
              <span className="truncate">{service.comment}</span>
            </>
          )}
        </>
      }
      actions={
        <>
          {can(me, "services") ? (
            <LinkButton
              href={`/services/${serviceLabel(service.name)}/edit`}
              variant="secondary"
              icon={PencilSimpleIcon}
            >
              Edit
            </LinkButton>
          ) : (
            <Button variant="secondary" icon={PencilSimpleIcon} disabled>
              Edit
            </Button>
          )}
          <ServiceMenu
            service={service}
            me={me}
            onDeleted={() => {
              void navigate({ to: "/services" });
            }}
          />
        </>
      }
    />
  );
}

/** The facts about the service: what it is called, where it answers and what it was told to say. */
function details(service: Service): KeyValue[] {
  const items: KeyValue[] = [
    {
      key: "name",
      label: "Name",
      value: <span className="font-mono">{service.name}</span>,
      copy: service.name,
      copyLabel: "Copy service name",
    },
    {
      key: "dnsName",
      label: "DNS name",
      ...(service.dnsName === ""
        ? { value: <span className="text-kumo-subtle">No base domain</span> }
        : { value: service.dnsName, copy: service.dnsName, copyLabel: "Copy DNS name" }),
    },
  ];

  for (const address of service.addresses) {
    items.push({
      key: address,
      label: isIpv4(address) ? "IPv4" : "IPv6",
      value: <span className="font-mono">{address}</span>,
      copy: address,
      copyLabel: `Copy ${address}`,
    });
  }

  items.push(
    {
      key: "ports",
      label: "Ports",
      value:
        service.ports.length === 0 ? (
          <span className="text-kumo-subtle">Whatever the hosts serve</span>
        ) : (
          <span className="font-mono text-[0.9em]">{service.ports.join(", ")}</span>
        ),
    },
    { key: "created", label: "Created", value: <RelativeTime value={service.createdAt} /> },
  );

  return items;
}
