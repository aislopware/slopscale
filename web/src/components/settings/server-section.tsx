import { Badge } from "@cloudflare/kumo/components/badge";
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
import { UrlText } from "~/components/ui/url-text.tsx";

const tlsLabels: Record<string, string> = {
  letsencrypt: "Let's Encrypt",
  files: "Certificate files",
  none: "None, a proxy in front terminates it",
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
          <Link
            to="/relays/map"
            className="flex items-center gap-1 text-kumo-link underline decoration-kumo-line underline-offset-2 hover:decoration-current"
          >
            {info.derpRegions === 0
              ? "No regions"
              : `${info.derpRegions} ${info.derpRegions === 1 ? "region" : "regions"}`}
            <ArrowRightIcon size={linkIconSize} aria-hidden />
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
      description="The server this console talks to. These values come from the config file and change only with a restart."
      bodyClassName="p-0"
    >
      <DefinitionList items={items} />
    </Section>
  );
}
