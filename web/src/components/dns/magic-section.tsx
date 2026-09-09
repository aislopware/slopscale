import type { ReactElement } from "react";

import type { Dns } from "~/api/queries.ts";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import { Section } from "~/components/ui/section.tsx";
import { Status } from "~/components/ui/status.tsx";

export function MagicDnsSection({ dns }: { readonly dns: Dns }): ReactElement {
  return (
    <Section
      title="MagicDNS"
      description="Set in the config file. Machines are named after the base domain, so it cannot change while they are registered."
      bodyClassName="p-0"
    >
      <DefinitionList
        items={[
          {
            label: "MagicDNS",
            value: (
              <Status tone={dns.magicDns ? "success" : "neutral"}>
                {dns.magicDns ? "On" : "Off"}
              </Status>
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
