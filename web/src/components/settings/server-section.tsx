import { cn } from "@cloudflare/kumo/utils";
import { ArrowRightIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";

import { api } from "~/api/client.ts";
import type { ServerInfo } from "~/api/queries.ts";
import { CopyText } from "~/components/ui/copy-text.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Section } from "~/components/ui/section.tsx";
import { Status } from "~/components/ui/status.tsx";
import { UrlText } from "~/components/ui/url-text.tsx";

const tlsLabels: Record<string, string> = {
  letsencrypt: "Let's Encrypt",
  files: "Certificate files",
  none: "None. A proxy in front terminates it.",
};

const databaseLabels: Record<string, string> = { sqlite: "SQLite", postgres: "PostgreSQL" };

/** The arrow that says a value leads somewhere, sized to the text beside it. */
const linkIconSize = 12;
const inlineLinkClass =
  "text-kumo-link underline decoration-kumo-line underline-offset-2 hover:decoration-current";

const policyLabels: Record<string, string> = {
  file: "Policy file on disk",
  database: "Stored in the database",
};

function Muted({ children }: { readonly children: string }): ReactElement {
  return <span className="text-kumo-subtle">{children}</span>;
}

/**
 * A value with a second, smaller line under it: what it is, then a qualifier such as where it is
 * overridden or what it listens on. Two facts side by side on one line read as one value cut in
 * half; stacked, each reads on its own.
 */
function Stacked({
  children,
  note,
}: {
  readonly children: ReactNode;
  readonly note: ReactNode;
}): ReactElement {
  return (
    <span className="flex flex-col items-end gap-0.5">
      <span>{children}</span>
      <span className="text-xs text-kumo-subtle">{note}</span>
    </span>
  );
}

function buildItems(info: ServerInfo, reachable: boolean): readonly Definition[] {
  return [
    { label: "Version", value: info.version, copy: info.version },
    { label: "Commit", value: info.commit, copy: info.commit },
    {
      label: "Running since",
      value: <RelativeTime value={info.startedAt} />,
    },
    { label: "Go", value: info.goVersion },
    // A page read out of the database says the database answers; only the failure is worth a word.
    {
      label: "Database",
      value: reachable ? (
        (databaseLabels[info.database] ?? info.database)
      ) : (
        <span className="inline-flex items-center gap-2">
          <span>{databaseLabels[info.database] ?? info.database}</span>
          <Status tone="danger">Unreachable</Status>
        </span>
      ),
    },
  ];
}

function networkItems(info: ServerInfo): readonly Definition[] {
  return [
    { label: "Server URL", value: <UrlText url={info.serverUrl} />, copy: info.serverUrl },
    { label: "Listening on", value: info.listenAddr, copy: info.listenAddr },
    { label: "IPv4 range", value: info.ipv4Prefix, copy: info.ipv4Prefix },
    { label: "IPv6 range", value: info.ipv6Prefix, copy: info.ipv6Prefix },
    ...(info.baseDomain === ""
      ? [{ label: "Base domain", value: <Muted>None</Muted> }]
      : [{ label: "Base domain", value: info.baseDomain, copy: info.baseDomain }]),
    { label: "MagicDNS", value: info.magicDns ? "On" : <Muted>Off</Muted> },
    { label: "TLS", value: tlsLabels[info.tls] ?? info.tls },
    {
      label: "Identity provider",
      value:
        info.oidcIssuer === "" ? (
          <Muted>None. Users are created by hand.</Muted>
        ) : (
          <CopyText
            value={info.oidcIssuer}
            display={<UrlText url={info.oidcIssuer} />}
            label="Copy the issuer URL"
          />
        ),
    },
  ];
}

function policyItems(info: ServerInfo): readonly Definition[] {
  return [
    {
      label: "Policy",
      value: (
        <span className="flex items-center gap-2">
          {policyLabels[info.policyMode] ?? info.policyMode}
          {info.policyPath === "" ? null : (
            <span className="truncate font-mono text-[0.9em] text-kumo-subtle">
              {info.policyPath}
            </span>
          )}
        </span>
      ),
    },
    {
      label: "Config file key expiry",
      wrap: true,
      value: (
        <Stacked
          note={
            <>
              {"Overridden by "}
              <Link to="/settings/tailnet" className={inlineLinkClass}>
                Settings › Tailnet
              </Link>
            </>
          }
        >
          {info.nodeExpiry === "" ? (
            <Muted>Never, unless the client asks for an expiry</Muted>
          ) : (
            info.nodeExpiry
          )}
        </Stacked>
      ),
    },
    { label: "Ephemeral timeout", value: info.ephemeralInactivityTimeout },
    { label: "Latest client", value: <LatestClient info={info} /> },
    {
      label: "Control dial plan",
      value:
        info.controlDialPlan.length > 0 ? (
          info.controlDialPlan.join(", ")
        ) : (
          <span className="text-kumo-subtle">Clients resolve the server URL</span>
        ),
    },
    { label: "Funnel", value: <FunnelValue info={info} />, wrap: true },
  ];
}

/**
 * Funnel: how many ingress nodes serve it, with the embedded ingress's state and the allowed ports
 * under that. The count comes first because Funnel delivers nothing without an ingress node, and an
 * external one counts as much as the embedded one; only a server with neither gets a word.
 */
function FunnelValue({ info }: { readonly info: ServerInfo }): ReactElement {
  if (!info.funnelIngress && info.funnelIngressNodes === 0) {
    return <Muted>Embedded ingress off</Muted>;
  }

  const listening =
    info.funnelIngress && info.funnelListenAddrs.length > 0
      ? `Embedded ingress on ${info.funnelListenAddrs.join(", ")}`
      : `Embedded ingress ${info.funnelIngress ? "on" : "off"}`;
  const ports = info.funnelPorts.length > 0 ? ` · Ports ${info.funnelPorts.join(", ")}` : "";

  return (
    <Stacked note={`${listening}${ports}`}>
      {info.funnelIngressNodes === 0 ? (
        <Status tone="warning">No ingress node has joined</Status>
      ) : (
        `${info.funnelIngressNodes} ingress ${info.funnelIngressNodes === 1 ? "node" : "nodes"}`
      )}
    </Stacked>
  );
}

/** The latest stable Tailscale client the server found, or why it has none. */
function LatestClient({ info }: { readonly info: ServerInfo }): ReactElement {
  if (!info.clientUpdatesCheck) {
    return <span className="text-kumo-subtle">Not checked (client_updates.check is off)</span>;
  }

  if (info.latestClientVersion === "") {
    return <span className="text-kumo-subtle">Not looked up yet</span>;
  }

  return <span>{info.latestClientVersion}</span>;
}

/** The build, addresses and config file values of the running server. */
export function ServerSection({ info }: { readonly info: ServerInfo }): ReactElement {
  const health = api.useQuery("get", "/api/v1/health");
  const reachable = health.data?.databaseConnectivity === true;
  const items: readonly Definition[] = [
    ...buildItems(info, reachable),
    ...networkItems(info),
    ...policyItems(info),
    {
      label: "DERP relays",
      wrap: true,
      value: (
        <Stacked note={info.derpServer ? "Embedded relay running" : "Embedded relay off"}>
          <Link to="/relays/map" className={cn(inlineLinkClass, "flex items-center gap-1")}>
            {info.derpRegions === 0
              ? "No regions"
              : `${info.derpRegions} ${info.derpRegions === 1 ? "region" : "regions"}`}
            <ArrowRightIcon size={linkIconSize} aria-hidden />
          </Link>
        </Stacked>
      ),
    },
  ];

  return (
    <Section
      title="Server"
      description="Values from the config file change only with a restart; the rest is read live."
      bodyClassName="p-0"
    >
      <DefinitionList items={items} />
    </Section>
  );
}
