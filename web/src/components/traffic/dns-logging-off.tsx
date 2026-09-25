import type { ReactElement } from "react";

import { TextLink } from "~/components/traffic/window-header.tsx";
import { Callout } from "~/components/ui/callout.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";

/** The empty lookups list while DNS logging is off, with the way to turn it on. */
export function DnsLoggingOff(): ReactElement {
  return (
    <SectionEmpty
      title="DNS logging is off"
      description="Turn on DNS logging and approve a gateway resolver to see what the machines using that gateway as their exit node look up."
      contents={<TextLink to="/traffic/settings">Traffic settings</TextLink>}
    />
  );
}

/** Over lookups kept from before DNS logging went off: nothing newer is coming. */
export function DnsLoggingOffNote(): ReactElement {
  return (
    <Callout
      title="DNS logging is off"
      description={
        <>
          {"These lookups are from before it was turned off; new ones are not recorded. "}
          <TextLink to="/traffic/settings">Traffic settings</TextLink>
        </>
      }
    />
  );
}
