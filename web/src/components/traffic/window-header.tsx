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

/**
 * "Sep 24, 2:05 PM – Sep 25, 2:05 PM" for the window the server answered for. The server widens a
 * window to whole buckets, which can end past now; the label stops at now.
 */
export function windowLabel(start: string, end: string, now: Date = new Date()): string {
  const from = parseTime(start);
  const to = parseTime(end);

  if (from === null || to === null) {
    return "";
  }

  return `${formatAbsolute(from)} – ${formatAbsolute(to > now ? now : to)}`;
}

type Gateway = Pick<TrafficReporter, "nodeId" | "nodeName">;

/**
 * The gateways that reported within the window: a gateway first seen after it ended, or silent
 * since before it began, carried nothing in it.
 */
export function gatewaysInWindow(
  reporters: readonly TrafficReporter[],
  window: { readonly start: string; readonly end: string } | undefined,
): TrafficReporter[] {
  const start = window === undefined ? null : parseTime(window.start);
  const end = window === undefined ? null : parseTime(window.end);

  return reporters.filter((reporter) => {
    const first = parseTime(reporter.firstSeenAt);
    const last = parseTime(reporter.lastReportAt);

    return (
      (end === null || first === null || first < end) &&
      (start === null || last === null || last >= start)
    );
  });
}

/** Which gateways the page looks through, in words. */
export function gatewayLabel(gateways: readonly Gateway[], gateway: string): string {
  if (gateway !== "") {
    const reporter = gateways.find((candidate) => candidate.nodeId === gateway);

    return `Through ${reporter === undefined ? `gateway ${gateway}` : trafficNodeName(reporter)}`;
  }

  return gateways.length === 1
    ? `Through ${trafficNodeName(gateways[0] ?? { nodeId: "", nodeName: "" })}`
    : `Through ${plural(gateways.length, "gateway")}`;
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
  carried,
  gateway,
  actions,
}: {
  readonly title: ReactNode;
  readonly description?: ReactNode;
  /** The bounds the server answered for; absent until the first answer. */
  readonly window: { readonly start: string; readonly end: string } | undefined;
  readonly reporters: readonly TrafficReporter[];
  /** The gateways that carried the page's traffic, when the page read them; else worked out. */
  readonly carried?: readonly Gateway[] | undefined;
  readonly gateway: string;
  readonly actions?: ReactNode;
}): ReactElement {
  const label = window === undefined ? "" : windowLabel(window.start, window.end);
  const gateways = carried ?? gatewaysInWindow(reporters, window);

  return (
    <PageHeader
      title={title}
      {...(description === undefined ? {} : { description })}
      {...(actions === undefined ? {} : { actions })}
      meta={
        <>
          {label === "" ? null : <span>{label}</span>}
          {gateways.length === 0 && gateway === "" ? null : (
            <>
              {label === "" ? null : <span aria-hidden>·</span>}
              <span>{gatewayLabel(gateways.length === 0 ? reporters : gateways, gateway)}</span>
            </>
          )}
        </>
      }
    />
  );
}
