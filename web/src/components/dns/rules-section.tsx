import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { PencilSimpleIcon, PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { DnsRule, Group } from "~/api/queries.ts";
import { GroupNames } from "~/components/access/group-names.tsx";
import { AddressList, ListCell, ListRow, ListTable } from "~/components/dns/list-table.tsx";
import { DnsRuleDialog } from "~/components/dns/rule-dialog.tsx";
import type { DnsRuleMutations } from "~/components/dns/rule-mutations.ts";
import { ConfirmDialog, ConfirmSummary } from "~/components/ui/confirm-dialog.tsx";
import { Domain } from "~/components/ui/domain.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";

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
      panel={false}
      {...(canEdit
        ? {
            actions: (
              <Button
                variant="secondary"
                size="sm"
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
      <ListTable
        columns={[
          { key: "name", label: "Rule", className: "w-[22%]" },
          { key: "domains", label: "Domains" },
          { key: "servers", label: "Nameservers" },
          { key: "groups", label: "Groups", className: "w-[18%] min-w-32" },
        ]}
        actions={canEdit}
        empty={
          rules.length === 0 ? (
            <SectionEmpty
              title="No group DNS rules"
              description="Every machine resolves names the same way."
            />
          ) : null
        }
      >
        {rules.map((rule) => (
          <RuleRow
            key={rule.id}
            rule={rule}
            groups={groups}
            canEdit={canEdit}
            onEdit={() => {
              setDialog({ edit: rule });
            }}
            onRemove={() => {
              setDialog({ remove: rule });
            }}
          />
        ))}
      </ListTable>
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
        description="The machines in its groups stop using its nameservers for these domains."
        summary={
          removing === undefined ? undefined : (
            <ConfirmSummary name={removing.name}>
              <span className="font-mono text-xs">{removing.domains.join(", ")}</span>
            </ConfirmSummary>
          )
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

/** One group DNS rule: its name and state, domains, resolvers, groups and menu. */
function RuleRow({
  rule,
  groups,
  canEdit,
  onEdit,
  onRemove,
}: {
  readonly rule: DnsRule;
  readonly groups: readonly Group[];
  readonly canEdit: boolean;
  readonly onEdit: () => void;
  readonly onRemove: () => void;
}): ReactElement {
  return (
    <ListRow>
      <ListCell>
        <span className="flex min-w-0 flex-col gap-0.5">
          <span className="truncate font-medium text-kumo-default">{rule.name}</span>
          {rule.enabled ? null : <span className="text-xs text-kumo-subtle">Disabled</span>}
        </span>
      </ListCell>
      <ListCell>
        <span className="flex min-w-0 flex-wrap gap-x-2 gap-y-0.5">
          {rule.domains.map((domain) => (
            <Domain key={domain} domain={domain} />
          ))}
        </span>
      </ListCell>
      <ListCell className="font-mono text-sm text-kumo-subtle">
        <AddressList addresses={rule.nameservers} />
      </ListCell>
      <ListCell>
        <GroupNames ids={rule.groupIds} groups={groups} emptyLabel="No group" />
      </ListCell>
      {canEdit ? (
        <ListCell actions>
          <RowMenu label={`Actions for DNS rule ${rule.name}`}>
            <DropdownMenu.Item icon={PencilSimpleIcon} onClick={onEdit}>
              Edit…
            </DropdownMenu.Item>
            <DropdownMenu.Separator />
            <DropdownMenu.Item icon={TrashIcon} variant="danger" onClick={onRemove}>
              Delete…
            </DropdownMenu.Item>
          </RowMenu>
        </ListCell>
      ) : null}
    </ListRow>
  );
}
