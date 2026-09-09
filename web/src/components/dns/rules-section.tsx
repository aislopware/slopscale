import { Button } from "@cloudflare/kumo/components/button";
import { PencilSimpleIcon, PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { DnsRule, Group } from "~/api/queries.ts";
import { GroupChips } from "~/components/access/group-chips.tsx";
import { DnsRuleDialog } from "~/components/dns/rule-dialog.tsx";
import type { DnsRuleMutations } from "~/components/dns/rule-mutations.ts";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { Section, SectionEmpty, SectionRow } from "~/components/ui/section.tsx";

const iconSize = 16;

type Dialog = "closed" | "new" | { readonly edit: DnsRule } | { readonly remove: DnsRule };

/** Split DNS that only some groups receive, one row per rule. */
export function DnsRulesSection({
  rules,
  groups,
  canEdit,
  mutations,
}: {
  readonly rules: readonly DnsRule[];
  readonly groups: readonly Group[];
  readonly canEdit: boolean;
  readonly mutations: DnsRuleMutations;
}): ReactElement {
  const [dialog, setDialog] = useState<Dialog>("closed");
  const close = (open: boolean): void => {
    if (!open) {
      setDialog("closed");
    }
  };
  const editing = typeof dialog === "object" && "edit" in dialog ? dialog.edit : undefined;
  const removing = typeof dialog === "object" && "remove" in dialog ? dialog.remove : undefined;

  return (
    <Section
      title="Split DNS per group"
      description="Split DNS that only the machines in some groups receive, on top of the split DNS above."
      bodyClassName="p-0"
      {...(canEdit
        ? {
            actions: (
              <Button
                variant="secondary"
                icon={PlusIcon}
                onClick={() => {
                  setDialog("new");
                }}
              >
                Add rule
              </Button>
            ),
          }
        : {})}
    >
      {rules.length === 0 ? (
        <SectionEmpty
          title="No group DNS rules"
          description="Every machine resolves names the same way."
        />
      ) : (
        rules.map((rule) => (
          <SectionRow key={rule.id} className="flex items-center justify-between gap-4 py-2.5">
            <RuleSummary rule={rule} groups={groups} />
            {canEdit ? (
              <span className="flex shrink-0 items-center">
                <Button
                  variant="ghost"
                  shape="square"
                  size="sm"
                  icon={<PencilSimpleIcon size={iconSize} />}
                  aria-label={`Edit DNS rule ${rule.name}`}
                  onClick={() => {
                    setDialog({ edit: rule });
                  }}
                />
                <Button
                  variant="ghost"
                  shape="square"
                  size="sm"
                  icon={<TrashIcon size={iconSize} />}
                  aria-label={`Delete DNS rule ${rule.name}`}
                  onClick={() => {
                    setDialog({ remove: rule });
                  }}
                />
              </span>
            ) : null}
          </SectionRow>
        ))
      )}
      <DnsRuleDialog
        key={editing?.id ?? "new"}
        rule={editing}
        groups={groups}
        open={dialog === "new" || editing !== undefined}
        onOpenChange={close}
        mutations={mutations}
      />
      <ConfirmDialog
        open={removing !== undefined}
        onOpenChange={close}
        title="Delete DNS rule?"
        description={
          removing === undefined
            ? ""
            : `The machines in its groups stop using ${removing.name}'s nameservers for ${removing.domains.join(", ")}.`
        }
        confirmLabel="Delete"
        loading={mutations.remove.isPending}
        error={mutations.remove.isError ? errorMessage(mutations.remove.error) : undefined}
        onConfirm={() => {
          if (removing !== undefined) {
            mutations.remove.mutate(
              { params: { path: { id: removing.id } } },
              {
                onSuccess: () => {
                  setDialog("closed");
                },
              },
            );
          }
        }}
      />
    </Section>
  );
}

function RuleSummary({
  rule,
  groups,
}: {
  readonly rule: DnsRule;
  readonly groups: readonly Group[];
}): ReactElement {
  return (
    <div className="flex min-w-0 flex-1 flex-col gap-1">
      <span className="flex items-center gap-2">
        <span className="truncate font-medium text-kumo-strong">{rule.name}</span>
        {rule.enabled ? null : <span className="text-xs text-kumo-subtle">Disabled</span>}
      </span>
      <div className="grid min-w-0 gap-x-6 gap-y-0.5 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.5fr)]">
        <span className="min-w-0 font-mono text-sm break-all text-kumo-default">
          {rule.domains.join(", ")}
        </span>
        <span className="min-w-0 font-mono text-sm break-all text-kumo-subtle">
          {rule.nameservers.join(", ")}
        </span>
      </div>
      <GroupChips ids={rule.groupIds} groups={groups} emptyLabel="No group" />
    </div>
  );
}
