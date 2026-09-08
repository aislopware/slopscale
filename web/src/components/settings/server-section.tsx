import { Badge } from "@cloudflare/kumo/components/badge";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import type { ServerInfo } from "~/api/queries.ts";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Section } from "~/components/ui/section.tsx";

const tlsLabels: Record<string, string> = {
  letsencrypt: "Let's Encrypt",
  files: "Certificate files",
  none: "None, a proxy in front terminates it",
};

const databaseLabels: Record<string, string> = { sqlite: "SQLite", postgres: "PostgreSQL" };

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
          <Badge appearance="dot" variant={reachable ? "success" : "error"}>
            {reachable ? "Reachable" : "Unreachable"}
          </Badge>
        </span>
      ),
    },
  ];
}

function networkItems(info: ServerInfo): readonly Definition[] {
  return [
    { label: "Server URL", value: info.serverUrl, copy: info.serverUrl },
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
            <Badge variant={info.magicDns ? "success" : "neutral"}>
              {info.magicDns ? "On" : "Off"}
            </Badge>
          </span>
        ),
    },
    { label: "TLS", value: tlsLabels[info.tls] ?? info.tls },
    {
      label: "Identity provider",
      value:
        info.oidcIssuer === "" ? (
          <Muted>None, users are created by hand</Muted>
        ) : (
          <span className="font-mono text-[0.9em]">{info.oidcIssuer}</span>
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
      label: "Default key expiry",
      value:
        info.nodeExpiry === "" ? <Muted>Never, unless the client asks</Muted> : info.nodeExpiry,
    },
    { label: "Ephemeral timeout", value: info.ephemeralInactivityTimeout },
  ];
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
          <Link to="/relays" className="underline-offset-2 hover:underline">
            {info.derpRegions === 0
              ? "No regions"
              : `${info.derpRegions} ${info.derpRegions === 1 ? "region" : "regions"}`}
          </Link>
          <Badge variant={info.derpServer ? "info" : "neutral"}>
            {info.derpServer ? "Embedded relay running" : "Embedded relay off"}
          </Badge>
        </span>
      ),
    },
  ];

  return (
    <Section
      title="Server"
      description="What this console is talking to. These come from the config file and change with a restart."
      bodyClassName="p-0"
    >
      <DefinitionList items={items} />
    </Section>
  );
}
