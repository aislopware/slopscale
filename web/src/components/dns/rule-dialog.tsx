import { Input, Textarea } from "@cloudflare/kumo/components/input";
import { Switch } from "@cloudflare/kumo/components/switch";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { DnsRule, Group } from "~/api/queries.ts";
import { groupItems } from "~/components/access/pickers.ts";
import {
  domainError,
  nameserversError,
  normalizeDomain,
  parseList,
} from "~/components/dns/model.ts";
import type { DnsRuleMutations } from "~/components/dns/rule-mutations.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { toast } from "~/components/ui/toast.ts";

export interface DnsRuleDialogProps {
  /** The rule to edit; absent when creating one. */
  readonly rule?: DnsRule | undefined;
  readonly groups: readonly Group[];
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: DnsRuleMutations;
}

/**
 * Creates a group DNS rule or edits one; the form mounts with the dialog so it starts from the
 * record.
 */
export function DnsRuleDialog(props: DnsRuleDialogProps): ReactElement {
  const editing = props.rule !== undefined;

  return (
    <DialogRoot open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        size="base"
        title={editing ? "Edit DNS rule" : "New DNS rule"}
        description="Queries for the domains go to these nameservers on the machines in the groups, and nowhere else."
      >
        <DnsRuleForm {...props} />
      </DialogContent>
    </DialogRoot>
  );
}

interface Draft {
  readonly name: string;
  readonly description: string;
  readonly domains: string;
  readonly nameservers: string;
  readonly groups: readonly string[];
  readonly enabled: boolean;
}

function draftFrom(rule: DnsRule | undefined): Draft {
  return {
    name: rule?.name ?? "",
    description: rule?.description ?? "",
    domains: (rule?.domains ?? []).join("\n"),
    nameservers: (rule?.nameservers ?? []).join("\n"),
    groups: rule?.groupIds ?? [],
    enabled: rule?.enabled ?? true,
  };
}

/** The first thing wrong with the domains, or null. */
function domainsError(domains: readonly string[]): string | null {
  if (domains.length === 0) {
    return "Enter at least one domain.";
  }

  for (const domain of domains) {
    const issue = domainError(normalizeDomain(domain));

    if (issue !== null) {
      return issue;
    }
  }

  return null;
}

/** Why the draft cannot be saved yet, or null when it can. */
function draftIssue(draft: Draft): string | null {
  if (draft.name.trim() === "" || draft.groups.length === 0) {
    return "incomplete";
  }

  return domainsError(parseList(draft.domains)) ?? nameserversError(parseList(draft.nameservers));
}

function DnsRuleForm({
  rule,
  groups,
  onOpenChange,
  mutations,
}: Omit<DnsRuleDialogProps, "open">): ReactElement {
  const [draft, setDraft] = useState<Draft>(() => draftFrom(rule));
  const [touched, setTouched] = useState(false);
  const mutation = rule === undefined ? mutations.create : mutations.update;
  const domainIssue = domainsError(parseList(draft.domains));
  const serverIssue = nameserversError(parseList(draft.nameservers));
  const update = (patch: Partial<Draft>): void => {
    setDraft((current) => ({ ...current, ...patch }));
  };

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    const body = {
      name: draft.name.trim(),
      description: draft.description.trim(),
      enabled: draft.enabled,
      domains: parseList(draft.domains).map((domain) => normalizeDomain(domain)),
      nameservers: parseList(draft.nameservers),
      groupIds: [...draft.groups],
    };
    const done = {
      onSuccess: (): void => {
        toast.success(rule === undefined ? "DNS rule created" : "DNS rule updated");
        onOpenChange(false);
      },
    };

    if (rule === undefined) {
      mutations.create.mutate({ body }, done);
    } else {
      mutations.update.mutate({ params: { path: { id: rule.id } }, body }, done);
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Name"
        value={draft.name}
        spellCheck={false}
        autoComplete="off"
        placeholder="Corp DNS"
        onChange={(event) => {
          update({ name: event.target.value });
        }}
      />
      <Textarea
        label="Domains"
        description="One per line. Queries for a domain and everything under it go to the nameservers."
        value={draft.domains}
        placeholder={"corp.example.com\ninternal.example.com"}
        spellCheck={false}
        autoResize
        minRows={2}
        maxRows={6}
        onChange={(event) => {
          update({ domains: event.target.value });
        }}
        onBlur={() => {
          setTouched(true);
        }}
        {...(touched && domainIssue !== null ? { error: domainIssue } : {})}
      />
      <Textarea
        label="Nameservers"
        description="One per line: an IP, an IP with port, or a known provider's DNS-over-HTTPS URL."
        value={draft.nameservers}
        placeholder={"10.0.0.53\n10.0.0.54"}
        spellCheck={false}
        autoResize
        minRows={2}
        maxRows={6}
        onChange={(event) => {
          update({ nameservers: event.target.value });
        }}
        onBlur={() => {
          setTouched(true);
        }}
        {...(touched && domainIssue === null && serverIssue !== null ? { error: serverIssue } : {})}
      />
      <MultiPicker
        label="Groups"
        description="Only the machines in these groups get the rule."
        placeholder="Groups that receive the rule…"
        items={groupItems(groups)}
        value={draft.groups}
        onValueChange={(chosen) => {
          update({ groups: chosen });
        }}
        empty="No group matches."
      />
      <Switch
        checked={draft.enabled}
        onCheckedChange={(enabled) => {
          update({ enabled });
        }}
        label={
          <span className="flex flex-col gap-0.5">
            <span className="font-medium text-kumo-default">Enabled</span>
            <span className="text-xs text-kumo-subtle">
              A disabled rule keeps its settings but is handed to nobody.
            </span>
          </span>
        }
      />
      <DialogError message={mutation.isError ? errorMessage(mutation.error) : undefined} />
      <FormFooter
        label={rule === undefined ? "Create rule" : "Save"}
        pending={mutation.isPending}
        disabled={draftIssue(draft) !== null}
      />
    </form>
  );
}
