import { Button } from "@cloudflare/kumo/components/button";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { AccessRule, Group } from "~/api/queries.ts";
import { isSynced, rulesUsingGroup } from "~/components/access/model.ts";
import type { AccessMutations } from "~/components/access/mutations.ts";
import { DeleteDialog } from "~/components/ui/confirm-dialog.tsx";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";

/**
 * Deletes a group: says which rules still name it (the server refuses those), warns that a synced
 * group comes back at the next sign-in, and otherwise asks for the name.
 */
export function DeleteGroupDialog({
  group,
  rules,
  open,
  onOpenChange,
  mutations,
  onDeleted,
}: {
  readonly group: Group;
  readonly rules: readonly AccessRule[];
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
  /** Runs once the group is gone, such as leaving the group's own page. */
  readonly onDeleted?: () => void;
}): ReactElement {
  const { deleteGroup } = mutations;
  const using = rulesUsingGroup(rules, group);

  // The server refuses to delete a group a rule names, so say which rules instead of asking for
  // a confirmation that could only fail.
  if (using.length > 0) {
    return (
      <DialogRoot open={open} onOpenChange={onOpenChange}>
        <DialogContent
          size="sm"
          title="Group in use"
          description={`${group.name} is named by ${using.length === 1 ? "a rule" : `${using.length} rules`}. Edit or delete them first.`}
        >
          <ul className="flex flex-col gap-1 text-kumo-default">
            {using.map((rule) => (
              <li key={rule.id} className="truncate">
                <Link
                  to="/policy/rules/$ruleId"
                  params={{ ruleId: rule.id }}
                  className="hover:text-kumo-link"
                  onClick={() => {
                    onOpenChange(false);
                  }}
                >
                  {rule.name}
                </Link>
              </li>
            ))}
          </ul>
          <DialogFooter>
            <DialogClose render={<Button variant="secondary">Close</Button>} />
          </DialogFooter>
        </DialogContent>
      </DialogRoot>
    );
  }

  const remove = (): void => {
    // Awaited rather than passed to mutate: the refetch after a delete drops the record, and
    // the page that showed it may unmount before mutate's own callbacks would run.
    void (async (): Promise<void> => {
      try {
        await deleteGroup.mutateAsync({ params: { path: { id: group.id } } });
      } catch {
        // The dialog shows the refusal from the mutation's own state.
        return;
      }
      onOpenChange(false);
      onDeleted?.();
    })();
  };

  // A synced group comes back at the next sign-in that carries its claim, as a new group without
  // this one's description, machines or rules, so say that instead of promising a lasting delete.
  if (isSynced(group)) {
    return (
      <DialogRoot open={open} onOpenChange={onOpenChange}>
        <DialogContent
          size="sm"
          title="Delete synced group"
          description={`${group.name} is synced from the identity provider. It comes back empty at the next sign-in that names it, unless you remove the claim at the provider or turn group sync off.`}
        >
          <DialogError
            message={deleteGroup.isError ? errorMessage(deleteGroup.error) : undefined}
          />
          <DialogFooter>
            <Button variant="destructive" loading={deleteGroup.isPending} onClick={remove}>
              Delete group
            </Button>
            <DialogClose render={<Button variant="secondary">Cancel</Button>} />
          </DialogFooter>
        </DialogContent>
      </DialogRoot>
    );
  }

  return (
    <DeleteDialog
      open={open}
      onOpenChange={onOpenChange}
      resourceType="group"
      resourceName={group.name}
      deleteButtonText="Delete group"
      isDeleting={deleteGroup.isPending}
      {...(deleteGroup.isError ? { errorMessage: errorMessage(deleteGroup.error) } : {})}
      onDelete={remove}
    />
  );
}
