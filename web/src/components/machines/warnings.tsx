import { Banner } from "@cloudflare/kumo/components/banner";
import { WarningIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";

interface Warning {
  readonly title: string;
  readonly description: string;
}

/** What each warn-* flag the client sends means, in words an operator can act on. */
const warnings: Record<string, Warning> = {
  "ip-forwarding-off": {
    title: "IP forwarding is off",
    description:
      "The machine advertises routes but its kernel drops forwarded packets, so nothing behind it is reachable. On Linux, set net.ipv4.ip_forward and net.ipv6.conf.all.forwarding to 1 with sysctl and make it persistent.",
  },
  "router-unhealthy": {
    title: "Route setup is failing",
    description:
      "The client cannot program its routes on this machine. The tailscaled log on the machine names what failed.",
  },
  "etc-apt-source-disabled": {
    title: "Tailscale's apt source is disabled",
    description:
      "The package repository is commented out in /etc/apt/sources.list.d, so updates from Tailscale do not reach this machine.",
  },
};

function describe(flag: string): Warning {
  return (
    warnings[flag] ?? {
      title: `The client reports ${flag}`,
      description: "The tailscaled log on the machine has the details.",
    }
  );
}

/** Problems the client reports about itself, shown above the machine's facts. */
export function ClientWarnings({ node }: { readonly node: Node }): ReactElement | null {
  if (node.clientWarnings.length === 0) {
    return null;
  }

  return (
    <div className="flex flex-col gap-3">
      {node.clientWarnings.map((flag) => {
        const warning = describe(flag);

        return (
          <Banner
            key={flag}
            size="sm"
            variant="alert"
            icon={<WarningIcon />}
            title={warning.title}
            description={warning.description}
          />
        );
      })}
    </div>
  );
}
