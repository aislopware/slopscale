import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Switch } from "@cloudflare/kumo/components/switch";
import { GlobeIcon, PathIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import { useNodeMutations } from "~/components/machines/mutations.ts";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { advertisesExit, isExitRoute } from "~/lib/node.ts";

const routeIconSize = 14;

/** Every route the machine advertises, approved one at a time. */
export function RoutesSection({
  node,
  canEdit,
}: {
  readonly node: Node;
  readonly canEdit: boolean;
}): ReactElement {
  const { setRoutes } = useNodeMutations();
  const routes = [...new Set([...node.availableRoutes, ...node.approvedRoutes])].toSorted();

  function set(route: string, approved: boolean): void {
    const next = approved
      ? [...node.approvedRoutes, route]
      : node.approvedRoutes.filter((entry) => entry !== route);
    setRoutes.mutate({ params: { path: { nodeId: node.id } }, body: { routes: next } });
  }

  return (
    <Section title="Routes" description="Only approved routes reach the rest of the tailnet.">
      {routes.length === 0 ? (
        <SectionRow className="text-kumo-subtle">
          Nothing advertised. Run{" "}
          <span className="font-mono text-[0.9em]">tailscale set --advertise-routes=…</span> or{" "}
          <span className="font-mono text-[0.9em]">--advertise-exit-node</span> on the machine.
        </SectionRow>
      ) : (
        routes.map((route) => (
          <RouteRow
            key={route}
            node={node}
            route={route}
            disabled={!canEdit || setRoutes.isPending}
            onSet={set}
          />
        ))
      )}
    </Section>
  );
}

function RouteRow({
  node,
  route,
  disabled,
  onSet,
}: {
  readonly node: Node;
  readonly route: string;
  readonly disabled: boolean;
  readonly onSet: (route: string, approved: boolean) => void;
}): ReactElement {
  const approved = node.approvedRoutes.includes(route);
  const exit = isExitRoute(route);

  return (
    <SectionRow className="flex flex-wrap items-center justify-between gap-3 py-2.5">
      <div className="flex min-w-0 items-center gap-2">
        <span className="flex h-lh items-center text-kumo-subtle">
          {exit ? <GlobeIcon size={routeIconSize} /> : <PathIcon size={routeIconSize} />}
        </span>
        <span className="truncate font-mono text-[0.9em]">{route}</span>
        {node.availableRoutes.includes(route) ? null : (
          <Badge variant="warning">No longer advertised</Badge>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <Badge appearance="dot" variant={approved ? "success" : "warning"}>
          {approved ? "Approved" : "Pending"}
        </Badge>
        <Button
          variant={approved ? "ghost" : "secondary"}
          size="sm"
          disabled={disabled}
          onClick={() => {
            onSet(route, !approved);
          }}
        >
          {approved ? "Reject" : "Approve"}
        </Button>
      </div>
    </SectionRow>
  );
}

/** The tailnet-wide preference: one machine every client is told to prefer as its exit node. */
export function GlobalExitSection({
  node,
  canEdit,
}: {
  readonly node: Node;
  readonly canEdit: boolean;
}): ReactElement {
  const { setGlobalExitNode } = useNodeMutations();
  const eligible = advertisesExit(node);

  return (
    <Section
      title={
        <span className="flex items-center gap-1.5">
          <span className="flex h-lh items-center text-kumo-subtle">
            <GlobeIcon size={routeIconSize} />
          </span>
          Global exit node
        </span>
      }
      bodyClassName="px-5 py-4"
      actions={
        <Switch
          aria-label="Use as global exit node"
          checked={node.globalExitNode}
          disabled={!canEdit || !eligible || setGlobalExitNode.isPending}
          onCheckedChange={(enabled) => {
            setGlobalExitNode.mutate({ params: { path: { nodeId: node.id } }, body: { enabled } });
          }}
        />
      }
    >
      <p className="text-kumo-subtle">
        {eligible
          ? "Every client is told to prefer this machine when it picks an exit node automatically."
          : "The machine must advertise itself as an exit node before it can serve the whole tailnet."}
      </p>
    </Section>
  );
}
