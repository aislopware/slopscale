import type { ReactElement } from "react";

import type { Dns } from "~/api/queries.ts";
import { FromFileBadge } from "~/components/dns/source.tsx";
import { CopyText } from "~/components/ui/copy-text.tsx";
import { SettingsCard, SettingsRow } from "~/components/ui/settings-card.tsx";
import { Status } from "~/components/ui/status.tsx";

/**
 * MagicDNS and the base domain as the dashboard's settings card: a row per setting with its
 * description on the left and its state on the right. Both come from the config file, so the right
 * side is the value rather than a switch.
 */
export function MagicDnsSection({ dns }: { readonly dns: Dns }): ReactElement {
  return (
    <SettingsCard>
      <SettingsRow
        title={
          <span className="inline-flex items-center gap-2">
            MagicDNS
            <FromFileBadge />
          </span>
        }
        description="Gives every machine a name under the base domain, so machines reach each other by name."
        control={
          <Status tone={dns.magicDns ? "success" : "neutral"}>{dns.magicDns ? "On" : "Off"}</Status>
        }
      />
      <SettingsRow
        title={
          <span className="inline-flex items-center gap-2">
            Base domain
            <FromFileBadge />
          </span>
        }
        description="Machines are named after the base domain, so it cannot change while they are registered."
        control={
          dns.baseDomain === "" ? (
            <span className="text-kumo-subtle">Not set</span>
          ) : (
            <CopyText value={dns.baseDomain} />
          )
        }
      />
    </SettingsCard>
  );
}
