import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Group } from "~/api/queries.ts";
import { isBuiltin } from "~/components/access/model.ts";
import type { AccessMutations } from "~/components/access/mutations.ts";
import { groupItems } from "~/components/access/pickers.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { DateTimeField } from "~/components/ui/date-time-field.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { toast } from "~/components/ui/toast.ts";
import { fromLocalInput } from "~/lib/time.ts";

/**
 * The note about the built-in group, for a record that is in one. The picker does not offer them,
 * so the note belongs only to a chip that is already there.
 */
function builtinNote(groups: readonly Group[], selected: readonly string[]): string | undefined {
  const present = selected.some((id) =>
    groups.some((group) => group.id === id && isBuiltin(group)),
  );

  return present ? "The built-in group holds every machine and cannot be edited." : undefined;
}

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
  const [expires, setExpires] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();
  const added = selected.filter((id) => !current.includes(id));
  const removed = current.filter((id) => !selected.includes(id));
  const dirty = added.length > 0 || removed.length > 0;
  const note = builtinNote(groups, selected);

  function leave(id: string): Promise<unknown> {
    return "nodeId" in member
      ? mutations.removeNode.mutateAsync({ params: { path: { id, nodeId: member.nodeId } } })
      : mutations.removeUser.mutateAsync({ params: { path: { id, userId: member.userId } } });
  }

  async function save(): Promise<void> {
    setPending(true);
    setError(undefined);

    const expiresAt = fromLocalInput(expires);
    const body = expiresAt === undefined ? member : { ...member, expiresAt };
    const calls = [
      ...added.map((id) => mutations.addMember.mutateAsync({ params: { path: { id } }, body })),
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
        {...(note === undefined ? {} : { description: note })}
        placeholder="Add groups…"
        items={groupItems(groups, "membership")}
        value={selected}
        onValueChange={setSelected}
        empty="No group matches. Create one under Access controls."
      />
      {added.length === 0 ? null : (
        <DateTimeField
          label="Until"
          required={false}
          emptyLabel="No end"
          description="The selected groups are removed again at this time. With no time they are kept for good."
          value={expires}
          onChange={setExpires}
        />
      )}
      <DialogError message={error} />
      <FormFooter label="Save" pending={pending} disabled={!dirty} />
    </form>
  );
}
