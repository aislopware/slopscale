import { LinkButton } from "@cloudflare/kumo/components/button";
import { GlobeIcon, PathIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import { machineRoutes, RouteName, routeState } from "~/components/machines/routes.tsx";
import { Badge } from "~/components/ui/badge.tsx";
import { Section } from "~/components/ui/section.tsx";
import { isExitRoute } from "~/lib/node.ts";

/** How many routes the Overview tab lists before "View all" takes over. */
const previewRows = 5;
const iconSize = 14;

/**
 * The Overview tab's look at the routes, as a Zero Trust profile previews its split tunnels: a
 * small table of the first few, 32px rows, and "View all (N)" to the Routes tab, where each one is
 * approved or revoked. Nothing here changes a route.
 */
export function RoutesPreview({ node }: { readonly node: Node }): ReactElement {
  const routes = machineRoutes(node);
  const shown = routes.slice(0, previewRows);

  return (
    <Section
      title="Routes"
      bodyClassName="p-0"
      actions={
        <LinkButton href={`/machines/${node.id}/routes`} variant="ghost" size="xs">
          {routes.length === 0 ? "Manage" : `View all (${String(routes.length)})`}
        </LinkButton>
      }
    >
      <table className="w-full text-sm">
        <thead className="text-left text-kumo-subtle">
          <tr className="border-b border-kumo-line">
            <th scope="col" className="px-4 py-2 font-medium">
              Route
            </th>
            <th scope="col" className="w-px px-4 py-2 text-right font-medium whitespace-nowrap">
              State
            </th>
          </tr>
        </thead>
        <tbody>
          {shown.length === 0 ? (
            <tr>
              <td colSpan={2} className="px-4 py-12 text-center text-kumo-subtle">
                This machine advertises no routes.
              </td>
            </tr>
          ) : (
            shown.map((route) => <PreviewRow key={route} node={node} route={route} />)
          )}
        </tbody>
      </table>
    </Section>
  );
}

function PreviewRow({
  node,
  route,
}: {
  readonly node: Node;
  readonly route: string;
}): ReactElement {
  const exit = isExitRoute(route);
  const { approved, available } = routeState(node, route);

  return (
    <tr className="h-8 border-b border-kumo-line last-of-type:border-b-0">
      <td className="max-w-0 px-4 py-1.5">
        <span className="flex min-w-0 items-center gap-2">
          <span className="flex shrink-0 text-kumo-subtle">
            {exit ? <GlobeIcon size={iconSize} /> : <PathIcon size={iconSize} />}
          </span>
          <RouteName route={route} exit={exit} available={available} />
        </span>
      </td>
      <td className="w-px px-4 py-1.5 text-right whitespace-nowrap">
        <Badge tone={approved ? "success" : "warning"}>{approved ? "Approved" : "Pending"}</Badge>
      </td>
    </tr>
  );
}
