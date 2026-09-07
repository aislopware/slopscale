import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Group } from "~/api/queries.ts";
import type { AccessMutations } from "~/components/access/mutations.ts";
import { groupItems } from "~/components/access/pickers.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { toast } from "~/components/ui/toast.ts";

/** Which record the dialog puts into groups; the API has one endpoint per kind. */
export type Member = { readonly nodeId: string } | { readonly userId: string };

export interface MembershipDialogProps {
  readonly title: string;
  readonly description: string;
  readonly member: Member;
  readonly groups: readonly Group[];
  /** The groups the record is a direct member of now. */
  readonly current: readonly string[];
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
}

/**
 * Edits the groups one machine or user is in. The picker holds the whole set and saving sends the
 * difference, one call per group joined or left, so the audit log records each change.
 */
export function MembershipDialog(props: MembershipDialogProps): ReactElement {
  return (
    <DialogRoot open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent size="base" title={props.title} description={props.description}>
        <MembershipForm {...props} />
      </DialogContent>
    </DialogRoot>
  );
}

function MembershipForm({
  member,
  groups,
  current,
  onOpenChange,
  mutations,
}: Omit<MembershipDialogProps, "open" | "title" | "description">): ReactElement {
  const [selected, setSelected] = useState<string[]>([...current]);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();
  const added = selected.filter((id) => !current.includes(id));
  const removed = current.filter((id) => !selected.includes(id));
  const dirty = added.length > 0 || removed.length > 0;

  function leave(id: string): Promise<unknown> {
    return "nodeId" in member
      ? mutations.removeNode.mutateAsync({ params: { path: { id, nodeId: member.nodeId } } })
      : mutations.removeUser.mutateAsync({ params: { path: { id, userId: member.userId } } });
  }

  async function save(): Promise<void> {
    setPending(true);
    setError(undefined);

    const calls = [
      ...added.map((id) =>
        mutations.addMember.mutateAsync({ params: { path: { id } }, body: member }),
      ),
      ...removed.map((id) => leave(id)),
    ];
    const outcome = await Promise.allSettled(calls);
    const failed = outcome.find((result) => result.status === "rejected");

    setPending(false);

    if (failed === undefined) {
      toast.success("Groups updated");
      onOpenChange(false);
    } else {
      setError(errorMessage(failed.reason));
    }
  }

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    void save();
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <MultiPicker
        label="Groups"
        description="The built-in group holds every machine and cannot be edited."
        placeholder="Add groups…"
        items={groupItems(groups, { builtin: false })}
        value={selected}
        onValueChange={setSelected}
        empty="No group matches. Create one under Access controls."
      />
      <DialogError message={error} />
      <FormFooter label="Save" pending={pending} disabled={!dirty} />
    </form>
  );
}
