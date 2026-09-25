import type { ReactElement } from "react";

import type { TrafficReporters } from "~/api/traffic.ts";
import { TextLink } from "~/components/traffic/window-header.tsx";
import { Callout } from "~/components/ui/callout.tsx";

/**
 * Which of the nameservers kept for exit node users the gateway resolvers cannot forward to. The
 * machines still ask those themselves; the fix is on the DNS nameservers page.
 */
export function ResolverNotices({
  reporters,
}: {
  readonly reporters: Pick<TrafficReporters, "skippedUpstreams">;
}): ReactElement | null {
  const { skippedUpstreams } = reporters;

  if (skippedUpstreams.length === 0) {
    return null;
  }

  return (
    <Callout
      title="The gateway resolvers skip some exit node nameservers"
      description={
        <>
          {`They forward only to IP addresses and https:// resolvers outside the tailnet, so they do not use ${skippedUpstreams.join(", ")}; the machines still ask it themselves. `}
          <TextLink to="/dns/nameservers">Global nameservers</TextLink>
        </>
      }
    />
  );
}
