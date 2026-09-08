import { Select } from "@cloudflare/kumo/components/select";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { User } from "~/api/queries.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import type { NodeDialogProps } from "~/components/machines/dialogs.tsx";
import { ownerId } from "~/components/machines/owner.ts";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";
import { userLabel } from "~/lib/node.ts";

type ShareDialogProps = NodeDialogProps & { readonly users: readonly User[] };

function userName(users: readonly User[], id: string): string {
  const user = users.find((candidate) => candidate.id === id);

  return user === undefined ? "" : userLabel(user);
}

export function ShareDialog({
  node,
  users,
  open,
  onOpenChange,
  mutations,
}: ShareDialogProps): ReactElement {
  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title="Share machine"
        description="The user's devices can reach this machine as if it were their own. The policy's autogroup:shared decides what they may access."
      >
        <ShareForm node={node} users={users} onOpenChange={onOpenChange} mutations={mutations} />
      </DialogContent>
    </DialogRoot>
  );
}

function ShareForm({
  node,
  users,
  onOpenChange,
  mutations,
}: Omit<ShareDialogProps, "open">): ReactElement {
  const candidates = users.filter(
    (user) => user.id !== ownerId(node) && !node.sharedWith.includes(user.id),
  );
  const [userId, setUserId] = useState(candidates[0]?.id ?? "");
  const { share } = mutations;

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    share.mutate(
      { params: { path: { nodeId: node.id } }, body: { userId } },
      {
        onSuccess: () => {
          toast.success("Machine shared");
          onOpenChange(false);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      {candidates.length === 0 ? (
        <p className="text-kumo-subtle">Every other user already has access.</p>
      ) : (
        <Select
          className="w-full"
          label="Share with"
          value={userId}
          onValueChange={(value) => {
            setUserId(value ?? "");
          }}
          placeholder="Choose a user"
          renderValue={(value) => userName(candidates, value)}
        >
          {candidates.map((user) => (
            <Select.Option key={user.id} value={user.id}>
              <span className="flex flex-col gap-0.5">
                <span>{userLabel(user)}</span>
                <span className="text-sm text-kumo-subtle">
                  {user.email === "" ? user.name : user.email}
                </span>
              </span>
            </Select.Option>
          ))}
        </Select>
      )}
      <DialogError message={share.isError ? errorMessage(share.error) : undefined} />
      <FormFooter label="Share" pending={share.isPending} disabled={userId === ""} />
    </form>
  );
}
