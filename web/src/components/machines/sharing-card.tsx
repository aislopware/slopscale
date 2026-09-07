import { Button } from "@cloudflare/kumo/components/button";
import { ShareNetworkIcon, XIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Node, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { useNodeMutations } from "~/components/machines/mutations.ts";
import { Card, CardBody, CardDescription, CardHeader, CardTitle } from "~/components/ui/card.tsx";
import { isTagged, userLabel } from "~/lib/node.ts";

/** Who else may reach this machine; sharing itself happens from the actions menu. */
export function SharingCard({
  node,
  users,
  me,
}: {
  readonly node: Node;
  readonly users: readonly User[];
  readonly me: Me;
}): ReactElement {
  const { unshare } = useNodeMutations();
  const ownNode = me.user !== undefined && node.user.id === me.user.id;
  const canEdit = can(me, "devices:core") || ownNode;

  return (
    <Card>
      <CardHeader>
        <div>
          <CardTitle className="flex items-center gap-1.5">
            <span className="flex h-lh items-center">
              <ShareNetworkIcon className="text-kumo-subtle" />
            </span>
            Sharing
          </CardTitle>
          <CardDescription>
            Users whose devices may reach this machine. Use the actions menu to share it.
          </CardDescription>
        </div>
      </CardHeader>
      <CardBody className="p-0">
        {node.sharedWith.length === 0 ? (
          <p className="px-5 py-4 text-kumo-subtle">
            {isTagged(node) ? "Tagged machines cannot be shared." : "Not shared with anyone."}
          </p>
        ) : (
          <ul className="divide-y divide-kumo-line">
            {node.sharedWith.map((userId) => {
              const user = users.find((candidate) => candidate.id === userId);

              return (
                <li key={userId} className="flex items-center justify-between gap-4 px-5 py-3">
                  <span>{user === undefined ? `User ${userId}` : userLabel(user)}</span>
                  {canEdit ? (
                    <Button
                      variant="ghost"
                      shape="square"
                      size="sm"
                      icon={XIcon}
                      aria-label="Stop sharing"
                      loading={unshare.isPending}
                      onClick={() => {
                        unshare.mutate({ params: { path: { nodeId: node.id, userId } } });
                      }}
                    />
                  ) : null}
                </li>
              );
            })}
          </ul>
        )}
      </CardBody>
    </Card>
  );
}
