import { ArrowRightIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

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

const policyLabels: Record<string, string> = {
  file: "Policy file on disk",
  database: "Stored in the database",
};

function Muted({ children }: { readonly children: string }): ReactElement {
  return <span className="text-kumo-subtle">{children}</span>;
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
    {
      label: "Database",
      value: (
        <span className="flex items-center gap-2">
          <span>{databaseLabels[info.database] ?? info.database}</span>
          <Status tone={reachable ? "success" : "danger"}>
            {reachable ? "Reachable" : "Unreachable"}
          </Status>
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
    {
      label: "MagicDNS",
      value:
        info.baseDomain === "" ? (
          <Muted>Off</Muted>
        ) : (
          <span className="flex items-center gap-2">
            <span className="font-mono text-[0.9em]">{info.baseDomain}</span>
            <Status tone={info.magicDns ? "success" : "neutral"}>
              {info.magicDns ? "On" : "Off"}
            </Status>
          </span>
        ),
    },
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
      value: (
        <span className="flex items-center gap-2">
          {info.nodeExpiry === "" ? (
            <Muted>Never, unless the client asks for an expiry</Muted>
          ) : (
            info.nodeExpiry
          )}
          <span className="text-kumo-subtle">Overridden by Settings › Tailnet</span>
        </span>
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
    {
      label: "Funnel",
      value: (
        <span className="flex flex-wrap items-center gap-2">
          <span>
            {info.funnelIngressNodes === 0
              ? "No ingress node has joined"
              : `${info.funnelIngressNodes} ingress ${info.funnelIngressNodes === 1 ? "node" : "nodes"}`}
          </span>
          <Status tone={info.funnelIngress ? "success" : "neutral"}>
            {info.funnelIngress ? "Embedded ingress running" : "Embedded ingress off"}
          </Status>
          <span className="text-kumo-subtle">
            {info.funnelIngress && info.funnelListenAddrs.length > 0
              ? `Listening on ${info.funnelListenAddrs.join(", ")}. `
              : ""}
            Ports {info.funnelPorts.join(", ")}
          </span>
        </span>
      ),
    },
  ];
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
      value: (
        <span className="flex items-center gap-2">
          <Link
            to="/relays/map"
            className="flex items-center gap-1 text-kumo-link underline decoration-kumo-line underline-offset-2 hover:decoration-current"
          >
            {info.derpRegions === 0
              ? "No regions"
              : `${info.derpRegions} ${info.derpRegions === 1 ? "region" : "regions"}`}
            <ArrowRightIcon size={linkIconSize} aria-hidden />
          </Link>
          <Status tone={info.derpServer ? "success" : "neutral"}>
            {info.derpServer ? "Embedded relay running" : "Embedded relay off"}
          </Status>
        </span>
      ),
    },
  ];

  return (
    <Section
      title="Server"
      description="These values come from the config file and change only with a restart."
      bodyClassName="p-0"
    >
      <DefinitionList items={items} />
    </Section>
  );
}
