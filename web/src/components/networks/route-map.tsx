import { cn } from "@cloudflare/kumo/utils";
import { DesktopIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import type { RouteStatus } from "~/components/networks/model.ts";
import type { RouteGroup } from "~/components/networks/routes-model.ts";
import { rowLinkClass } from "~/components/table/styles.ts";
import { nodeName } from "~/lib/node.ts";

/** A prefix pill's height and the gap between two, which the connector curves are drawn from. */
const pillHeight = 28;
const pillGap = 6;
const connectorWidth = 40;
/** Prefixes a router shows before it counts the rest, and routers the map draws at most. */
const maxPrefixes = 5;
const maxRouters = 12;
const iconSize = 14;

interface Prefix {
  readonly label: string;
  readonly status: RouteStatus;
}

interface Router {
  readonly node: Node;
  readonly prefixes: readonly Prefix[];
}

const dots: Record<RouteStatus, string> = {
  approved: "bg-kumo-success",
  pending: "bg-kumo-warning",
  stale: "bg-kumo-interact",
};

const statusWords: Record<RouteStatus, string> = {
  approved: "approved",
  pending: "waiting for approval",
  stale: "no longer advertised",
};

/** The routes regrouped by the machine that carries them, the shape the map draws. */
export function routersOf(groups: readonly RouteGroup[]): Router[] {
  const byNode = new Map<string, { node: Node; prefixes: Prefix[] }>();

  for (const group of groups) {
    for (const advertiser of group.advertisers) {
      const entry = byNode.get(advertiser.node.id) ?? { node: advertiser.node, prefixes: [] };

      entry.prefixes.push({
        label: advertiser.exit ? "Exit node" : advertiser.route,
        status: advertiser.status,
      });
      byNode.set(advertiser.node.id, entry);
    }
  }

  return [...byNode.values()].toSorted((left, right) =>
    nodeName(left.node).localeCompare(nodeName(right.node)),
  );
}

/**
 * The route map over the dashboard's Routes table: on a dotted canvas, each machine's prefixes as
 * mono pills joined by curves to a pill for the machine that carries them. It draws what the table
 * under it lists, filters included, so a router serving three subnets reads as one cluster before
 * the table spells out each row. A dot on each prefix says whether it is approved; the dot's title
 * names the state for a pointer that rests on it.
 */
export function RouteMap({
  groups,
}: {
  readonly groups: readonly RouteGroup[];
}): ReactElement | null {
  const routers = routersOf(groups);

  if (routers.length === 0) {
    return null;
  }

  const shown = routers.slice(0, maxRouters);
  const hidden = routers.length - shown.length;

  return (
    // A picture of the table under it: the table carries the same routes and links for the
    // keyboard and the screen reader, so the map is hidden from both rather than said twice.
    <figure
      aria-hidden
      className="flex flex-wrap items-center justify-center gap-x-12 gap-y-6 rounded-lg bg-kumo-base bg-[radial-gradient(var(--color-kumo-line)_1px,transparent_1px)] [background-size:12px_12px] px-6 py-6 ring ring-kumo-line"
    >
      {shown.map((router) => (
        <Cluster key={router.node.id} router={router} />
      ))}
      {hidden === 0 ? null : (
        <figcaption className="text-sm text-kumo-subtle">
          {`and ${hidden} more ${hidden === 1 ? "machine" : "machines"} in the table`}
        </figcaption>
      )}
    </figure>
  );
}

function Cluster({ router }: { readonly router: Router }): ReactElement {
  const rest = router.prefixes.length - maxPrefixes;
  const pills: Prefix[] = router.prefixes.slice(0, maxPrefixes);
  const labels =
    rest > 0 ? [...pills, { label: `+${rest} more`, status: "approved" as const }] : pills;
  const height = labels.length * pillHeight + (labels.length - 1) * pillGap;
  const middle = height / 2;

  return (
    <div className="flex items-center">
      <ul className="flex flex-col items-end" style={{ gap: pillGap }}>
        {labels.map((prefix, index) => {
          const more = rest > 0 && index === labels.length - 1;

          return (
            <li
              key={prefix.label}
              style={{ height: pillHeight }}
              className="flex items-center gap-1.5 rounded-full bg-kumo-base px-2.5 text-xs shadow-sm ring ring-kumo-hairline"
            >
              {more ? (
                <span className="text-kumo-subtle">{prefix.label}</span>
              ) : (
                <>
                  <span
                    title={statusWords[prefix.status]}
                    className={cn("size-1.5 rounded-full", dots[prefix.status])}
                  />
                  <span
                    className={cn(
                      prefix.label === "Exit node" ? "font-medium" : "font-mono",
                      "text-kumo-default",
                    )}
                  >
                    {prefix.label}
                  </span>
                </>
              )}
            </li>
          );
        })}
      </ul>
      <svg aria-hidden width={connectorWidth} height={height} className="shrink-0" fill="none">
        {labels.map((prefix, index) => {
          const top = pillHeight / 2 + index * (pillHeight + pillGap);
          const bend = connectorWidth / 2;

          return (
            <path
              key={prefix.label}
              d={`M0 ${top} C${bend} ${top} ${bend} ${middle} ${connectorWidth} ${middle}`}
              stroke="var(--color-kumo-line)"
            />
          );
        })}
      </svg>
      <span className="flex h-8 items-center gap-1.5 rounded-full bg-kumo-base px-3 text-sm shadow-sm ring ring-kumo-hairline">
        <DesktopIcon size={iconSize} aria-hidden className="text-kumo-subtle" />
        <Link
          to="/machines/$nodeId"
          params={{ nodeId: router.node.id }}
          tabIndex={-1}
          className={cn("whitespace-nowrap", rowLinkClass)}
        >
          {nodeName(router.node)}
        </Link>
      </span>
    </div>
  );
}
