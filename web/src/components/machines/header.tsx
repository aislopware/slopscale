import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { CheckIcon, TerminalWindowIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Node, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { MachineMenu } from "~/components/machines/menu.tsx";
import { useNodeMutations } from "~/components/machines/mutations.ts";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { nodeName } from "~/lib/node.ts";

/**
 * The machine page's title row: its name with the docs pill, and what can be done to it on the
 * right. How it is doing (state, owner, address) is the summary strip under the rule.
 */
export function MachineHeader({
  node,
  me,
  users,
}: {
  readonly node: Node;
  readonly me: Me;
  readonly users: readonly User[];
}): ReactElement {
  return (
    <PageHeader
      title={nodeName(node)}
      docs="ref/device-management"
      actions={
        <>
          <SSHButton node={node} me={me} />
          <MachineMenu node={node} me={me} users={users} labelled hideDestructive />
          {!node.approved && can(me, "devices:core") ? <ApproveButton node={node} /> : null}
        </>
      }
    />
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
