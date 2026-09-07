import { Switch } from "@cloudflare/kumo/components/switch";
import { ArrowRightIcon, ArrowsLeftRightIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { AccessRule, Group } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { GroupChips } from "~/components/access/group-chips.tsx";
import { groupName, protocolSummary } from "~/components/access/model.ts";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { RuleMenu } from "~/components/access/rule-menu.tsx";
import { createAppColumnHelper, useTableContext } from "~/components/table/app-table.tsx";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { toast } from "~/components/ui/toast.ts";

/** A rule with its group names spelled out, so the global filter can match them. */
export interface RuleRow extends AccessRule {
  readonly sourceNames: string;
  readonly destinationNames: string;
}

export function toRuleRows(rules: readonly AccessRule[], groups: readonly Group[]): RuleRow[] {
  return rules.map((rule) => ({
    ...rule,
    sourceNames: names(rule.sourceGroupIds, groups),
    destinationNames: names(rule.destinationGroupIds, groups),
  }));
}

function names(ids: readonly string[], groups: readonly Group[]): string {
  return ids.map((id) => groupName(groups, id)).join(", ");
}

const helper = createAppColumnHelper<RuleRow>();

const directionIconSize = 16;

export const ruleColumns = helper.columns([
  helper.accessor((rule) => `${rule.name} ${rule.description}`, {
    id: "name",
    header: "Rule",
    enableSorting: true,
    cell: ({ row }) => <NameCell rule={row.original} />,
    meta: { className: "w-[26%] min-w-44" },
  }),
  helper.accessor((rule) => rule.sourceNames, {
    id: "sources",
    header: "Sources",
    enableSorting: false,
    cell: ({ row, table }) => (
      <GroupChips ids={row.original.sourceGroupIds} groups={table.options.meta?.groups ?? []} />
    ),
    meta: { className: "min-w-32" },
  }),
  helper.accessor((rule) => (rule.bidirectional ? "both ways" : "one way"), {
    id: "direction",
    header: "",
    enableSorting: false,
    enableGlobalFilter: false,
    cell: ({ row }) => <DirectionCell bidirectional={row.original.bidirectional} />,
    meta: { className: "w-10 px-0 text-center" },
  }),
  helper.accessor((rule) => rule.destinationNames, {
    id: "destinations",
    header: "Destinations",
    enableSorting: false,
    cell: ({ row, table }) => (
      <GroupChips
        ids={row.original.destinationGroupIds}
        groups={table.options.meta?.groups ?? []}
      />
    ),
    meta: { className: "min-w-32" },
  }),
  helper.accessor((rule) => protocolSummary(rule), {
    id: "protocol",
    header: "Protocol & ports",
    enableSorting: true,
    cell: ({ row }) => <ProtocolCell rule={row.original} />,
    meta: { className: "hidden whitespace-nowrap md:table-cell" },
  }),
  helper.accessor((rule) => (rule.enabled ? 1 : 0), {
    id: "enabled",
    header: "Enabled",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row, table }) => {
      const { me } = table.options.meta ?? {};

      return me === undefined ? null : <EnabledCell rule={row.original} me={me} />;
    },
    meta: { className: "whitespace-nowrap" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const { me, groups, rules, policyFileEnforces } = table.options.meta ?? {};

      return me === undefined ? null : (
        <RuleMenu
          rule={row.original}
          groups={groups ?? []}
          rules={rules ?? []}
          policyFileEnforces={policyFileEnforces === true}
          me={me}
        />
      );
    },
    meta: { className: "w-12 text-right" },
  }),
]);

function NameCell({ rule }: { readonly rule: AccessRule }): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="truncate font-medium text-kumo-default">{rule.name}</span>
      {rule.description === "" ? null : (
        <span className="truncate text-xs text-kumo-subtle">{rule.description}</span>
      )}
      {/* The protocol column is hidden on small screens, so the name carries it there. */}
      <span className="truncate text-xs text-kumo-subtle md:hidden">{protocolSummary(rule)}</span>
    </div>
  );
}

function DirectionCell({ bidirectional }: { readonly bidirectional: boolean }): ReactElement {
  const label = bidirectional ? "Both directions" : "Sources to destinations";

  return (
    <span className="inline-flex text-kumo-subtle" title={label} aria-label={label}>
      {bidirectional ? (
        <ArrowsLeftRightIcon size={directionIconSize} />
      ) : (
        <ArrowRightIcon size={directionIconSize} />
      )}
    </span>
  );
}

function ProtocolCell({ rule }: { readonly rule: AccessRule }): ReactElement {
  return <span className="text-kumo-subtle">{protocolSummary(rule)}</span>;
}

/**
 * An inline switch on PATCH, so only the flag travels and an edit made elsewhere is never
 * overwritten. Disabling the last enabled rule opens the tailnet when no policy file restricts it,
 * and that asks first.
 */
function EnabledCell({ rule, me }: { readonly rule: AccessRule; readonly me: Me }): ReactElement {
  const { setRuleEnabled } = useAccessMutations();
  const { rules, policyFileEnforces } = useTableContext().options.meta ?? {};
  const [confirming, setConfirming] = useState(false);
  const lastEnabled =
    rule.enabled && (rules ?? []).filter((candidate) => candidate.enabled).length === 1;
  const opensTailnet = lastEnabled && policyFileEnforces !== true;

  const apply = (enabled: boolean): void => {
    setRuleEnabled.mutate(
      { params: { path: { id: rule.id } }, body: { enabled } },
      {
        onSuccess: () => {
          setConfirming(false);
          toast.success(enabled ? "Rule enabled" : "Rule disabled");
        },
        onError: (error) => {
          toast.error(errorMessage(error));
        },
      },
    );
  };

  return (
    <>
      <Switch
        size="sm"
        aria-label={`${rule.name} enabled`}
        checked={rule.enabled}
        disabled={!can(me, "policy_file") || setRuleEnabled.isPending}
        transitioning={setRuleEnabled.isPending}
        onCheckedChange={(enabled) => {
          if (!enabled && opensTailnet) {
            setConfirming(true);
          } else {
            apply(enabled);
          }
        }}
      />
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Disable the last rule?"
        description="No other rule is enabled and the policy file restricts nothing, so every machine will be able to reach every other machine."
        confirmLabel="Disable rule"
        loading={setRuleEnabled.isPending}
        error={setRuleEnabled.isError ? errorMessage(setRuleEnabled.error) : undefined}
        onConfirm={() => {
          apply(false);
        }}
      />
    </>
  );
}
