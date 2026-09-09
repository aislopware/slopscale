import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { AccessRule, Group, Posture } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { DeleteRuleDialog, RuleDialog } from "~/components/access/rule-dialogs.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";

type Dialog = "edit" | "delete";

/** Edit and delete for one rule, gated by the policy scope. */
export function RuleMenu({
  rule,
  groups,
  postures,
  rules,
  policyFileEnforces,
  me,
}: {
  readonly rule: AccessRule;
  readonly groups: readonly Group[];
  readonly postures: readonly Posture[];
  readonly rules: readonly AccessRule[];
  readonly policyFileEnforces: boolean;
  readonly me: Me;
}): ReactElement {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const mutations = useAccessMutations();
  const writable = can(me, "policy_file");
  const close = (open: boolean): void => {
    if (!open) {
      setDialog(null);
    }
  };

  return (
    <>
      <RowMenu label={`Actions for rule ${rule.name}`}>
        <DropdownMenu.Item
          icon={PencilSimpleIcon}
          disabled={!writable}
          onClick={() => {
            setDialog("edit");
          }}
        >
          Edit…
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item
          icon={TrashIcon}
          variant="danger"
          disabled={!writable}
          onClick={() => {
            setDialog("delete");
          }}
        >
          Delete…
        </DropdownMenu.Item>
      </RowMenu>
      <RuleDialog
        rule={rule}
        groups={groups}
        postures={postures}
        policyFileEnforces={policyFileEnforces}
        open={dialog === "edit"}
        onOpenChange={close}
        mutations={mutations}
      />
      <DeleteRuleDialog
        rule={rule}
        opensTailnet={
          rule.enabled &&
          !policyFileEnforces &&
          rules.filter((candidate) => candidate.enabled).length === 1
        }
        open={dialog === "delete"}
        onOpenChange={close}
        mutations={mutations}
      />
    </>
  );
}
