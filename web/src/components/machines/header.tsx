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
import { OsMark } from "~/components/ui/os-mark.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { TagList } from "~/components/ui/tag.tsx";
import { isTagged, nodeName, nodeStatus, ownerLabel } from "~/lib/node.ts";
import { osLabel } from "~/lib/os.ts";

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

/**
 * The line under the name: the state first, since it is the fact the operator came for, then who
 * owns it (its tags, for a tagged machine), how it joined and when it was last seen.
 */
function MachineFacts({ node }: { readonly node: Node }): ReactElement {
  return (
    <>
      <StatusBadge status={nodeStatus(node)} />
      <span aria-hidden>·</span>
      {isTagged(node) ? <TagList tags={node.tags} size="sm" /> : <span>{ownerLabel(node)}</span>}
      {node.os === "" ? null : (
        <>
          <span aria-hidden>·</span>
          <span className="inline-flex items-center gap-1">
            <OsMark os={node.os} version={node.osVersion} />
            {osLabel(node.os, node.osVersion)}
          </span>
        </>
      )}
      <span aria-hidden>·</span>
      <span>{registerMethods[node.registerMethod] ?? "registered"}</span>
      {node.ephemeral ? (
        <>
          <span aria-hidden>·</span>
          <span>ephemeral, deleted when it logs out or goes offline</span>
        </>
      ) : null}
      {/* The badge above already says a connected machine is connected. */}
      {node.online ? null : (
        <>
          <span aria-hidden>·</span>
          <span>
            {"last seen "}
            <RelativeTime value={node.lastSeen} />
          </span>
        </>
      )}
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
