import { Badge } from "@cloudflare/kumo/components/badge";
import { Collapsible } from "@cloudflare/kumo/components/collapsible";
import { CaretRightIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";
import { Section } from "~/components/ui/section.tsx";
import { Status } from "~/components/ui/status.tsx";
import { isTagged, nodeName, userLabel } from "~/lib/node.ts";
import { formatAbsolute, parseTime } from "~/lib/time.ts";

const registerMethods: Record<string, string> = {
  REGISTER_METHOD_AUTH_KEY: "Pre-auth key",
  REGISTER_METHOD_CLI: "Command line",
  REGISTER_METHOD_OIDC: "OpenID Connect",
};

const caretSize = 14;

/** How the machine was registered and who it answers to. */
export function OverviewSection({ node }: { readonly node: Node }): ReactElement {
  return (
    <Section title="Overview">
      <DefinitionList items={overview(node)} columns={2} />
      <Keys node={node} />
    </Section>
  );
}

function overview(node: Node): Definition[] {
  return [
    { key: "hostname", label: "Hostname", value: node.name, copy: node.name },
    { key: "owner", label: "Owner", value: <Owner node={node} /> },
    { key: "id", label: "Machine ID", value: node.id, copy: node.id },
    {
      key: "method",
      label: "Registered with",
      value: registerMethods[node.registerMethod] ?? "Unknown",
    },
    { key: "created", label: "Created", value: <Timestamp value={node.createdAt} /> },
    { key: "expiry", label: "Key expiry", value: <Expiry node={node} /> },
    { key: "funnel", label: "Funnel", value: <Funnel node={node} /> },
    { key: "client", label: "Client", value: <Client node={node} /> },
  ];
}

/** The Tailscale client version the machine reported, and whether a newer stable one exists. */
function Client({ node }: { readonly node: Node }): ReactElement {
  if (node.clientVersion === "") {
    return <span className="text-kumo-subtle">Unknown until it connects</span>;
  }

  return (
    <span className="flex flex-wrap items-center gap-2">
      <span>{node.clientVersion}</span>
      {node.updateAvailable ? <Status tone="warning">Update available</Status> : null}
    </span>
  );
}

/** Whether the client reports a Funnel endpoint, which the ingress delivers public traffic to. */
function Funnel({ node }: { readonly node: Node }): ReactElement {
  return node.funnelEnabled ? (
    <Status tone="success">On</Status>
  ) : (
    <span className="text-kumo-subtle">Off</span>
  );
}

/**
 * The two long keys, folded away: they are how the machine proves who it is rather than something
 * an operator reads, and at full width they drown out every fact above them.
 */
function Keys({ node }: { readonly node: Node }): ReactElement {
  return (
    <Collapsible.Root className="border-t border-kumo-hairline">
      <Collapsible.Trigger className="group flex w-full items-center gap-1.5 px-5 py-2.5 text-left text-kumo-subtle hover:bg-kumo-tint focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none">
        <CaretRightIcon size={caretSize} className="group-data-[panel-open]:rotate-90" />
        Keys
      </Collapsible.Trigger>
      <Collapsible.Panel>
        <DefinitionList
          wrap
          className="border-t border-kumo-hairline"
          items={[
            { key: "nodeKey", label: "Node key", value: node.nodeKey, copy: node.nodeKey },
            {
              key: "machineKey",
              label: "Machine key",
              value: node.machineKey,
              copy: node.machineKey,
            },
          ]}
        />
      </Collapsible.Panel>
    </Collapsible.Root>
  );
}

/** Every way to reach the machine, each one click-to-copy. */
export function AddressesSection({ node }: { readonly node: Node }): ReactElement {
  const addresses: Definition[] = node.ipAddresses.map((address) => ({
    key: address,
    label: address.includes(":") ? "IPv6" : "IPv4",
    value: address,
    copy: address,
  }));

  return (
    <Section title="Addresses">
      <DefinitionList
        items={[
          { key: "name", label: "Name", value: nodeName(node), copy: nodeName(node) },
          ...addresses,
        ]}
      />
    </Section>
  );
}

function Owner({ node }: { readonly node: Node }): ReactElement {
  if (isTagged(node)) {
    return (
      <span className="flex flex-wrap justify-end gap-1">
        {node.tags.map((tag) => (
          <Badge key={tag} variant="secondary">
            <span className="font-mono">{tag}</span>
          </Badge>
        ))}
      </span>
    );
  }

  return <span>{userLabel(node.user)}</span>;
}

function Expiry({ node }: { readonly node: Node }): ReactElement {
  return parseTime(node.expiry) === null ? <span>Never</span> : <Timestamp value={node.expiry} />;
}

function Timestamp({ value }: { readonly value: string | null }): ReactElement {
  const date = parseTime(value);

  return date === null ? (
    <span className="text-kumo-subtle">Unknown</span>
  ) : (
    <span>{formatAbsolute(date)}</span>
  );
}
