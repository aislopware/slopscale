import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { servicesQuery } from "~/api/queries.ts";
import type { Service } from "~/api/queries.ts";
import { reachableCount } from "~/components/services/model.ts";
import { ServicesTab } from "~/components/services/services-tab.tsx";
import { CommandBox } from "~/components/ui/command-text.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { PageWithRail, RailList, RailRow, RailSection } from "~/components/ui/page.tsx";
import { StatTile } from "~/components/ui/stat-tile.tsx";
import { docsUrl } from "~/lib/docs.ts";
import { textSearchSchema } from "~/lib/search-text.ts";

export const Route = createFileRoute("/_app/services/")({
  validateSearch: textSearchSchema,
  loader: async ({ context }) => {
    await context.queryClient.query(servicesQuery);
  },
  component: ServicesPage,
});

function ServicesPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { services } = useSuspenseQuery(servicesQuery).data;
  const reachable = reachableCount(services);

  const setSearch = (value: string): void => {
    void navigate({
      search: (current) => ({ ...current, q: value === "" ? undefined : value }),
      replace: true,
    });
  };

  return (
    <PageWithRail rail={<ServicesRail services={services} reachable={reachable} />}>
      <PageHeader
        title="Services"
        docs="ref/services"
        description="Names with tailnet addresses of their own that tagged machines host, so a client reaches one by name however it moves."
        meta={reachable === 1 ? "1 service reachable" : `${reachable} services reachable`}
      />
      <ServicesTab me={me} services={services} search={search.q ?? ""} onSearchChange={setSearch} />
    </PageWithRail>
  );
}

/** Machines that announce a service but wait for an operator to let them host it. */
function hostsToApprove(services: readonly Service[]): number {
  return services.reduce(
    (sum, service) => sum + service.hosts.filter((host) => host.announced && !host.approved).length,
    0,
  );
}

/**
 * The rail the dashboard's Workers list has: the figures first, then how a service comes to be
 * hosted, since every new one takes the same two commands on a machine.
 */
function ServicesRail({
  services,
  reachable,
}: {
  readonly services: readonly Service[];
  readonly reachable: number;
}): ReactElement {
  const waiting = hostsToApprove(services);

  return (
    <>
      <RailSection title="Services" size="lg">
        <div className="grid grid-cols-2 gap-3">
          <StatTile label="Services" value={services.length} />
          <StatTile label="Reachable" value={reachable} />
          <StatTile
            label="Hosts to approve"
            value={waiting}
            tone={waiting === 0 ? "default" : "warning"}
            className="col-span-2"
            hint="Machines that announce a service you have not let them host"
          />
        </div>
      </RailSection>
      <RailSection title="Host a service">
        <RailList>
          <RailRow
            title="1. Create the service here"
            description="Its name, the ports it answers on and the tags of the machines that may host it."
          />
          <div className="flex flex-col gap-2 py-3">
            <span className="text-sm font-medium text-kumo-default">
              2. Serve it on a tagged machine
            </span>
            <CommandBox
              size="sm"
              wrap
              command="tailscale serve --service=svc:web --https=443 localhost:8080"
            />
            <CommandBox size="sm" wrap command="tailscale serve advertise svc:web" />
          </div>
          <RailRow
            title="3. Approve the machine as a host"
            description="Open the service and switch the machine on under Hosts."
            link={{ href: docsUrl("ref/services"), label: "About services", external: true }}
          />
        </RailList>
      </RailSection>
    </>
  );
}
