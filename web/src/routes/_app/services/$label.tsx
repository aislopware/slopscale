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
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";
import { RouteNotFound } from "~/components/ui/error-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Section } from "~/components/ui/section.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";
import { isIpv4 } from "~/lib/ip.ts";
import { requireScope } from "~/lib/require-scope.ts";

const emptyNodes: readonly Node[] = [];

export const Route = createFileRoute("/_app/services/$label")({
  beforeLoad: requireScope("services:read"),
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
      <div className="grid grid-cols-1 items-start gap-6 min-[1200px]:grid-cols-[minmax(0,2fr)_minmax(0,22rem)]">
        <HostsSection
          service={service}
          services={services}
          nodes={nodes.data?.nodes ?? emptyNodes}
          me={me}
        />
        <Section title="Details">
          <DefinitionList items={details(service)} />
        </Section>
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
        <ServiceMenu
          service={service}
          me={me}
          onDeleted={() => {
            void navigate({ to: "/services" });
          }}
        />
      }
    />
  );
}

/** The facts about the service: what it is called, where it answers and what it was told to say. */
function details(service: Service): Definition[] {
  const items: Definition[] = [
    { key: "name", label: "Name", value: service.name, copy: service.name },
    {
      key: "dnsName",
      label: "DNS name",
      ...(service.dnsName === ""
        ? { value: <span className="text-kumo-subtle">No base domain</span> }
        : { value: service.dnsName, copy: service.dnsName }),
    },
  ];

  for (const address of service.addresses) {
    items.push({
      key: address,
      label: isIpv4(address) ? "IPv4" : "IPv6",
      value: address,
      copy: address,
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
