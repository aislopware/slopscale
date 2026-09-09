import { Badge } from "@cloudflare/kumo/components/badge";
import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { CheckIcon, TerminalWindowIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Node, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { MachineMenu } from "~/components/machines/menu.tsx";
import { useNodeMutations } from "~/components/machines/mutations.ts";
import { StatusBadge } from "~/components/machines/status-badge.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { isTagged, nodeName, nodeStatus, ownerLabel } from "~/lib/node.ts";

const registerMethods: Record<string, string> = {
  REGISTER_METHOD_AUTH_KEY: "registered with a pre-auth key",
  REGISTER_METHOD_CLI: "registered from the command line",
  REGISTER_METHOD_OIDC: "signed in through the identity provider",
};

/** The machine page's title row: what it is, who owns it, how it joined and how it is doing. */
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
      eyebrow={
        <>
          <StatusBadge status={nodeStatus(node)} />
          {node.tags.map((tag) => (
            <Badge key={tag} variant="secondary">
              <span className="font-mono">{tag}</span>
            </Badge>
          ))}
        </>
      }
      title={nodeName(node)}
      meta={<MachineFacts node={node} />}
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

function MachineFacts({ node }: { readonly node: Node }): ReactElement {
  return (
    <>
      <span>{isTagged(node) ? "Tagged machine" : ownerLabel(node)}</span>
      <span aria-hidden>·</span>
      <span>{registerMethods[node.registerMethod] ?? "registered"}</span>
      {node.ephemeral ? (
        <>
          <span aria-hidden>·</span>
          <span>ephemeral, deleted when it logs out or goes offline</span>
        </>
      ) : null}
      <span aria-hidden>·</span>
      <span>
        {node.online ? (
          "connected"
        ) : (
          <>
            {"last seen "}
            <RelativeTime value={node.lastSeen} />
          </>
        )}
      </span>
    </>
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
