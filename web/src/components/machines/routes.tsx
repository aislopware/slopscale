import { Button } from "@cloudflare/kumo/components/button";
import { Switch } from "@cloudflare/kumo/components/switch";
import { GlobeIcon, PathIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Node } from "~/api/queries.ts";
import { useNodeMutations } from "~/components/machines/mutations.ts";
import { withRouteApproved } from "~/components/networks/model.ts";
import { Code } from "~/components/ui/code.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { Flagged } from "~/components/ui/flagged.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { Status } from "~/components/ui/status.tsx";
import { toast } from "~/components/ui/toast.ts";
import { advertisesExit, exitRoutes, isExitNode, isExitRoute } from "~/lib/node.ts";

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
  const rawRoutes = new Set([...node.availableRoutes, ...node.approvedRoutes]);
  const hasExit = exitRoutes.some((route) => rawRoutes.has(route));
  const subnets = [...rawRoutes].filter((route) => !isExitRoute(route)).toSorted();
  const routes = hasExit ? [exitRoutes[0] ?? "", ...subnets] : subnets;

  function set(route: string, approved: boolean): void {
    const next = withRouteApproved(node, route, approved);
    setRoutes.mutate(
      { params: { path: { nodeId: node.id } }, body: { routes: next } },
      {
        onError: (error) => {
          toast.error(errorMessage(error));
        },
      },
    );
  }

  return (
    <Section title="Routes" description="Only approved routes reach the rest of the tailnet.">
      {routes.length === 0 ? (
        <SectionRow className="text-kumo-subtle">
          Nothing advertised. Run <Code>tailscale set --advertise-routes</Code> or{" "}
          <Code>--advertise-exit-node</Code> on the machine.
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
  const exit = isExitRoute(route);
  const approved = exit ? isExitNode(node) : node.approvedRoutes.includes(route);
  const available = exit ? advertisesExit(node) : node.availableRoutes.includes(route);

  return (
    <SectionRow className="flex flex-wrap items-center justify-between gap-3 py-2.5">
      <div className="flex min-w-0 items-center gap-2">
        <span className="flex h-lh items-center text-kumo-subtle">
          {exit ? <GlobeIcon size={routeIconSize} /> : <PathIcon size={routeIconSize} />}
        </span>
        <RouteName route={route} exit={exit} available={available} />
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <Status tone={approved ? "success" : "warning"}>{approved ? "Approved" : "Pending"}</Status>
        <Button
          variant={approved ? "ghost" : "secondary"}
          size="sm"
          disabled={disabled}
          onClick={() => {
            onSet(route, !approved);
          }}
        >
          {approved ? "Revoke" : "Approve"}
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
        <DisabledReason reason={eligible ? undefined : "Not advertising an exit node"}>
          <Switch
            aria-label="Use as global exit node"
            checked={node.globalExitNode}
            disabled={!canEdit || !eligible || setGlobalExitNode.isPending}
            onCheckedChange={(enabled) => {
              setGlobalExitNode.mutate({
                params: { path: { nodeId: node.id } },
                body: { enabled },
              });
            }}
          />
        </DisabledReason>
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

/** The route by name; one the machine stopped advertising is flagged, with the reason a hover away. */
function RouteName({
  route,
  exit,
  available,
}: {
  readonly route: string;
  readonly exit: boolean;
  readonly available: boolean;
}): ReactElement {
  const name = exit ? (
    <span className="font-medium text-kumo-default">Exit node</span>
  ) : (
    <span className="truncate font-mono text-[0.9em]">{route}</span>
  );

  if (available) {
    return name;
  }

  return (
    <Flagged
      title="No longer advertised"
      detail={
        exit
          ? "The machine stopped offering itself as an exit node. The approval stays until you reject it."
          : "The machine stopped advertising this route. The approval stays until you reject it."
      }
    >
      {name}
    </Flagged>
  );
}
