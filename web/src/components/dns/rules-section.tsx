import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { cn } from "@cloudflare/kumo/utils";
import { PencilSimpleIcon, PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import type { DnsRule, Group } from "~/api/queries.ts";
import { GroupNames } from "~/components/access/group-names.tsx";
import { AddressList, ListCell, ListRow, ListTable } from "~/components/dns/list-table.tsx";
import { DeleteDnsRuleDialog } from "~/components/dns/rule-delete-dialog.tsx";
import type { DnsRuleMutations } from "~/components/dns/rule-mutations.ts";
import { rowLinkClass } from "~/components/table/styles.ts";
import { Domain } from "~/components/ui/domain.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";

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
  const [removing, setRemoving] = useState<DnsRule | undefined>();
  const navigate = useNavigate();

  return (
    <Section
      title="Split DNS per group"
      description="Split DNS that only the machines in some groups receive, on top of the split DNS above."
      panel={false}
      actions={
        canEdit ? (
          <LinkButton href="/dns/split/rules/new" variant="secondary" size="sm" icon={PlusIcon}>
            Add rule
          </LinkButton>
        ) : (
          <Button variant="secondary" size="sm" icon={PlusIcon} disabled>
            Add rule
          </Button>
        )
      }
    >
      <ListTable
        columns={[
          { key: "name", label: "Rule", className: "w-[22%]" },
          { key: "domains", label: "Domains" },
          { key: "servers", label: "Nameservers" },
          { key: "groups", label: "Groups", className: "w-[18%] min-w-32" },
        ]}
        actions
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
              void navigate({ to: "/dns/split/rules/$ruleId", params: { ruleId: rule.id } });
            }}
            onRemove={() => {
              setRemoving(rule);
            }}
          />
        ))}
      </ListTable>
      <DeleteDnsRuleDialog
        rule={removing}
        open={removing !== undefined}
        onOpenChange={(open) => {
          if (!open) {
            setRemoving(undefined);
          }
        }}
        mutations={mutations}
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
          <Link
            to="/dns/split/rules/$ruleId"
            params={{ ruleId: rule.id }}
            className={cn("truncate font-medium", rowLinkClass)}
          >
            {rule.name}
          </Link>
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
      <ListCell actions>
        <RowMenu label={`Actions for DNS rule ${rule.name}`}>
          <DropdownMenu.Item icon={PencilSimpleIcon} onClick={onEdit}>
            {canEdit ? "Edit" : "View"}
          </DropdownMenu.Item>
          <DropdownMenu.Separator />
          <DropdownMenu.Item
            icon={TrashIcon}
            variant="danger"
            disabled={!canEdit}
            onClick={onRemove}
          >
            Delete…
          </DropdownMenu.Item>
        </RowMenu>
      </ListCell>
    </ListRow>
  );
}
