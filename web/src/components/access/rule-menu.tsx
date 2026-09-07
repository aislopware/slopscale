import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { DotsThreeIcon, PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { AccessRule, Group } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { DeleteRuleDialog, RuleDialog } from "~/components/access/rule-dialogs.tsx";

const actionsIconSize = 18;

type Dialog = "edit" | "delete";

/** Edit and delete for one rule, gated by the policy scope. */
export function RuleMenu({
  rule,
  groups,
  rules,
  policyFileEnforces,
  me,
}: {
  readonly rule: AccessRule;
  readonly groups: readonly Group[];
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
      <DropdownMenu>
        <DropdownMenu.Trigger
          render={
            <Button
              variant="ghost"
              shape="square"
              size="sm"
              icon={<DotsThreeIcon size={actionsIconSize} weight="bold" />}
              aria-label={`Actions for rule ${rule.name}`}
            />
          }
        />
        <DropdownMenu.Content align="end">
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
        </DropdownMenu.Content>
      </DropdownMenu>
      <RuleDialog
        rule={rule}
        groups={groups}
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
