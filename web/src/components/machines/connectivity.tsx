import { LinkButton } from "@cloudflare/kumo/components/button";
import { useQuery } from "@tanstack/react-query";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import { nodeTlsCertQuery } from "~/api/queries.ts";
import type { Node } from "~/api/queries.ts";
import type { NodeDerpLatency, NodeNetInfo, NodeTlsCertStatus } from "~/api/schema.gen.ts";
import {
  homeLatency,
  linkLabel,
  msLabel,
  natLabel,
  natTone,
  otherLatencies,
  portMapLabel,
  yesNo,
} from "~/components/derp/latency-model.ts";
import { signInCommand } from "~/components/machines/connect.ts";
import { CommandBox } from "~/components/ui/command-text.tsx";
import { FactGrid } from "~/components/ui/fact-grid.tsx";
import type { Fact } from "~/components/ui/fact-grid.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { Status, StatusDetail } from "~/components/ui/status.tsx";
import { nodeName, nodeStatus } from "~/lib/node.ts";

/**
 * How the machine reaches the rest of the tailnet, the Mesh node page's connection card: the relay
 * region it homes on, and what its own network report says about the path out. The client sends the
 * report, so it is what the machine believes rather than something the server measured. A machine
 * that is not connected gets the next step first, the way the Mesh node page puts its connect
 * commands in this card while a node is offline, and its last report under that on Diagnostics.
 */
export function ConnectivitySection({
  node,
  preview = false,
}: {
  readonly node: Node;
  /**
   * The Overview tab's compact card: the report's facts alone, without the certificate (a live
   * round trip to the client) or the other regions, and a link to the Diagnostics tab for both.
   */
  readonly preview?: boolean;
}): ReactElement {
  // Any machine may hold a certificate for its own name: ordinary HTTPS Serve fetches one without
  // Funnel or a VIP service, and one fetched by hand is a certificate all the same. Only a
  // connected machine is asked, because the question is a live round trip to the client, which
  // answers "no certificate" as cheaply as it answers with one.
  const cert = useQuery({ ...nodeTlsCertQuery(node.id), enabled: node.online && !preview });
  const { netInfo } = node;
  const others = netInfo === undefined ? [] : otherLatencies(netInfo);

  const tls: Fact[] =
    node.online && !preview
      ? [
          {
            key: "tls",
            label: "TLS certificate",
            value: (
              <CertStatus
                status={cert.data}
                loading={cert.isPending}
                error={cert.isError ? errorMessage(cert.error) : undefined}
              />
            ),
          },
        ]
      : [];

  return (
    <Section
      title="Connection"
      bodyClassName="p-0"
      {...(preview
        ? {
            actions: (
              <LinkButton href={`/machines/${node.id}/diagnostics`} variant="ghost" size="xs">
                View diagnostics
              </LinkButton>
            ),
          }
        : {})}
    >
      {node.online ? null : <NextStep node={node} />}
      <Report node={node} preview={preview} tls={tls} />
      {netInfo === undefined && tls.length > 0 ? <FactGrid facts={tls} /> : null}
      {preview || others.length === 0 ? null : <OtherRegions regions={others} />}
    </Section>
  );
}

/**
 * The client's network report: what a connected machine says now, or, on Diagnostics, the last
 * thing a disconnected one said, under its next step. A connected machine that has not sent one yet
 * says so; a disconnected one leaves the card to its next step.
 */
function Report({
  node,
  preview,
  tls,
}: {
  readonly node: Node;
  readonly preview: boolean;
  readonly tls: readonly Fact[];
}): ReactElement | null {
  const { netInfo } = node;

  if (netInfo === undefined) {
    return node.online ? (
      <SectionEmpty
        title="No network report yet"
        description="The client sends one when it connects: its home relay, the link it is on and how it gets through NAT."
      />
    ) : null;
  }

  if (node.online) {
    return <FactGrid facts={[...facts(netInfo), ...tls]} />;
  }

  return preview ? null : (
    <>
      <p className="border-t border-kumo-line px-4 pt-3 text-xs text-kumo-subtle">Last report</p>
      <FactGrid facts={facts(netInfo)} />
    </>
  );
}

/**
 * What brings a machine that is not connected back, by why it is not: a stopped or signed-out
 * client rejoins with the join command's sign-in half (the same flags, so `tailscale up` does not
 * refuse over one left out), an expired key signs in again, and a suspended or unapproved machine
 * waits on an administrator, which no command on the machine changes.
 */
function NextStep({ node }: { readonly node: Node }): ReactElement {
  const name = nodeName(node);
  const status = nodeStatus(node);

  if (status === "suspended") {
    return (
      <Guide
        title={`${name} is suspended`}
        description="It stays off the tailnet until an administrator resumes it from the danger zone in its settings."
        action={
          <LinkButton href={`/machines/${node.id}/settings`} variant="secondary" size="sm">
            Open settings
          </LinkButton>
        }
      />
    );
  }

  if (status === "pending") {
    return (
      <Guide
        title={`${name} is waiting for approval`}
        description="It joins the tailnet as soon as an administrator approves it."
      />
    );
  }

  if (status === "expired") {
    return (
      <Guide
        title={`Sign ${name} in again`}
        description="Its key has expired. Run this on the machine to sign in again; a tagged machine signs in with a new pre-auth key, given as --authkey, instead."
        command={`${signInCommand()} --force-reauth`}
      />
    );
  }

  return (
    <Guide
      title={`Reconnect ${name}`}
      description="If Tailscale is stopped or signed out on the machine, run this there, with sudo on Linux and macOS. It rejoins as the same machine."
      command={signInCommand()}
    />
  );
}

/** The next step as the Mesh node page states it: a centred title and sentence, then the command. */
function Guide({
  title,
  description,
  command,
  action,
}: {
  readonly title: string;
  readonly description: string;
  readonly command?: string;
  readonly action?: ReactElement;
}): ReactElement {
  return (
    <div className="flex flex-col items-center gap-4 px-4 py-8 text-center">
      <div className="flex max-w-[60ch] flex-col gap-1 text-pretty">
        <h3 className="text-lg font-semibold text-kumo-default">{title}</h3>
        <p className="text-kumo-subtle">{description}</p>
      </div>
      {command === undefined ? null : (
        <CommandBox command={command} wrap className="w-full max-w-2xl text-left" />
      )}
      {action}
    </div>
  );
}

/**
 * The certificate the machine caches for its own MagicDNS name. Serve and Funnel need it and it
 * fails quietly when it cannot be renewed, which is exactly the case worth showing.
 */
function CertStatus({
  status,
  loading,
  error,
}: {
  readonly status: NodeTlsCertStatus | undefined;
  readonly loading: boolean;
  readonly error: string | undefined;
}): ReactElement {
  if (error !== undefined) {
    return <CertProblem label="Unavailable" reason={error} />;
  }

  if (status === undefined) {
    return <span className="text-kumo-subtle">{loading ? "Asking the machine…" : "Unknown"}</span>;
  }

  // A client sends its reason with every state, "no certificate"
  // included, so the states come before the reason: a machine that never
  // fetched one has nothing wrong with it.
  if (status.missing) {
    return <span className="text-kumo-subtle">Missing</span>;
  }

  if (status.expired) {
    return <Status tone="danger">Expired</Status>;
  }

  if (status.error !== undefined && status.error !== "") {
    return <CertProblem label="Failed" reason={status.error} />;
  }

  if (status.valid) {
    return <Status tone="success">Valid</Status>;
  }

  return <span className="text-kumo-subtle">Missing</span>;
}

/**
 * The state as a word, with the reason one hover, tap or focus away. A row of this list holds one
 * line and truncates it, so a sentence put where "Valid" goes would be cut off and read as a state
 * of its own.
 */
function CertProblem({
  label,
  reason,
}: {
  readonly label: string;
  readonly reason: string;
}): ReactElement {
  return <StatusDetail tone="danger" label={label} detail={reason} />;
}

function facts(netInfo: NodeNetInfo): Fact[] {
  return [
    { key: "home", label: "Home relay", value: <HomeRelay netInfo={netInfo} /> },
    { key: "link", label: "Link", value: linkLabel(netInfo.linkType) },
    {
      key: "nat",
      label: "NAT",
      value: (
        <Status tone={natTone(netInfo.mappingVariesByDestIp)}>
          {natLabel(netInfo.mappingVariesByDestIp)}
        </Status>
      ),
    },
    { key: "udp", label: "UDP out", value: yesNo(netInfo.workingUdp) },
    { key: "ipv6", label: "IPv6", value: yesNo(netInfo.workingIpv6) },
    { key: "portmap", label: "Port mapping", value: portMapLabel(netInfo) },
  ];
}

/** The region the client picked, with its own measurement of it. */
function HomeRelay({ netInfo }: { readonly netInfo: NodeNetInfo }): ReactElement {
  const home = homeLatency(netInfo);

  if (home === undefined) {
    return (
      <span className="text-kumo-subtle">
        {netInfo.preferredDerpName === "" ? "Unknown" : netInfo.preferredDerpName}
      </span>
    );
  }

  return (
    <span className="flex min-w-0 items-baseline gap-2">
      <span className="font-mono text-[0.9em]">{home.code}</span>
      <span className="truncate">{home.name}</span>
      <span className="shrink-0 text-kumo-subtle tabular-nums">{msLabel(home.ms)}</span>
    </span>
  );
}

/** Every other region the client measured, nearest first, so a better home is easy to spot. */
function OtherRegions({ regions }: { readonly regions: readonly NodeDerpLatency[] }): ReactElement {
  return (
    <div className="border-t border-kumo-line px-4 py-3 text-sm">
      <p className="pb-1.5 text-xs text-kumo-subtle">Other regions</p>
      <ul className="flex flex-col gap-1">
        {regions.map((region) => (
          <li key={region.regionId} className="flex items-baseline justify-between gap-4">
            <span className="flex min-w-0 items-baseline gap-2">
              <span className="font-mono text-[0.9em]">{region.code}</span>
              <span className="truncate text-kumo-subtle">{region.name}</span>
            </span>
            <span className="shrink-0 text-kumo-subtle tabular-nums">{msLabel(region.ms)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
