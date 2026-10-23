import { Input } from "@cloudflare/kumo/components/input";
import { useEffect, useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { DnsSettings } from "~/api/schema.gen.ts";
import { InlineRowEditor } from "~/components/dns/inline-row-editor.tsx";
import {
  domainError,
  nameserversError,
  normalizeDomain,
  parseList,
  withSplit,
  withoutSplit,
} from "~/components/dns/model.ts";
import type { DnsMutations } from "~/components/dns/mutations.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { DialogError } from "~/components/ui/dialog.tsx";
import { fieldError } from "~/components/ui/field-error.ts";
import { toast } from "~/components/ui/toast.ts";

/** The split DNS table's columns: domain, nameservers, exit node, and the Edit button. */
const columnCount = 4;

export interface Editing {
  readonly domain: string;
  readonly servers: readonly string[];
}

interface SplitDraft {
  readonly domain: string;
  readonly servers: string;
}

function draftFrom(editing: Editing | null): SplitDraft {
  return { domain: editing?.domain ?? "", servers: editing?.servers.join(", ") ?? "" };
}

interface SplitIssues {
  readonly domain: string | null;
  readonly servers: string | null;
}

/** What stops the draft from saving, per field; a domain that already has an entry counts. */
function splitIssues(
  settings: DnsSettings,
  editing: Editing | null,
  draft: SplitDraft,
): SplitIssues {
  const domain = normalizeDomain(draft.domain);
  const taken =
    editing?.domain !== domain && settings.splitNameservers[domain] !== undefined
      ? "This domain already has resolvers. Edit that entry instead."
      : null;

  return {
    domain: domainError(domain) ?? taken,
    servers: nameserversError(parseList(draft.servers)),
  };
}

/**
 * Holds a split DNS draft and decides which of its errors show: format errors once a field is left,
 * then live; an empty field only after a save was tried.
 */
function useSplitDraft(
  settings: DnsSettings,
  editing: Editing | null,
): {
  readonly draft: SplitDraft;
  readonly update: (patch: Partial<SplitDraft>) => void;
  readonly issues: SplitIssues;
  readonly shown: SplitIssues;
  readonly leave: (field: keyof SplitDraft) => void;
  readonly markSubmitted: () => void;
  readonly dirty: boolean;
} {
  const initial = draftFrom(editing);
  const [draft, setDraft] = useState(initial);
  const [left, setLeft] = useState<ReadonlySet<keyof SplitDraft>>(new Set());
  const [submitted, setSubmitted] = useState(false);
  const issues = splitIssues(settings, editing, draft);
  const show = (field: keyof SplitDraft): string | null =>
    submitted || (left.has(field) && draft[field].trim() !== "") ? issues[field] : null;

  return {
    draft,
    update: (patch) => {
      setDraft((current) => ({ ...current, ...patch }));
    },
    issues,
    shown: { domain: show("domain"), servers: show("servers") },
    leave: (field) => {
      setLeft((current) => new Set([...current, field]));
    },
    markSubmitted: () => {
      setSubmitted(true);
    },
    dirty: JSON.stringify(draft) !== JSON.stringify(initial),
  };
}

/** The settings with the draft saved over `editing`, or added when it is null. */
function savedSettings(
  settings: DnsSettings,
  editing: Editing | null,
  draft: SplitDraft,
): DnsSettings {
  return withSplit(settings, {
    domain: normalizeDomain(draft.domain),
    servers: parseList(draft.servers),
    previous: editing?.domain,
  });
}

/** The domain and its resolvers side by side, in the add dialog and the inline editor alike. */
function SplitFields({
  draft,
  shown,
  onChange,
  onLeave,
}: {
  readonly draft: SplitDraft;
  readonly shown: SplitIssues;
  readonly onChange: (patch: Partial<SplitDraft>) => void;
  readonly onLeave: (field: keyof SplitDraft) => void;
}): ReactElement {
  return (
    <div className="grid items-start gap-4 sm:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
      <Input
        className="w-full"
        label="Domain"
        value={draft.domain}
        placeholder="corp.example.com"
        spellCheck={false}
        autoComplete="off"
        {...fieldError(shown.domain ?? undefined)}
        onChange={(event) => {
          onChange({ domain: event.target.value });
        }}
        onBlur={() => {
          onLeave("domain");
        }}
      />
      <Input
        className="w-full font-mono"
        label="Nameservers"
        description="Separate several with commas. An IP, an IP with port, or a DNS-over-HTTPS URL."
        value={draft.servers}
        placeholder="10.0.0.53, 10.0.0.54"
        spellCheck={false}
        autoComplete="off"
        {...fieldError(shown.servers ?? undefined)}
        onChange={(event) => {
          onChange({ servers: event.target.value });
        }}
        onBlur={() => {
          onLeave("servers");
        }}
      />
    </div>
  );
}

export function SplitAddForm({
  settings,
  mutations,
  onDone,
}: {
  readonly settings: DnsSettings;
  readonly mutations: DnsMutations;
  readonly onDone: () => void;
}): ReactElement {
  const { draft, update, issues, shown, leave, markSubmitted } = useSplitDraft(settings, null);

  // A failure from the last form must not greet this one. Only `reset` goes in the dependencies:
  // the mutation object is rebuilt on every render and `reset` itself notifies, so depending on
  // the whole object ran the effect after every render it caused, without end.
  const { reset } = mutations.set;

  useEffect(() => {
    reset();
  }, [reset]);

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    markSubmitted();

    if (issues.domain !== null || issues.servers !== null) {
      return;
    }

    mutations.set.mutate(
      { body: savedSettings(settings, null, draft) },
      {
        onSuccess: () => {
          toast.success("Split DNS domain added");
          onDone();
        },
      },
    );
  }

  return (
    <form noValidate onSubmit={submit} className="flex flex-col gap-4">
      <SplitFields draft={draft} shown={shown} onChange={update} onLeave={leave} />
      <DialogError
        message={mutations.set.isError ? errorMessage(mutations.set.error) : undefined}
      />
      <FormFooter
        label="Save"
        pending={mutations.set.isPending}
        disabled={draft.domain.trim() === "" || draft.servers.trim() === ""}
      />
    </form>
  );
}

export function SplitInlineEditor({
  editing,
  settings,
  mutations,
  onClose,
  onDeleted,
}: {
  readonly editing: Editing;
  readonly settings: DnsSettings;
  readonly mutations: DnsMutations;
  readonly onClose: () => void;
  readonly onDeleted: () => void;
}): ReactElement {
  const { draft, update, issues, shown, leave, markSubmitted, dirty } = useSplitDraft(
    settings,
    editing,
  );
  const { reset } = mutations.set;

  useEffect(() => {
    reset();
  }, [reset]);

  return (
    <InlineRowEditor
      columns={columnCount}
      label={`Edit ${editing.domain}`}
      dirty={dirty}
      pending={mutations.set.isPending}
      error={mutations.set.isError ? errorMessage(mutations.set.error) : undefined}
      onSave={() => {
        markSubmitted();

        if (issues.domain !== null || issues.servers !== null) {
          return;
        }

        mutations.set.mutate(
          { body: savedSettings(settings, editing, draft) },
          {
            onSuccess: () => {
              toast.success("Split DNS domain saved");
              onClose();
            },
          },
        );
      }}
      onCancel={onClose}
      onDelete={() => {
        mutations.apply(withoutSplit(settings, editing.domain), `Removed ${editing.domain}`);
        onDeleted();
      }}
    >
      <SplitFields
        draft={draft}
        shown={shown}
        onChange={(patch) => {
          update(patch);

          if (mutations.set.isError) {
            reset();
          }
        }}
        onLeave={leave}
      />
    </InlineRowEditor>
  );
}
