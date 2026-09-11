import { Collapsible } from "@cloudflare/kumo/components/collapsible";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { CaretRightIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import type { NodeHardwareAttestation, NodeTpm } from "~/api/schema.gen.ts";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";
import { Section } from "~/components/ui/section.tsx";
import { Status, StatusDetail } from "~/components/ui/status.tsx";
import { TagList } from "~/components/ui/tag.tsx";
import { isTagged, nodeName, userLabel } from "~/lib/node.ts";
import { formatAbsolute, formatRelative, parseTime } from "~/lib/time.ts";

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
    { key: "funnel", label: "Funnel", value: <OnOff on={node.funnelEnabled} /> },
    { key: "client", label: "Client", value: <Client node={node} /> },
    { key: "attestation", label: "Hardware attestation", value: <Attestation node={node} /> },
    ...(node.tpm === undefined
      ? []
      : [{ key: "tpm", label: "TPM", value: tpmText(node.tpm), copy: tpmText(node.tpm) }]),
    // The Preferences section carries the line that turns it on, with room to read it.
    { key: "remote", label: "Remote configuration", value: <OnOff on={node.remoteConfig} /> },
  ];
}

/**
 * The Tailscale client version the machine reported, and whether a newer stable one exists. The
 * update itself is an item in the Actions menu with the other things done to the machine; a button
 * here beside the version and the state made one row three things.
 */
function Client({ node }: { readonly node: Node }): ReactElement {
  if (node.clientVersion === "") {
    return <span className="text-kumo-subtle">Unknown until it connects</span>;
  }

  if (!node.updateAvailable) {
    return <span>{node.clientVersion}</span>;
  }

  return (
    <span className="inline-flex items-center gap-2">
      <span>{node.clientVersion}</span>
      <StatusDetail
        tone="warning"
        label="Update available"
        title="A newer stable Tailscale is out"
        detail={
          node.online
            ? "Update client in the Actions menu asks the machine to install it. Tailscale restarts there while it does."
            : "Update client in the Actions menu asks the machine to install it, once it is connected again."
        }
      />
    </span>
  );
}

/**
 * What the machine's hardware attestation key proved. A record with `attested` false is a machine
 * that once proved itself and no longer does, which is worth saying out loud; no record at all is
 * simply a client that never signed a map request with one.
 */
function Attestation({ node }: { readonly node: Node }): ReactElement {
  const record = node.hardwareAttestation;

  if (record === undefined) {
    return <span className="text-kumo-subtle">Not attested</span>;
  }

  const state = record.attested ? (
    <Status tone="success">Attested</Status>
  ) : (
    <Status tone="warning">Lost</Status>
  );
  const detail = attestationDetail(record);

  return detail === null ? state : <Tooltip content={detail}>{state}</Tooltip>;
}

/** When it was proved and when the key behind it last changed; null while neither is known. */
function attestationDetail(record: NodeHardwareAttestation): string | null {
  const attestedAt = parseTime(record.attestedAt);
  const changedAt = parseTime(record.keyChangedAt);
  const parts = [
    ...(attestedAt === null ? [] : [`Attested ${formatRelative(attestedAt)}`]),
    ...(changedAt === null ? [] : [`key changed ${formatRelative(changedAt)}`]),
  ];

  return parts.length === 0 ? null : parts.join(" · ");
}

/** "MSFT Microsoft, firmware 8410" — whichever of the three the client filled in. */
function tpmText(tpm: NodeTpm): string {
  const maker = [tpm.manufacturer, tpm.vendor].filter((part) => part !== "").join(" ");
  const parts = [
    ...(maker === "" ? [] : [maker]),
    ...(tpm.firmwareVersion === 0 ? [] : [`firmware ${String(tpm.firmwareVersion)}`]),
  ];

  return parts.length === 0 ? "Present" : parts.join(", ");
}

/**
 * A setting that is either on or off, as the other rows of this list read: the state alone. What to
 * do about an "Off" belongs where the setting is changed, not in a row of facts.
 */
function OnOff({ on }: { readonly on: boolean }): ReactElement {
  return on ? <Status tone="success">On</Status> : <span className="text-kumo-subtle">Off</span>;
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
    return <TagList tags={node.tags} size="sm" className="justify-end" />;
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
