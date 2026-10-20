import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import { CheckIcon, TerminalWindowIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Node, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { MachineMenu } from "~/components/machines/menu.tsx";
import { useNodeMutations } from "~/components/machines/mutations.ts";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { FullBleed } from "~/components/ui/page.tsx";
import { AppLink } from "~/lib/link.tsx";

/** The machine page's tabs, each an address of its own under the machine's. */
export const machineTabs = ["overview", "routes", "diagnostics", "settings"] as const;
export type MachineTab = (typeof machineTabs)[number];

const tabLabels: Record<MachineTab, string> = {
  overview: "Overview",
  routes: "Routes",
  diagnostics: "Diagnostics",
  settings: "Settings",
};

/** The tab an address under a machine is, from its last segment: Overview for the machine's own. */
export function machineTabOf(pathname: string): MachineTab {
  const last = pathname.split("/").findLast((segment) => segment !== "");

  return machineTabs.find((tab) => tab !== "overview" && tab === last) ?? "overview";
}

function tabHref(nodeId: string, tab: MachineTab): string {
  return tab === "overview" ? `/machines/${nodeId}` : `/machines/${nodeId}/${tab}`;
}

/**
 * The machine page's bar under the top bar, as a Zero Trust detail page draws it: Kumo's segmented
 * Tabs on the left, one link per tab so each is an address, and what can be done to the machine on
 * the right. It spans the page edge to edge with a rule under it and stays put while the tab
 * scrolls; on a phone the actions wrap under the tabs and the bar scrolls away with the page.
 */
export function MachineTabBar({
  node,
  me,
  users,
  tab,
}: {
  readonly node: Node;
  readonly me: Me;
  readonly users: readonly User[];
  readonly tab: MachineTab;
}): ReactElement {
  return (
    <FullBleed className="-mt-8 grid grid-cols-subgrid border-b border-kumo-line bg-kumo-canvas md:sticky md:top-(--app-header-height,0px) md:z-5">
      <nav
        aria-label="Machine"
        className="col-start-2 flex min-h-[58px] min-w-0 flex-wrap items-center justify-between gap-x-4 gap-y-2 py-2.5"
      >
        <Tabs
          variant="segmented"
          value={tab}
          tabs={machineTabs.map((value) => ({
            value,
            label: tabLabels[value],
            nativeButton: false,
            render: <AppLink href={tabHref(node.id, value)} />,
          }))}
        />
        <div className="flex max-w-full flex-wrap items-center gap-2">
          <SSHButton node={node} me={me} />
          <MachineMenu node={node} me={me} users={users} labelled hideDestructive />
          {!node.approved && can(me, "devices:core") ? <ApproveButton node={node} /> : null}
        </div>
      </nav>
    </FullBleed>
  );
}

/**
 * Why SSH is unavailable, or undefined while it works: the session needs a signed-in user, and a
 * connected machine that runs Tailscale SSH (`tailscale set --ssh`).
 */
export function sshDisabledReason(
  node: Pick<Node, "online" | "sshServer">,
  me: Pick<Me, "user">,
): string | undefined {
  if (me.user === undefined || me.user === null) {
    return "SSH requires a user login";
  }
  if (!node.online) {
    return "Machine is offline";
  }
  if (!node.sshServer) {
    return "Machine does not run Tailscale SSH";
  }
  return undefined;
}

function SSHButton({ node, me }: { readonly node: Node; readonly me: Me }): ReactElement {
  const reason = sshDisabledReason(node, me);

  if (reason !== undefined) {
    return (
      <DisabledReason reason={reason}>
        <Button variant="secondary" icon={TerminalWindowIcon} disabled>
          SSH
        </Button>
      </DisabledReason>
    );
  }

  return (
    <LinkButton href={`/machines/${node.id}/ssh`} variant="secondary" icon={TerminalWindowIcon}>
      SSH
    </LinkButton>
  );
}

function ApproveButton({ node }: { readonly node: Node }): ReactElement {
  const { approve } = useNodeMutations();

  return (
    <Button
      variant="primary"
      icon={CheckIcon}
      loading={approve.isPending}
      onClick={() => {
        approve.mutate({ params: { path: { nodeId: node.id } }, body: {} });
      }}
    >
      Approve
    </Button>
  );
}
