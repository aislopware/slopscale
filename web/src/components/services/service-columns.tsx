import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { reachStates } from "~/components/services/model.ts";
import type { ServiceRow } from "~/components/services/model.ts";
import { ServiceMenu } from "~/components/services/service-menu.tsx";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { CopyText } from "~/components/ui/copy-text.tsx";
import { Status } from "~/components/ui/status.tsx";

const helper = createAppColumnHelper<ServiceRow>();

export const serviceColumns = helper.columns([
  helper.accessor((service) => `${service.name} ${service.displayName}`, {
    id: "name",
    header: "Service",
    enableSorting: true,
    cell: ({ row }) => <NameCell service={row.original} />,
    meta: { className: "w-[22%] min-w-44" },
  }),
  helper.accessor((service) => service.dnsName, {
    id: "dnsName",
    header: "DNS name",
    enableSorting: false,
    cell: ({ row }) => <DnsNameCell service={row.original} />,
    meta: { className: "min-w-40" },
  }),
  helper.accessor((service) => service.addresses.join(" "), {
    id: "addresses",
    header: "Addresses",
    enableSorting: false,
    cell: ({ row }) => <AddressesCell service={row.original} />,
    meta: { className: "hidden lg:table-cell" },
  }),
  helper.accessor((service) => service.ports.join(" "), {
    id: "ports",
    header: "Ports",
    enableSorting: false,
    cell: ({ row }) => <PortsCell service={row.original} />,
    meta: { className: "hidden md:table-cell" },
  }),
  helper.accessor((service) => service.hostNames, {
    id: "hosts",
    header: "Hosts",
    enableSorting: false,
    cell: ({ row }) => <HostsCell service={row.original} />,
    meta: { className: "min-w-44" },
  }),
  helper.accessor((service) => service.comment, {
    id: "comment",
    header: "Comment",
    enableSorting: false,
    cell: ({ row }) => (
      <span className="line-clamp-2 text-kumo-subtle">{row.original.comment}</span>
    ),
    meta: { className: "hidden xl:table-cell" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const { me } = table.options.meta ?? {};

      return me === undefined ? null : <ServiceMenu service={row.original} me={me} />;
    },
    meta: { className: "w-12 text-right", sticky: "right" },
  }),
]);

/**
 * The `svc:` name is what the policy file and the machine's serve command refer to, so it is what
 * the row leads with; the display name clients show goes under it.
 */
function NameCell({ service }: { readonly service: ServiceRow }): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <Link
        to="/services/$label"
        params={{ label: service.label }}
        className="block truncate font-mono text-[0.9em] font-medium text-kumo-default hover:text-kumo-link hover:underline focus-visible:underline"
      >
        {service.name}
      </Link>
      {service.displayName === "" ? null : (
        <span className="truncate text-xs text-kumo-subtle">{service.displayName}</span>
      )}
    </div>
  );
}

function DnsNameCell({ service }: { readonly service: ServiceRow }): ReactElement {
  if (service.dnsName === "") {
    return <span className="text-kumo-subtle">No base domain</span>;
  }

  return <CopyText value={service.dnsName} />;
}

function AddressesCell({ service }: { readonly service: ServiceRow }): ReactElement {
  return (
    <div className="flex flex-col items-start gap-0.5 whitespace-nowrap text-kumo-subtle">
      {service.addresses.map((address) => (
        <CopyText key={address} value={address} className="max-w-none" />
      ))}
    </div>
  );
}

function PortsCell({ service }: { readonly service: ServiceRow }): ReactElement {
  if (service.ports.length === 0) {
    return <span className="text-kumo-subtle">Whatever the hosts serve</span>;
  }

  return <span className="font-mono text-[0.9em]">{service.ports.join(", ")}</span>;
}

/** Where the service stands and who serves it: the state, then the primary host or the count. */
function HostsCell({ service }: { readonly service: ServiceRow }): ReactElement {
  const { tone, label } = reachStates[service.reach];

  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <Status tone={tone}>{label}</Status>
      <span className="truncate text-xs text-kumo-subtle">{hostSummary(service)}</span>
    </div>
  );
}

function hostSummary(service: ServiceRow): string {
  if (service.hosts.length === 0) {
    return "No machine announces it";
  }

  const count = service.hosts.length === 1 ? "1 host" : `${service.hosts.length} hosts`;

  return service.primaryHost === "" ? count : `${count} · ${service.primaryHost} serving`;
}
