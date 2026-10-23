import { Input } from "@cloudflare/kumo/components/input";
import { useState } from "react";
import type { ReactElement } from "react";

import type { OAuthClient } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { ClaimRulesEditor } from "~/components/keys/claim-rules-editor.tsx";
import {
  recordToClaimRules,
  subjectHint,
  validateClaimRules,
  validateIssuerUrl,
} from "~/components/keys/federated.ts";
import type { ClaimRuleEntry } from "~/components/keys/federated.ts";
import { needsTags, scopeItemsWithExisting } from "~/components/keys/scopes.ts";
import { useKnownTags } from "~/components/tags/use-known-tags.ts";
import { fieldError } from "~/components/ui/field-error.ts";
import { FieldGrid, FormSection, nameFieldClass } from "~/components/ui/form-page.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { TagField } from "~/components/ui/tag-field.tsx";

/** What the federated identity and OAuth client pages edit; a plain client leaves the trust empty. */
export interface OAuthDraft {
  readonly description: string;
  readonly issuer: string;
  readonly audience: string;
  readonly subject: string;
  readonly rules: readonly ClaimRuleEntry[];
  readonly scopes: readonly string[];
  readonly tags: readonly string[];
}

export type OAuthDraftChange = (patch: Partial<OAuthDraft>) => void;

/** The draft for a client as it stands, or an empty identity trusting `audience` by default. */
export function oauthDraftFrom(client: OAuthClient | undefined, audience = ""): OAuthDraft {
  return {
    description: client?.description ?? "",
    issuer: client?.issuer ?? "",
    audience: client?.audience ?? audience,
    subject: client?.subject ?? "",
    rules: recordToClaimRules(client?.customClaimRules),
    scopes: client?.scopes ?? [],
    tags: client?.tags ?? [],
  };
}

export interface OAuthDraftIssues {
  readonly issuer?: string;
  readonly audience?: string;
  readonly subject?: string;
  readonly rules?: string;
  readonly scopes?: string;
  readonly tags?: string;
}

/** What is wrong with each field, or nothing; the pages show them after the first save. */
export function oauthDraftIssues(draft: OAuthDraft, federated: boolean): OAuthDraftIssues {
  const access = {
    ...(draft.scopes.length === 0 ? { scopes: "Pick at least one scope." } : {}),
    ...(needsTags(draft.scopes) && draft.tags.length === 0
      ? { tags: "Add a tag: the machines and keys the tokens create are owned by it." }
      : {}),
  };

  if (!federated) {
    return access;
  }

  const issuer = validateIssuerUrl(draft.issuer);
  const rules = validateClaimRules(draft.rules);

  return {
    ...(issuer === undefined ? {} : { issuer }),
    ...(draft.audience.trim() === "" ? { audience: "Enter the audience the token carries." } : {}),
    ...(draft.subject.trim() === "" ? { subject: "Enter the subject the token carries." } : {}),
    ...(rules === undefined ? {} : { rules }),
    ...access,
  };
}

/** The description, the one free-text name these records have. */
export function DescriptionSection({
  title,
  placeholder,
  value,
  onChange,
}: {
  readonly title: string;
  readonly placeholder: string;
  readonly value: string;
  readonly onChange: OAuthDraftChange;
}): ReactElement {
  return (
    <FormSection title={title} optional info="What it is for, so it can be told apart in the list.">
      <Input
        aria-label={title}
        className={nameFieldClass}
        value={value}
        placeholder={placeholder}
        onChange={(event) => {
          onChange({ description: event.target.value });
        }}
      />
    </FormSection>
  );
}

/**
 * Who is trusted: the issuer and audience side by side, the subject under them and any further
 * claims. The issuer says what is wrong with its format once it is left; an empty field waits for
 * the save.
 */
export function TrustSection({
  draft,
  issues,
  defaultAudience,
  onChange,
}: {
  readonly draft: OAuthDraft;
  /** Shown after a save that they stopped. */
  readonly issues: OAuthDraftIssues;
  readonly defaultAudience: string;
  readonly onChange: OAuthDraftChange;
}): ReactElement {
  const [left, setLeft] = useState(false);
  const format = left && draft.issuer.trim() !== "" ? validateIssuerUrl(draft.issuer) : undefined;

  return (
    <FormSection
      title="Trust conditions"
      info="A workload's OIDC token must come from this issuer and carry this audience and subject before the server trades it for an API token."
    >
      <FieldGrid>
        <Input
          label="Issuer"
          type="url"
          value={draft.issuer}
          spellCheck={false}
          placeholder="https://token.actions.githubusercontent.com"
          description="The https URL of the OIDC provider that signs workload tokens."
          {...fieldError(issues.issuer ?? format)}
          onChange={(event) => {
            onChange({ issuer: event.target.value });
          }}
          onBlur={() => {
            setLeft(true);
          }}
        />
        <Input
          label="Audience"
          value={draft.audience}
          spellCheck={false}
          placeholder={defaultAudience || "https://scale.example.com"}
          description="The audience the token must carry. The server URL is the usual choice."
          {...fieldError(issues.audience)}
          onChange={(event) => {
            onChange({ audience: event.target.value });
          }}
        />
      </FieldGrid>
      <Input
        label="Subject"
        value={draft.subject}
        spellCheck={false}
        placeholder="repo:org/repo:ref:refs/heads/main"
        description={subjectHint(draft.issuer)}
        {...fieldError(issues.subject)}
        onChange={(event) => {
          onChange({ subject: event.target.value });
        }}
      />
      <ClaimRulesEditor
        entries={draft.rules}
        error={issues.rules}
        onChange={(rules) => {
          onChange({ rules });
        }}
      />
    </FormSection>
  );
}

const noScopes: readonly string[] = [];

/** The scopes the tokens carry and the tags they may hand out, a section each. */
export function AccessSections({
  me,
  draft,
  existingScopes = noScopes,
  issues,
  onChange,
}: {
  readonly me: Me;
  readonly draft: OAuthDraft;
  /** Scopes the record already has, kept pickable even when the caller does not hold them. */
  readonly existingScopes?: readonly string[];
  readonly issues: OAuthDraftIssues;
  readonly onChange: OAuthDraftChange;
}): ReactElement {
  const knownTags = useKnownTags(me);
  const tagged = needsTags(draft.scopes);

  return (
    <>
      <FormSection
        title="Scopes"
        info="What the tokens may do. You can only grant scopes you hold."
      >
        <MultiPicker
          label="Scopes"
          description="Pick at least one."
          placeholder="Pick scopes"
          items={scopeItemsWithExisting(me, existingScopes)}
          value={draft.scopes}
          onValueChange={(scopes) => {
            onChange({ scopes });
          }}
          empty="No scope matches."
          error={issues.scopes}
        />
      </FormSection>
      <FormSection
        title="Tags"
        optional={!tagged}
        info="Machines and pre-auth keys the tokens create are owned by these tags."
      >
        <TagField
          required={tagged}
          description={
            tagged
              ? "Machine and pre-auth key scopes need tags. Everything the tokens create is owned by them."
              : "Tags the tokens may put on the machines and keys they create."
          }
          placeholder="tag:ci"
          value={draft.tags}
          suggestions={knownTags}
          onValueChange={(tags) => {
            onChange({ tags });
          }}
          error={issues.tags}
        />
      </FormSection>
    </>
  );
}
