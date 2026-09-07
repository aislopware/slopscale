import { Badge } from "@cloudflare/kumo/components/badge";
import type { ReactElement } from "react";

import type { Dns } from "~/api/queries.ts";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import { Section } from "~/components/ui/section.tsx";

export function MagicDnsSection({ dns }: { readonly dns: Dns }): ReactElement {
  return (
    <Section
      title="MagicDNS"
      description="Set in the config file: machines are named after the base domain, so it cannot change while they are registered."
      bodyClassName="p-0"
    >
      <DefinitionList
        items={[
          {
            label: "MagicDNS",
            value: (
              <Badge appearance="dot" variant={dns.magicDns ? "success" : "neutral"}>
                {dns.magicDns ? "On" : "Off"}
              </Badge>
            ),
          },
          {
            label: "Base domain",
            value:
              dns.baseDomain === "" ? (
                <span className="text-kumo-subtle">Not set</span>
              ) : (
                dns.baseDomain
              ),
            ...(dns.baseDomain === "" ? {} : { copy: dns.baseDomain }),
          },
        ]}
      />
    </Section>
  );
}
