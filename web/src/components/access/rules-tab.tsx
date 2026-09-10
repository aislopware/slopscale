import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { PlusIcon } from "@phosphor-icons/react";
import { useDeferredValue, useMemo, useState } from "react";
import type { ReactElement } from "react";

import type { AccessRule, Group, Posture } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { ruleColumns, toRuleRows } from "~/components/access/rule-columns.tsx";
import { RuleDialog } from "~/components/access/rule-dialogs.tsx";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";
import { Callout } from "~/components/ui/callout.tsx";
import { Frame } from "~/components/ui/frame.tsx";

export interface RulesTabProps {
  readonly me: Me;
  readonly rules: readonly AccessRule[];
  readonly groups: readonly Group[];
  readonly postures: readonly Posture[];
  /** Whether the policy file restricts traffic on its own; false means the rules are all there is. */
  readonly policyFileEnforces: boolean;
  readonly search: string;
  readonly onSearchChange: (value: string) => void;
}

/** Rules between groups, the way most operators will write access control. */
export function RulesTab({
  me,
  rules,
  groups,
  postures,
  policyFileEnforces,
  search,
  onSearchChange,
}: RulesTabProps): ReactElement {
  const canEdit = can(me, "policy_file");
  const query = useDeferredValue(search);
  const [creating, setCreating] = useState(false);
  const mutations = useAccessMutations();
  const enabled = rules.filter((rule) => rule.enabled).length;
  const rows = useMemo(() => toRuleRows(rules, groups, postures), [rules, groups, postures]);

  const table = useAppTable({
    data: rows,
    columns: ruleColumns,
    getRowId: (rule) => rule.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me, groups, postures, rules, policyFileEnforces },
  });

  const total = rules.length;
  const shown = table.getRowModel().rows.length;

  return (
    <>
      <StateBanner enabled={enabled} policyFileEnforces={policyFileEnforces} />
      <TableToolbar
        actions={
          <Button
            variant="primary"
            icon={PlusIcon}
            disabled={!canEdit}
            onClick={() => {
              setCreating(true);
            }}
          >
            New rule
          </Button>
        }
      >
        <SearchInput
          value={search}
          placeholder="Search by name, group or protocol"
          onValueChange={onSearchChange}
        />
      </TableToolbar>
      <Frame>
        <table.AppTable>
          <DataTable
            empty={
              total === 0 ? (
                <Empty
                  className={tableEmptyClass}
                  size="sm"
                  title="No rules yet"
                  description={
                    policyFileEnforces
                      ? "The policy file decides who reaches what. A rule adds to it."
                      : "Every machine can reach every other machine. The first enabled rule blocks everything it does not allow."
                  }
                  contents={
                    <Button
                      variant="primary"
                      disabled={!canEdit}
                      onClick={() => {
                        setCreating(true);
                      }}
                    >
                      New rule
                    </Button>
                  }
                />
              ) : (
                <Empty
                  className={tableEmptyClass}
                  size="sm"
                  title="No rules match"
                  contents={
                    <Button
                      variant="secondary"
                      onClick={() => {
                        onSearchChange("");
                      }}
                    >
                      Clear search
                    </Button>
                  }
                />
              )
            }
            footer={
              total === 0 ? undefined : (
                <TableFooter>{`Showing ${shown} of ${countRules(total)} · ${enabled} enabled`}</TableFooter>
              )
            }
          />
        </table.AppTable>
      </Frame>
      <RuleDialog
        groups={groups}
        postures={postures}
        policyFileEnforces={policyFileEnforces}
        open={creating}
        onOpenChange={setCreating}
        mutations={mutations}
      />
    </>
  );
}

function countRules(total: number): string {
  return total === 1 ? "1 rule" : `${total} rules`;
}

/** What the tailnet does right now, so a disabled last rule is never a surprise. */
function StateBanner({
  enabled,
  policyFileEnforces,
}: {
  readonly enabled: number;
  readonly policyFileEnforces: boolean;
}): ReactElement | null {
  if (policyFileEnforces) {
    return (
      <Callout
        title="The policy file restricts access"
        description="Rules add to what the file allows and cannot take any of it away. Its tags, SSH rules and autogroups still apply."
      />
    );
  }

  if (enabled === 0) {
    return (
      <Callout
        tone="warning"
        title="The tailnet is open"
        description="No rule is enabled and the policy file restricts nothing, so every machine can reach every other machine."
      />
    );
  }

  return null;
}
