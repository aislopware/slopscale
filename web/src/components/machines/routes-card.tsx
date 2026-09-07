import { Badge } from "@cloudflare/kumo/components/badge";
import { Switch } from "@cloudflare/kumo/components/switch";
import { GlobeIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import { useNodeMutations } from "~/components/machines/mutations.ts";
import { Card, CardBody, CardDescription, CardHeader, CardTitle } from "~/components/ui/card.tsx";
import { advertisesExit, isExitRoute } from "~/lib/node.ts";

/** Every route the machine advertises, with its approval switch. */
export function RoutesCard({
  node,
  canEdit,
}: {
  readonly node: Node;
  readonly canEdit: boolean;
}): ReactElement {
  const { setRoutes } = useNodeMutations();
  const routes = [...new Set([...node.availableRoutes, ...node.approvedRoutes])].toSorted();

  function toggle(route: string, approved: boolean): void {
    const next = approved
      ? [...node.approvedRoutes, route]
      : node.approvedRoutes.filter((entry) => entry !== route);
    setRoutes.mutate({ params: { path: { nodeId: node.id } }, body: { routes: next } });
  }

  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle>Routes</CardTitle>
          <CardDescription>
            Subnets and exit routes this machine advertises. Only approved routes reach the tailnet.
          </CardDescription>
        </div>
      </CardHeader>
      <CardBody className="flex flex-col divide-y divide-kumo-line p-0">
        {routes.length === 0 ? (
          <p className="px-5 py-4 text-kumo-subtle">
            No routes advertised. Run{" "}
            <span className="font-mono text-[0.9em]">tailscale set --advertise-routes=…</span> or{" "}
            <span className="font-mono text-[0.9em]">--advertise-exit-node</span> on the machine.
          </p>
        ) : (
          routes.map((route) => (
            <RouteRow
              key={route}
              node={node}
              route={route}
              disabled={!canEdit || setRoutes.isPending}
              onToggle={toggle}
            />
          ))
        )}
      </CardBody>
    </Card>
  );
}

function RouteRow({
  node,
  route,
  disabled,
  onToggle,
}: {
  readonly node: Node;
  readonly route: string;
  readonly disabled: boolean;
  readonly onToggle: (route: string, approved: boolean) => void;
}): ReactElement {
  const approved = node.approvedRoutes.includes(route);
  const advertised = node.availableRoutes.includes(route);

  return (
    <div className="flex items-center justify-between gap-4 px-5 py-3">
      <Switch
        label={<span className="font-mono text-[0.9em]">{route}</span>}
        checked={approved}
        disabled={disabled}
        onCheckedChange={(checked) => {
          onToggle(route, checked);
        }}
      />
      <div className="flex flex-wrap items-center justify-end gap-1.5">
        {isExitRoute(route) ? <Badge variant="outline">Exit</Badge> : null}
        {advertised ? null : <Badge variant="warning">No longer advertised</Badge>}
        <span className="text-sm text-kumo-subtle">{approved ? "Approved" : "Pending"}</span>
      </div>
    </div>
  );
}

/** The tailnet-wide preference: one machine every client is told to prefer as its exit node. */
export function GlobalExitCard({
  node,
  canEdit,
}: {
  readonly node: Node;
  readonly canEdit: boolean;
}): ReactElement {
  const { setGlobalExitNode } = useNodeMutations();
  const eligible = advertisesExit(node);

  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle className="flex items-center gap-1.5">
            <span className="flex h-lh items-center">
              <GlobeIcon className="text-kumo-subtle" />
            </span>
            Global exit node
          </CardTitle>
          <CardDescription>
            Every client is told to prefer this machine when it picks an exit node automatically.
          </CardDescription>
        </div>
        <Switch
          aria-label="Use as global exit node"
          checked={node.globalExitNode}
          disabled={!canEdit || !eligible || setGlobalExitNode.isPending}
          onCheckedChange={(enabled) => {
            setGlobalExitNode.mutate({ params: { path: { nodeId: node.id } }, body: { enabled } });
          }}
        />
      </CardHeader>
      {eligible ? null : (
        <CardBody className="text-kumo-subtle">
          The machine must advertise itself as an exit node first.
        </CardBody>
      )}
    </Card>
  );
}
