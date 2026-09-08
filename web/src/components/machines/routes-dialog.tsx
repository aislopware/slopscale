import { Checkbox } from "@cloudflare/kumo/components/checkbox";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import type { NodeDialogProps } from "~/components/machines/dialogs.tsx";
import { Code } from "~/components/ui/code.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";
import { isExitRoute } from "~/lib/node.ts";

export function RoutesDialog({
  node,
  open,
  onOpenChange,
  mutations,
}: NodeDialogProps): ReactElement {
  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title="Approve routes"
        description="Only approved routes are advertised to the rest of the tailnet."
      >
        <RoutesForm node={node} onOpenChange={onOpenChange} mutations={mutations} />
      </DialogContent>
    </DialogRoot>
  );
}

function RoutesForm({
  node,
  onOpenChange,
  mutations,
}: Omit<NodeDialogProps, "open">): ReactElement {
  const [approved, setApproved] = useState<readonly string[]>(node.approvedRoutes);
  const { setRoutes } = mutations;
  const available = [...new Set([...node.availableRoutes, ...node.approvedRoutes])].toSorted();
  const exit = available.filter((route) => isExitRoute(route));
  const subnets = available.filter((route) => !isExitRoute(route));

  function toggle(route: string, checked: boolean): void {
    setApproved((current) =>
      checked ? [...current, route] : current.filter((entry) => entry !== route),
    );
  }

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    setRoutes.mutate(
      { params: { path: { nodeId: node.id } }, body: { routes: [...approved] } },
      {
        onSuccess: () => {
          toast.success("Routes updated");
          onOpenChange(false);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      {available.length === 0 ? (
        <p className="text-kumo-subtle">
          This machine does not advertise any routes. Run{" "}
          <Code>tailscale set --advertise-routes</Code> or <Code>--advertise-exit-node</Code> on it
          first.
        </p>
      ) : null}
      {exit.length > 0 ? (
        <RouteGroup legend="Exit node" routes={exit} approved={approved} onToggle={toggle} />
      ) : null}
      {subnets.length > 0 ? (
        <RouteGroup legend="Subnet routes" routes={subnets} approved={approved} onToggle={toggle} />
      ) : null}
      <DialogError message={setRoutes.isError ? errorMessage(setRoutes.error) : undefined} />
      <FormFooter label="Save routes" pending={setRoutes.isPending} />
    </form>
  );
}

function RouteGroup({
  legend,
  routes,
  approved,
  onToggle,
}: {
  readonly legend: string;
  readonly routes: readonly string[];
  readonly approved: readonly string[];
  readonly onToggle: (route: string, checked: boolean) => void;
}): ReactElement {
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="mb-1.5 text-sm font-medium text-kumo-subtle">{legend}</legend>
      {routes.map((route) => (
        <Checkbox
          key={route}
          label={<span className="font-mono text-[0.9em]">{route}</span>}
          checked={approved.includes(route)}
          onCheckedChange={(checked) => {
            onToggle(route, checked);
          }}
        />
      ))}
    </fieldset>
  );
}
