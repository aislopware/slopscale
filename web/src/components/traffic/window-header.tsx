import { Link } from "@tanstack/react-router";
import type { LinkProps } from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";

import type { TrafficReporter } from "~/api/traffic.ts";
import { plural } from "~/components/overview/plural.ts";
import { trafficNodeName } from "~/components/traffic/machines-table.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { formatAbsolute, parseTime } from "~/lib/time.ts";

/** A link inside a sentence: the link colour and an underline that firms up on hover. */
export const textLinkClass =
  "text-kumo-link underline decoration-kumo-line underline-offset-2 hover:decoration-current";

/** A router link that reads as a link in running text or a table footer. */
export function TextLink({
  children,
  ...props
}: LinkProps & { readonly children: ReactNode }): ReactElement {
  return (
    <Link {...props} className={textLinkClass}>
      {children}
    </Link>
  );
}

/** "Sep 24, 2:05 PM – Sep 25, 2:05 PM" for the window the server answered for. */
export function windowLabel(start: string, end: string): string {
  const from = parseTime(start);
  const to = parseTime(end);

  return from === null || to === null ? "" : `${formatAbsolute(from)} – ${formatAbsolute(to)}`;
}

/** Which gateways the page looks through, in words. */
export function gatewayLabel(reporters: readonly TrafficReporter[], gateway: string): string {
  if (gateway !== "") {
    const reporter = reporters.find((candidate) => candidate.nodeId === gateway);

    return `Through ${reporter === undefined ? `gateway ${gateway}` : trafficNodeName(reporter)}`;
  }

  return reporters.length === 1
    ? `Through ${trafficNodeName(reporters[0] ?? { nodeId: "", nodeName: "" })}`
    : `Through ${plural(reporters.length, "gateway")}`;
}

/**
 * The title of a traffic page, with the window the server actually read (it widens a window to
 * whole buckets) and the gateways it looked through on the meta line.
 */
export function WindowHeader({
  title,
  description,
  window,
  reporters,
  gateway,
  actions,
}: {
  readonly title: ReactNode;
  readonly description?: ReactNode;
  /** The bounds the server answered for; absent until the first answer. */
  readonly window: { readonly start: string; readonly end: string } | undefined;
  readonly reporters: readonly TrafficReporter[];
  readonly gateway: string;
  readonly actions?: ReactNode;
}): ReactElement {
  const label = window === undefined ? "" : windowLabel(window.start, window.end);

  return (
    <PageHeader
      title={title}
      {...(description === undefined ? {} : { description })}
      {...(actions === undefined ? {} : { actions })}
      meta={
        <>
          {label === "" ? null : <span>{label}</span>}
          {reporters.length === 0 && gateway === "" ? null : (
            <>
              {label === "" ? null : <span aria-hidden>·</span>}
              <span>{gatewayLabel(reporters, gateway)}</span>
            </>
          )}
        </>
      }
    />
  );
}
