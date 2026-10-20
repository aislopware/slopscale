import { Button } from "@cloudflare/kumo/components/button";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { AccessRule, Posture } from "~/api/queries.ts";
import type { AccessMutations } from "~/components/access/mutations.ts";
import { rulesUsingPosture } from "~/components/access/posture-model.ts";
import { DeleteDialog } from "~/components/ui/confirm-dialog.tsx";
import { DialogClose, DialogContent, DialogFooter, DialogRoot } from "~/components/ui/dialog.tsx";

/**
 * Deletes a posture, or says which rules still require it: the server refuses to delete one a rule
 * names.
 */
export function DeletePostureDialog({
  posture,
  rules,
  open,
  onOpenChange,
  mutations,
  onDeleted,
}: {
  readonly posture: Posture;
  readonly rules: readonly AccessRule[];
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
  /** Runs once the posture is gone, such as leaving the posture's own page. */
  readonly onDeleted?: () => void;
}): ReactElement {
  const { deletePosture } = mutations;
  const using = rulesUsingPosture(rules, posture);

  // The server refuses to delete a posture a rule names, so say which rules instead of asking
  // for a confirmation that could only fail.
  if (using.length > 0) {
    return (
      <DialogRoot open={open} onOpenChange={onOpenChange}>
        <DialogContent
          size="sm"
          title="Posture in use"
          description={`${posture.name} is required by ${using.length === 1 ? "a rule" : `${using.length} rules`}. Edit or delete them first.`}
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

  return (
    <DeleteDialog
      open={open}
      onOpenChange={onOpenChange}
      resourceType="posture"
      resourceName={posture.name}
      deleteButtonText="Delete posture"
      isDeleting={deletePosture.isPending}
      {...(deletePosture.isError ? { errorMessage: errorMessage(deletePosture.error) } : {})}
      onDelete={() => {
        // Awaited rather than passed to mutate: the refetch after a delete drops the record, and
        // the page that showed it may unmount before mutate's own callbacks would run.
        void (async (): Promise<void> => {
          try {
            await deletePosture.mutateAsync({ params: { path: { id: posture.id } } });
          } catch {
            // The dialog shows the refusal from the mutation's own state.
            return;
          }
          onOpenChange(false);
          onDeleted?.();
        })();
      }}
    />
  );
}
