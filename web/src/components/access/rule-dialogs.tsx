import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { AccessRule } from "~/api/queries.ts";
import type { AccessMutations } from "~/components/access/mutations.ts";
import { ConfirmDialog, DeleteDialog } from "~/components/ui/confirm-dialog.tsx";

/**
 * Deletes a rule: the typed-name check for an ordinary one, and a plain warning when the rule is
 * the last thing keeping the tailnet closed.
 */
export function DeleteRuleDialog({
  rule,
  opensTailnet,
  open,
  onOpenChange,
  mutations,
  onDeleted,
}: {
  readonly rule: AccessRule;
  /** Whether this is the last enabled rule with no restricting policy file behind it. */
  readonly opensTailnet: boolean;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
  /** Runs once the rule is gone, such as leaving the rule's own page. */
  readonly onDeleted?: () => void;
}): ReactElement {
  const { deleteRule } = mutations;
  const remove = (): void => {
    // Awaited rather than passed to mutate: the refetch after a delete drops the record, and
    // the page that showed it may unmount before mutate's own callbacks would run.
    void (async (): Promise<void> => {
      try {
        await deleteRule.mutateAsync({ params: { path: { id: rule.id } } });
      } catch {
        // The dialog shows the refusal from the mutation's own state.
        return;
      }
      onOpenChange(false);
      onDeleted?.();
    })();
  };

  // Deleting the record is the small part; opening the tailnet is what needs a clear yes.
  if (opensTailnet) {
    return (
      <ConfirmDialog
        open={open}
        onOpenChange={onOpenChange}
        title="Delete the last enabled rule?"
        description={`"${rule.name}" is the only enabled rule and the policy file restricts nothing. Without it every machine can reach every other machine.`}
        confirmLabel="Delete rule"
        loading={deleteRule.isPending}
        error={deleteRule.isError ? errorMessage(deleteRule.error) : undefined}
        onConfirm={remove}
      />
    );
  }

  return (
    <DeleteDialog
      open={open}
      onOpenChange={onOpenChange}
      resourceType="rule"
      resourceName={rule.name}
      deleteButtonText="Delete rule"
      isDeleting={deleteRule.isPending}
      {...(deleteRule.isError ? { errorMessage: errorMessage(deleteRule.error) } : {})}
      onDelete={remove}
    />
  );
}
