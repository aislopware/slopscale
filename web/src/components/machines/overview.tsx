import { Collapsible } from "@cloudflare/kumo/components/collapsible";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { CaretRightIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import type { NodeHardwareAttestation, NodeTpm } from "~/api/schema.gen.ts";
import { FactGrid } from "~/components/machines/fact-grid.tsx";
import type { Fact } from "~/components/machines/fact-grid.tsx";
import { CopyText } from "~/components/ui/copy-text.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import { OsMark } from "~/components/ui/os-mark.tsx";
import { Section } from "~/components/ui/section.tsx";
import { Status, StatusDetail } from "~/components/ui/status.tsx";
import { TagList } from "~/components/ui/tag.tsx";
import { isTagged, userLabel } from "~/lib/node.ts";
import { osLabel } from "~/lib/os.ts";
import { formatRelative, parseTime } from "~/lib/time.ts";

const registerMethods: Record<string, string> = {
  REGISTER_METHOD_AUTH_KEY: "Pre-auth key",
  REGISTER_METHOD_CLI: "Command line",
  REGISTER_METHOD_OIDC: "OpenID Connect",
};

const caretSize = 14;

/**
 * What the machine is and how it joined, as the settings card of a Zero Trust detail page: a grid
 * of short facts, with the two long keys folded away under it. Its identity (name, ID, addresses,
 * times) is in the details rail beside it, so no fact on the page is written twice.
 */
export function OverviewSection({ node }: { readonly node: Node }): ReactElement {
  return (
    <Section title="Overview" bodyClassName="p-0">
      <FactGrid facts={overview(node)} />
      <Keys node={node} />
    </Section>
  );
}

function overview(node: Node): Fact[] {
  return [
    { key: "hostname", label: "Hostname", value: <CopyText value={node.name} /> },
    { key: "owner", label: "Owner", value: <Owner node={node} /> },
    {
      key: "method",
      label: "Registered with",
      value: registerMethods[node.registerMethod] ?? "Unknown",
    },
    { key: "client", label: "Client", value: <Client node={node} /> },
    { key: "platform", label: "Platform", value: <Platform node={node} /> },
    { key: "funnel", label: "Funnel", value: <OnOff on={node.funnelEnabled} /> },
    { key: "attestation", label: "Hardware attestation", value: <Attestation node={node} /> },
    ...(node.tpm === undefined
      ? []
      : [{ key: "tpm", label: "TPM", value: <span className="block">{tpmText(node.tpm)}</span> }]),
    // The Preferences card carries the line that turns it on, with room to read it.
    { key: "remote", label: "Remote configuration", value: <OnOff on={node.remoteConfig} /> },
  ];
}

/**
 * The Tailscale client version the machine reported, and whether a newer stable one exists. The
 * update itself is an item in the Actions menu with the other things done to the machine; a button
 * here beside the version and the state made one fact three things.
 */
function Client({ node }: { readonly node: Node }): ReactElement {
  if (node.clientVersion === "") {
    return <span className="text-kumo-subtle">Unknown until it connects</span>;
  }

  if (!node.updateAvailable) {
    return <span>{node.clientVersion}</span>;
  }

  return (
    <span className="inline-flex flex-wrap items-center gap-x-2 gap-y-0.5">
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

/** The operating system the client reported, its mark beside the name. */
function Platform({ node }: { readonly node: Node }): ReactElement {
  if (node.os === "") {
    return <span className="text-kumo-subtle">Unknown until it connects</span>;
  }

  return (
    <span className="inline-flex items-center gap-1.5">
      <OsMark os={node.os} version={node.osVersion} />
      {osLabel(node.os, node.osVersion)}
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
 * A setting that is either on or off, as the other facts read: the state alone. What to do about an
 * "Off" belongs where the setting is changed, not in a grid of facts.
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
    <Collapsible.Root className="border-t border-kumo-line">
      <Collapsible.Trigger className="group flex w-full items-center gap-1.5 px-4 py-2.5 text-left text-sm text-kumo-subtle hover:bg-kumo-tint focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none">
        <CaretRightIcon size={caretSize} className="group-data-[panel-open]:rotate-90" />
        Keys
      </Collapsible.Trigger>
      <Collapsible.Panel>
        <DefinitionList
          wrap
          className="border-t border-kumo-line"
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

function Owner({ node }: { readonly node: Node }): ReactElement {
  if (isTagged(node)) {
    return <TagList tags={node.tags} size="sm" />;
  }

  return <span className="block truncate">{userLabel(node.user)}</span>;
}
