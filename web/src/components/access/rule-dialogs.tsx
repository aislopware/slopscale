import { DeleteResource } from "@cloudflare/kumo";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { Switch } from "@cloudflare/kumo/components/switch";
import { useState } from "react";
import type { ReactElement, ReactNode, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { AccessRule, Group, Posture } from "~/api/queries.ts";
import { portsError, protocolLabel, protocols, toProtocol } from "~/components/access/model.ts";
import type { Protocol } from "~/components/access/model.ts";
import type { AccessMutations } from "~/components/access/mutations.ts";
import { groupItems, postureItems } from "~/components/access/pickers.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { toast } from "~/components/ui/toast.ts";

export interface RuleDialogProps {
  /** The rule to edit; absent when creating one. */
  readonly rule?: AccessRule | undefined;
  readonly groups: readonly Group[];
  /** Postures a rule may require of its sources. */
  readonly postures: readonly Posture[];
  /** Whether the policy file restricts traffic on its own, which changes what a rule means. */
  readonly policyFileEnforces: boolean;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
}

/** Creates a rule or edits one; the form mounts with the dialog so it starts from the record. */
export function RuleDialog(props: RuleDialogProps): ReactElement {
  const editing = props.rule !== undefined;

  return (
    <DialogRoot open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        size="lg"
        title={editing ? "Edit rule" : "New rule"}
        description={
          props.policyFileEnforces
            ? "Sources may open connections to destinations. A rule adds to what the policy file allows and cannot take any of it away."
            : "Sources may open connections to destinations. Once a rule is enabled, whatever no rule allows is blocked."
        }
      >
        <RuleForm {...props} />
      </DialogContent>
    </DialogRoot>
  );
}

interface Draft {
  readonly name: string;
  readonly description: string;
  readonly sources: readonly string[];
  readonly destinations: readonly string[];
  readonly postures: readonly string[];
  readonly protocol: Protocol;
  readonly ports: string;
  readonly bidirectional: boolean;
  readonly enabled: boolean;
}

function draftFrom(rule: AccessRule | undefined): Draft {
  return {
    name: rule?.name ?? "",
    description: rule?.description ?? "",
    sources: rule?.sourceGroupIds ?? [],
    destinations: rule?.destinationGroupIds ?? [],
    postures: rule?.postureIds ?? [],
    protocol: toProtocol(rule?.protocol ?? "all"),
    ports: rule?.ports ?? "",
    bidirectional: rule?.bidirectional ?? false,
    enabled: rule?.enabled ?? true,
  };
}

function hasPorts(protocol: Protocol): boolean {
  return protocol === "tcp" || protocol === "udp";
}

/** Why the draft cannot be saved yet, or null when it can. */
function draftIssue(draft: Draft): string | null {
  if (draft.name.trim() === "" || draft.sources.length === 0 || draft.destinations.length === 0) {
    return "incomplete";
  }

  return hasPorts(draft.protocol) ? portsError(draft.ports) : null;
}

function RuleForm({
  rule,
  groups,
  postures,
  onOpenChange,
  mutations,
}: Omit<RuleDialogProps, "open" | "policyFileEnforces">): ReactElement {
  const [draft, setDraft] = useState<Draft>(() => draftFrom(rule));
  const mutation = rule === undefined ? mutations.createRule : mutations.updateRule;
  const update = (patch: Partial<Draft>): void => {
    setDraft((current) => ({ ...current, ...patch }));
  };

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    const body = {
      name: draft.name.trim(),
      description: draft.description.trim(),
      enabled: draft.enabled,
      protocol: draft.protocol,
      ports: hasPorts(draft.protocol) ? draft.ports.trim() : "",
      bidirectional: draft.bidirectional,
      sourceGroupIds: [...draft.sources],
      destinationGroupIds: [...draft.destinations],
      postureIds: [...draft.postures],
    };
    const done = {
      onSuccess: (): void => {
        toast.success(rule === undefined ? "Rule created" : "Rule updated");
        onOpenChange(false);
      },
    };

    if (rule === undefined) {
      mutations.createRule.mutate({ body }, done);
    } else {
      mutations.updateRule.mutate({ params: { path: { id: rule.id } }, body }, done);
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <RuleFields draft={draft} groups={groups} postures={postures} onChange={update} />
      <DialogError message={mutation.isError ? errorMessage(mutation.error) : undefined} />
      <FormFooter
        label={rule === undefined ? "Create rule" : "Save"}
        pending={mutation.isPending}
        disabled={draftIssue(draft) !== null}
      />
    </form>
  );
}

function RuleFields({
  draft,
  groups,
  postures,
  onChange,
}: {
  readonly draft: Draft;
  readonly groups: readonly Group[];
  readonly postures: readonly Posture[];
  readonly onChange: (patch: Partial<Draft>) => void;
}): ReactElement {
  const items = groupItems(groups);

  return (
    <>
      <Input
        label="Name"
        value={draft.name}
        spellCheck={false}
        autoComplete="off"
        placeholder="Engineering to production"
        onChange={(event) => {
          onChange({ name: event.target.value });
        }}
      />
      <Input
        label="Description"
        required={false}
        value={draft.description}
        placeholder="SSH and HTTPS from engineers' laptops"
        onChange={(event) => {
          onChange({ description: event.target.value });
        }}
      />
      <MultiPicker
        label="Sources"
        placeholder="Groups that may open the connection…"
        items={items}
        value={draft.sources}
        onValueChange={(sources) => {
          onChange({ sources });
        }}
        empty="No group matches."
      />
      <MultiPicker
        label="Destinations"
        placeholder="Groups that accept it…"
        items={items}
        value={draft.destinations}
        onValueChange={(destinations) => {
          onChange({ destinations });
        }}
        empty="No group matches."
      />
      {postures.length === 0 ? null : (
        <MultiPicker
          label="Required postures"
          description="A source machine must satisfy at least one of them; none means any machine in the groups."
          placeholder="Postures the source must meet…"
          items={postureItems(postures)}
          value={draft.postures}
          onValueChange={(selected) => {
            onChange({ postures: selected });
          }}
          empty="No posture matches."
        />
      )}
      <ProtocolFields draft={draft} onChange={onChange} />
      <Switch.Group>
        <Switch.Legend>Options</Switch.Legend>
        <OptionSwitch
          label="Both directions"
          description="Destinations may open connections to sources too."
          checked={draft.bidirectional}
          onChange={(bidirectional) => {
            onChange({ bidirectional });
          }}
        />
        <OptionSwitch
          label="Enabled"
          description="A disabled rule is kept but not enforced."
          checked={draft.enabled}
          onChange={(enabled) => {
            onChange({ enabled });
          }}
        />
      </Switch.Group>
    </>
  );
}

function ProtocolFields({
  draft,
  onChange,
}: {
  readonly draft: Draft;
  readonly onChange: (patch: Partial<Draft>) => void;
}): ReactElement {
  const withPorts = hasPorts(draft.protocol);
  const issue = withPorts ? portsError(draft.ports) : null;

  return (
    <div className="grid items-start gap-4 sm:grid-cols-2">
      <Select
        className="w-full"
        label="Protocol"
        value={draft.protocol}
        onValueChange={(value) => {
          onChange({ protocol: toProtocol(value ?? "all") });
        }}
        renderValue={(value) => protocolLabel(value)}
      >
        {protocols.map((option) => (
          <Select.Option key={option} value={option}>
            {protocolLabel(option)}
          </Select.Option>
        ))}
      </Select>
      <Input
        label="Ports"
        required={false}
        disabled={!withPorts}
        value={withPorts ? draft.ports : ""}
        spellCheck={false}
        autoComplete="off"
        placeholder={withPorts ? "22, 443, 8000-8100" : "Every port"}
        description={withPorts ? "Empty means every port." : "Ports apply to TCP and UDP."}
        {...(issue === null ? {} : { error: issue })}
        onChange={(event) => {
          onChange({ ports: event.target.value });
        }}
      />
    </div>
  );
}

function OptionSwitch({
  label,
  description,
  checked,
  onChange,
}: {
  readonly label: string;
  readonly description: ReactNode;
  readonly checked: boolean;
  readonly onChange: (checked: boolean) => void;
}): ReactElement {
  return (
    <Switch
      checked={checked}
      onCheckedChange={onChange}
      label={
        <span className="flex flex-col gap-0.5">
          <span className="font-medium text-kumo-default">{label}</span>
          <span className="text-xs text-kumo-subtle">{description}</span>
        </span>
      }
    />
  );
}

export function DeleteRuleDialog({
  rule,
  opensTailnet,
  open,
  onOpenChange,
  mutations,
}: {
  readonly rule: AccessRule;
  /** Whether this is the last enabled rule with no restricting policy file behind it. */
  readonly opensTailnet: boolean;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
}): ReactElement {
  const { deleteRule } = mutations;
  const remove = (): void => {
    deleteRule.mutate(
      { params: { path: { id: rule.id } } },
      {
        onSuccess: () => {
          onOpenChange(false);
        },
      },
    );
  };

  // Deleting the record is the small part; opening the tailnet is what needs a clear yes.
  if (opensTailnet) {
    return (
      <ConfirmDialog
        open={open}
        onOpenChange={onOpenChange}
        title="Delete the last enabled rule?"
        description={`"${rule.name}" is the only enabled rule and the policy file restricts nothing. Without it every machine can reach every other machine.`}
        confirmLabel="Delete rule"
        loading={deleteRule.isPending}
        error={deleteRule.isError ? errorMessage(deleteRule.error) : undefined}
        onConfirm={remove}
      />
    );
  }

  return (
    <DeleteResource
      open={open}
      onOpenChange={onOpenChange}
      resourceType="rule"
      resourceName={rule.name}
      deleteButtonText="Delete rule"
      isDeleting={deleteRule.isPending}
      {...(deleteRule.isError ? { errorMessage: errorMessage(deleteRule.error) } : {})}
      onDelete={remove}
    />
  );
}
