import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import type { AccessRule } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { DeleteRuleDialog } from "~/components/access/rule-dialogs.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";

/** Edit and delete for one rule, gated by the policy scope. Editing opens the rule's own page. */
export function RuleMenu({
  rule,
  rules,
  policyFileEnforces,
  me,
}: {
  readonly rule: AccessRule;
  readonly rules: readonly AccessRule[];
  readonly policyFileEnforces: boolean;
  readonly me: Me;
}): ReactElement {
  const [deleting, setDeleting] = useState(false);
  const navigate = useNavigate();
  const mutations = useAccessMutations();
  const writable = can(me, "policy_file");

  return (
    <>
      <RowMenu label={`Actions for rule ${rule.name}`}>
        <DropdownMenu.Item
          icon={PencilSimpleIcon}
          onClick={() => {
            void navigate({ to: "/policy/rules/$ruleId", params: { ruleId: rule.id } });
          }}
        >
          {writable ? "Edit" : "View"}
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item
          icon={TrashIcon}
          variant="danger"
          disabled={!writable}
          onClick={() => {
            setDeleting(true);
          }}
        >
          Delete…
        </DropdownMenu.Item>
      </RowMenu>
      <DeleteRuleDialog
        rule={rule}
        opensTailnet={
          rule.enabled &&
          !policyFileEnforces &&
          rules.filter((candidate) => candidate.enabled).length === 1
        }
        open={deleting}
        onOpenChange={setDeleting}
        mutations={mutations}
      />
    </>
  );
}
