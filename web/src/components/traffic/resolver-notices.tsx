import type { ReactElement } from "react";

import type { TrafficReporters } from "~/api/traffic.ts";
import { TextLink } from "~/components/traffic/window-header.tsx";
import { Callout } from "~/components/ui/callout.tsx";

/**
 * Why the gateway resolvers reach fewer clients than the settings suggest: DNS logging is on but
 * has no global nameserver to keep the clients on, or some global nameservers are ones the
 * resolvers cannot forward to. Both are fixed on the DNS nameservers page.
 */
export function ResolverNotices({
  reporters,
}: {
  readonly reporters: Pick<TrafficReporters, "dnsBlocked" | "skippedUpstreams">;
}): ReactElement | null {
  const { dnsBlocked, skippedUpstreams } = reporters;

  if (dnsBlocked === "" && skippedUpstreams.length === 0) {
    return null;
  }

  return (
    <div className="flex flex-col gap-3">
      {dnsBlocked === "" ? null : (
        <Callout
          tone="warning"
          title="DNS logging is on, but no machine uses a gateway resolver"
          description={
            <>
              {`${dnsBlocked.charAt(0).toUpperCase()}${dnsBlocked.slice(1)}. `}
              <TextLink to="/dns/nameservers">Global nameservers</TextLink>
            </>
          }
        />
      )}
      {skippedUpstreams.length === 0 ? null : (
        <Callout
          title="The gateway resolvers skip some global nameservers"
          description={
            <>
              {`They forward only to IP addresses and https:// resolvers outside the tailnet, so they do not use ${skippedUpstreams.join(", ")}. `}
              <TextLink to="/dns/nameservers">Global nameservers</TextLink>
            </>
          }
        />
      )}
    </div>
  );
}
