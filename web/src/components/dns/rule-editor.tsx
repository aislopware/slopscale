import { Input, Textarea } from "@cloudflare/kumo/components/input";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { DnsRule, Group } from "~/api/queries.ts";
import type { DnsRuleRequestBody } from "~/api/schema.gen.ts";
import { groupItems } from "~/components/access/pickers.ts";
import {
  domainError,
  nameserversError,
  normalizeDomain,
  parseList,
} from "~/components/dns/model.ts";
import { DeleteDnsRuleDialog } from "~/components/dns/rule-delete-dialog.tsx";
import { useDnsRuleMutations } from "~/components/dns/rule-mutations.ts";
import { SettingToggle } from "~/components/policy/rule-builder.tsx";
import { fieldError } from "~/components/ui/field-error.ts";
import { FormPage, FormSection, nameFieldClass, useDraft } from "~/components/ui/form-page.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { toast } from "~/components/ui/toast.ts";

export const rulesListPath = "/dns/split";

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

interface Issues {
  readonly name?: string;
  readonly domains?: string;
  readonly nameservers?: string;
  readonly groups?: string;
}

/** What is wrong with each field, or nothing; the page shows them after the first save. */
function draftIssues(draft: Draft): Issues {
  const domains = domainsError(parseList(draft.domains));
  const servers = nameserversError(parseList(draft.nameservers));

  return {
    ...(draft.name.trim() === "" ? { name: "Give the rule a name." } : {}),
    ...(domains === null ? {} : { domains }),
    ...(servers === null ? {} : { nameservers: servers }),
    ...(draft.groups.length === 0 ? { groups: "Pick the groups that get the rule." } : {}),
  };
}

function bodyFrom(draft: Draft): DnsRuleRequestBody {
  return {
    name: draft.name.trim(),
    description: draft.description.trim(),
    enabled: draft.enabled,
    domains: parseList(draft.domains).map((domain) => normalizeDomain(domain)),
    nameservers: parseList(draft.nameservers),
    groupIds: [...draft.groups],
  };
}

export interface DnsRuleEditorProps {
  /** The rule to edit; absent when creating one. */
  readonly rule?: DnsRule | undefined;
  readonly groups: readonly Group[];
  readonly canEdit: boolean;
}

/**
 * A group DNS rule on a page of its own, for creating one or editing it: what it is called, the
 * domains and the nameservers that answer them, and the groups that get it. Saving goes back to the
 * Split DNS page.
 */
export function DnsRuleEditor({ rule, groups, canEdit }: DnsRuleEditorProps): ReactElement {
  const navigate = useNavigate();
  const mutations = useDnsRuleMutations();
  const mutation = rule === undefined ? mutations.create : mutations.update;
  const { draft, dirty, submitted, update, markSubmitted } = useDraft(draftFrom(rule), mutation);
  const [deleting, setDeleting] = useState(false);
  const issues = draftIssues(draft);
  const shown = submitted ? issues : {};

  const toList = (): void => {
    void navigate({ to: rulesListPath, ignoreBlocker: true });
  };

  function save(): boolean {
    markSubmitted();

    if (Object.keys(issues).length > 0) {
      return false;
    }

    const body = bodyFrom(draft);
    const done = {
      onSuccess: (): void => {
        toast.success(rule === undefined ? "DNS rule created" : "DNS rule saved");
        toList();
      },
    };

    if (rule === undefined) {
      mutations.create.mutate({ body }, done);
    } else {
      mutations.update.mutate({ params: { path: { id: rule.id } }, body }, done);
    }

    return true;
  }

  return (
    <FormPage
      noun="DNS rule"
      creating={rule === undefined}
      canEdit={canEdit}
      error={mutation.isError ? errorMessage(mutation.error) : undefined}
      errorTitle={
        rule === undefined ? "The DNS rule was not created" : "The DNS rule was not saved"
      }
      pending={mutation.isPending}
      dirty={dirty}
      onSubmit={save}
      cancelTo={rulesListPath}
      onDelete={() => {
        setDeleting(true);
      }}
      dialog={
        rule === undefined ? null : (
          <DeleteDnsRuleDialog
            rule={rule}
            open={deleting}
            onOpenChange={setDeleting}
            mutations={mutations}
            onDeleted={toList}
          />
        )
      }
    >
      <DetailsSection draft={draft} issue={shown.name} onChange={update} />
      <ResolutionSection draft={draft} issues={shown} onChange={update} />
      <FormSection
        title="Access"
        info="Only the machines in these groups get the rule. Every other machine resolves the domains as before."
      >
        <MultiPicker
          label="Groups"
          description="Pick the built-in All group for everyone."
          placeholder="Search groups…"
          items={groupItems(groups)}
          value={draft.groups}
          error={shown.groups}
          onValueChange={(chosen) => {
            update({ groups: chosen });
          }}
          empty="No group matches."
        />
      </FormSection>
      <FormSection title="Status">
        <SettingToggle
          title="Enabled"
          description="A disabled rule keeps its settings but reaches no machine."
          checked={draft.enabled}
          onCheckedChange={(enabled) => {
            update({ enabled });
          }}
        />
      </FormSection>
    </FormPage>
  );
}

type Change = (patch: Partial<Draft>) => void;

function DetailsSection({
  draft,
  issue,
  onChange,
}: {
  readonly draft: Draft;
  readonly issue: string | undefined;
  readonly onChange: Change;
}): ReactElement {
  return (
    <FormSection title="Rule details">
      <Input
        label="Name"
        className={nameFieldClass}
        value={draft.name}
        spellCheck={false}
        autoComplete="off"
        placeholder="Corp DNS"
        {...fieldError(issue)}
        onChange={(event) => {
          onChange({ name: event.target.value });
        }}
      />
      <Input
        label="Description"
        required={false}
        value={draft.description}
        placeholder="Internal zones for the engineering VPN"
        onChange={(event) => {
          onChange({ description: event.target.value });
        }}
      />
    </FormSection>
  );
}

/**
 * The domains and their nameservers say what is wrong with them as soon as the field is left, like
 * the dashboard's format checks; an empty list waits for the save.
 */
function ResolutionSection({
  draft,
  issues,
  onChange,
}: {
  readonly draft: Draft;
  readonly issues: Issues;
  readonly onChange: Change;
}): ReactElement {
  const [left, setLeft] = useState({ domains: false, nameservers: false });
  const domains = parseList(draft.domains);
  const servers = parseList(draft.nameservers);
  const domainFormat = left.domains && domains.length > 0 ? domainsError(domains) : null;
  const serverFormat = left.nameservers && servers.length > 0 ? nameserversError(servers) : null;

  return (
    <FormSection
      title="Resolution"
      info="Queries for a domain and everything under it go to these nameservers, and nowhere else."
    >
      <Textarea
        label="Domains"
        description="One per line."
        value={draft.domains}
        placeholder={"corp.example.com\ninternal.example.com"}
        spellCheck={false}
        autoResize
        minRows={2}
        maxRows={8}
        className="font-mono"
        {...fieldError(issues.domains ?? domainFormat ?? undefined)}
        onChange={(event) => {
          onChange({ domains: event.target.value });
        }}
        onBlur={() => {
          setLeft((current) => ({ ...current, domains: true }));
        }}
      />
      <Textarea
        label="Nameservers"
        description="One per line. An IP, an IP with port, or a DNS-over-HTTPS URL."
        value={draft.nameservers}
        placeholder={"10.0.0.53\n10.0.0.54"}
        spellCheck={false}
        autoResize
        minRows={2}
        maxRows={8}
        className="font-mono"
        {...fieldError(issues.nameservers ?? serverFormat ?? undefined)}
        onChange={(event) => {
          onChange({ nameservers: event.target.value });
        }}
        onBlur={() => {
          setLeft((current) => ({ ...current, nameservers: true }));
        }}
      />
    </FormSection>
  );
}
