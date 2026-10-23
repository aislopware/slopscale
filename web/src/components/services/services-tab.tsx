import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { cn } from "@cloudflare/kumo/utils";
import { ArrowRightIcon, HardDrivesIcon, PlugIcon, PlusIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useDeferredValue, useMemo } from "react";
import type { ReactElement } from "react";

import type { Service } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { reachStates, servicePathLabel, toServiceRows } from "~/components/services/model.ts";
import type { ServiceRow } from "~/components/services/model.ts";
import { ServiceMenu } from "~/components/services/service-menu.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { rowLinkClass } from "~/components/table/styles.ts";
import { TableFooter, TableGroup, TableToolbar } from "~/components/table/toolbar.tsx";
import { Badge } from "~/components/ui/badge.tsx";
import { cardClass } from "~/components/ui/card.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";

export interface ServicesTabProps {
  readonly me: Me;
  readonly services: readonly Service[];
  readonly search: string;
  readonly onSearchChange: (value: string) => void;
}

/** Whether a service matches the search on any of what its card shows, hosts included. */
function matches(service: ServiceRow, query: string): boolean {
  const text = [
    service.name,
    service.displayName,
    service.dnsName,
    service.addresses.join(" "),
    service.ports.join(" "),
    service.hostNames,
    service.comment,
  ]
    .join(" ")
    .toLowerCase();

  return text.includes(query);
}

/**
 * The tailnet's services as the dashboard lists Workers: a card per service, its name and the DNS
 * name clients reach it at on a white layer, and a strip under it with where it stands and who
 * hosts it. A service is a named endpoint like a Worker, few to a tailnet, and what an operator
 * wants from the list is which of them answer; a card says that without a column to read across.
 */
export function ServicesTab({
  me,
  services,
  search,
  onSearchChange,
}: ServicesTabProps): ReactElement {
  const canEdit = can(me, "services");
  const query = useDeferredValue(search);
  const rows = useMemo(
    () => toServiceRows(services).toSorted((left, right) => left.name.localeCompare(right.name)),
    [services],
  );
  const needle = query.trim().toLowerCase();
  const shown = needle === "" ? rows : rows.filter((row) => matches(row, needle));
  const total = services.length;

  return (
    <>
      <TableToolbar
        actions={
          canEdit ? (
            <LinkButton href="/services/new" variant="primary" icon={PlusIcon}>
              New service
            </LinkButton>
          ) : (
            <Button variant="primary" icon={PlusIcon} disabled>
              New service
            </Button>
          )
        }
      >
        <SearchInput
          value={search}
          placeholder="Search by name, address, port or host"
          onValueChange={onSearchChange}
        />
      </TableToolbar>
      <TableGroup>
        {shown.length === 0 ? (
          <div className={cardClass}>
            {total === 0 ? (
              <SectionEmpty
                title="No services"
                description="Nothing announces a service yet. Create one here, then serve it from a machine that carries one of its tags."
              />
            ) : (
              <SectionEmpty
                title="No services match"
                contents={
                  <Button
                    variant="secondary"
                    onClick={() => {
                      onSearchChange("");
                    }}
                  >
                    Clear search
                  </Button>
                }
              />
            )}
          </div>
        ) : (
          <ul className="grid gap-4" aria-label="Services">
            {shown.map((service) => (
              <ServiceCard key={service.label} service={service} me={me} />
            ))}
          </ul>
        )}
        {total === 0 ? null : (
          <TableFooter>{`Showing ${shown.length} of ${countServices(total)}`}</TableFooter>
        )}
      </TableGroup>
    </>
  );
}

/**
 * One service as the dashboard's Worker card: the white top layer with an icon tile, the name (16px
 * medium, the link to its page) over its `svc:` name and DNS name, its state and the ⋯ menu; the
 * canvas strip under it with the hosts on the left and the ports and addresses on the right.
 */
function ServiceCard({
  service,
  me,
}: {
  readonly service: ServiceRow;
  readonly me: Me;
}): ReactElement {
  const { tone, label } = reachStates[service.reach];

  return (
    <li className="overflow-hidden rounded-lg bg-kumo-canvas ring ring-kumo-line">
      <div
        className={cn(
          "relative flex min-h-18.5 items-center gap-3.5 py-3 pr-3 pl-4",
          "rounded-lg bg-kumo-base ring ring-kumo-line",
        )}
      >
        <span className="flex size-9 shrink-0 items-center justify-center rounded-md text-kumo-subtle ring ring-kumo-line">
          <HardDrivesIcon aria-hidden className="size-5" />
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <Link
            to="/services/$label"
            params={{ label: servicePathLabel(service.name) }}
            className="w-fit max-w-full truncate text-lg font-medium text-kumo-default hover:underline focus-visible:rounded-sm focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none"
          >
            {service.displayName === "" ? service.name : service.displayName}
          </Link>
          <span className="flex min-w-0 flex-wrap items-center gap-x-2 text-sm text-kumo-subtle">
            {service.displayName === "" ? null : <span className="font-mono">{service.name}</span>}
            {service.dnsName === "" ? (
              <span>No base domain</span>
            ) : (
              <span className="truncate">{service.dnsName}</span>
            )}
            {service.comment === "" ? null : <span className="truncate">{service.comment}</span>}
          </span>
        </div>
        <Badge tone={tone}>{label}</Badge>
        <ServiceMenu service={service} me={me} />
      </div>
      <div className="flex min-h-10 flex-wrap items-center justify-between gap-x-4 gap-y-1 px-4 py-2 text-sm text-kumo-subtle">
        <Link
          to="/services/$label"
          params={{ label: servicePathLabel(service.name) }}
          className={cn("inline-flex items-center gap-1", rowLinkClass)}
        >
          {hostSummary(service)}
          <ArrowRightIcon aria-hidden className="size-3" />
        </Link>
        <span className="flex flex-wrap items-center gap-x-4 gap-y-1 tabular-nums">
          <span className="inline-flex items-center gap-1.5">
            <PlugIcon aria-hidden className="size-4" />
            {service.ports.length === 0 ? (
              "Whatever the hosts serve"
            ) : (
              <span className="font-mono">{service.ports.join(", ")}</span>
            )}
          </span>
          {service.addresses.map((address) => (
            <span key={address} className="font-mono">
              {address}
            </span>
          ))}
        </span>
      </div>
    </li>
  );
}

/** Who hosts the service: nobody yet, how many, and which one clients reach right now. */
function hostSummary(service: ServiceRow): string {
  if (service.hosts.length === 0) {
    return "No machine announces it";
  }

  const count = service.hosts.length === 1 ? "1 host" : `${service.hosts.length} hosts`;

  return service.primaryHost === "" ? count : `${count} · ${service.primaryHost} serving`;
}

function countServices(total: number): string {
  return total === 1 ? "1 service" : `${total} services`;
}
