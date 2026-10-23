import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { DnsRule } from "~/api/queries.ts";
import type { DnsRuleMutations } from "~/components/dns/rule-mutations.ts";
import { ConfirmDialog, ConfirmSummary } from "~/components/ui/confirm-dialog.tsx";

/** Asks before a group DNS rule goes, from the row menu or the rule's own page. */
export function DeleteDnsRuleDialog({
  rule,
  open,
  onOpenChange,
  mutations,
  onDeleted,
}: {
  readonly rule: DnsRule | undefined;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: DnsRuleMutations;
  /** Leaves the rule's page once it is gone. */
  readonly onDeleted?: () => void;
}): ReactElement {
  const { remove } = mutations;

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Delete DNS rule?"
      description="The machines in its groups stop using its nameservers for these domains."
      summary={
        rule === undefined ? undefined : (
          <ConfirmSummary name={rule.name}>
            <span className="font-mono text-xs">{rule.domains.join(", ")}</span>
          </ConfirmSummary>
        )
      }
      confirmLabel="Delete"
      loading={remove.isPending}
      error={remove.isError ? errorMessage(remove.error) : undefined}
      onConfirm={() => {
        if (rule !== undefined) {
          remove.mutate(
            { params: { path: { id: rule.id } } },
            {
              onSuccess: () => {
                onOpenChange(false);
                onDeleted?.();
              },
            },
          );
        }
      }}
    />
  );
}
