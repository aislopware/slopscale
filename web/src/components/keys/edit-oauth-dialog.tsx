import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import type { ReactElement, SubmitEvent } from "react";
import { useState } from "react";

import { errorMessage } from "~/api/error.ts";
import type { OAuthClient } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { FederatedIdentityFields } from "~/components/keys/federated-dialogs.tsx";
import {
  claimRulesToRecord,
  diffOAuthClient,
  recordToClaimRules,
  validateClaimRules,
  validateIssuerUrl,
} from "~/components/keys/federated.ts";
import type { ClaimRuleEntry } from "~/components/keys/federated.ts";
import { useOAuthClientMutations } from "~/components/keys/mutations.ts";
import { needsTags, scopeItemsWithExisting } from "~/components/keys/scopes.ts";
import { useKnownTags } from "~/components/tags/use-known-tags.ts";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { TagField } from "~/components/ui/tag-field.tsx";
import { toast } from "~/components/ui/toast.ts";
import { useBaseline } from "~/lib/use-baseline.ts";

export function EditOAuthClientDialog({
  client,
  me,
  open,
  onOpenChange,
}: {
  readonly client: OAuthClient;
  readonly me?: Me | undefined;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  const isFederated = client.keyType === "federated";
  const title = isFederated ? "Edit federated identity" : "Edit OAuth client";
  const description = isFederated
    ? "Update the trust conditions, scopes or tags for this federated identity."
    : "Update the description, scopes or tags for this OAuth client.";

  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent size="lg" title={title} description={description}>
        {/* Keyed by the client alone: Base UI unmounts the body on close, so every open starts from
            the record as it is then, and nothing remounts the form while it is up. */}
        <EditOAuthClientForm
          key={client.clientId}
          client={client}
          me={me}
          onDone={() => {
            onOpenChange(false);
          }}
        />
      </DialogContent>
    </DialogRoot>
  );
}

function canSubmitEdit({
  dirty,
  scopes,
  tags,
  isFederated,
  issuerError,
  rulesError,
  subject,
  audience,
}: {
  readonly dirty: boolean;
  readonly scopes: readonly string[];
  readonly tags: readonly string[];
  readonly isFederated: boolean;
  readonly issuerError?: string | undefined;
  readonly rulesError?: string | undefined;
  readonly subject: string;
  readonly audience: string;
}): boolean {
  if (!dirty || scopes.length === 0) {
    return false;
  }
  if (needsTags(scopes) && tags.length === 0) {
    return false;
  }
  if (isFederated) {
    return (
      issuerError === undefined &&
      rulesError === undefined &&
      subject.trim() !== "" &&
      audience.trim() !== ""
    );
  }
  return true;
}

function ScopesAndTagsFields({
  me,
  scopes,
  tags,
  existingScopes,
  onScopesChange,
  onTagsChange,
}: {
  readonly me?: Me | undefined;
  readonly scopes: readonly string[];
  readonly tags: readonly string[];
  readonly existingScopes: readonly string[];
  readonly onScopesChange: (scopes: string[]) => void;
  readonly onTagsChange: (tags: string[]) => void;
}): ReactElement {
  const knownTags = useKnownTags(me);

  return (
    <>
      <MultiPicker
        label="Scopes"
        description="What the tokens may do. You can only grant scopes you hold."
        placeholder="Pick scopes"
        items={scopeItemsWithExisting(me, existingScopes)}
        value={scopes}
        onValueChange={onScopesChange}
        empty="No scope matches."
      />
      <TagField
        required={needsTags(scopes)}
        description={
          needsTags(scopes)
            ? "Machine and pre-auth key scopes need tags. Everything created is owned by them."
            : "Tags the tokens may put on the machines and keys created."
        }
        placeholder="tag:ci"
        value={tags}
        suggestions={knownTags}
        onValueChange={onTagsChange}
      />
    </>
  );
}

function EditOAuthClientForm({
  client,
  me,
  onDone,
}: {
  readonly client: OAuthClient;
  readonly me?: Me | undefined;
  readonly onDone: () => void;
}): ReactElement {
  const { update } = useOAuthClientMutations();
  // The record the form opened on, kept as it was. The list behind the dialog refetches, and
  // diffing against a fresher record would turn a field nobody touched into a change to send.
  const baseline = useBaseline(client, client.clientId);
  const isFederated = baseline.keyType === "federated";

  const [description, setDescription] = useState(baseline.description);
  const [scopes, setScopes] = useState<readonly string[]>(baseline.scopes);
  const [tags, setTags] = useState<readonly string[]>(baseline.tags);
  const [issuer, setIssuer] = useState(baseline.issuer);
  const [audience, setAudience] = useState(baseline.audience);
  const [subject, setSubject] = useState(baseline.subject);
  const [rules, setRules] = useState<readonly ClaimRuleEntry[]>(() =>
    recordToClaimRules(baseline.customClaimRules),
  );

  const issuerError = isFederated ? validateIssuerUrl(issuer) : undefined;
  const rulesError = isFederated ? validateClaimRules(rules) : undefined;

  const diff = diffOAuthClient(baseline, {
    description,
    scopes,
    tags,
    issuer,
    audience,
    subject,
    customClaimRules: claimRulesToRecord(rules),
  });
  const dirty = Object.keys(diff).length > 0;

  const canSubmit = canSubmitEdit({
    dirty,
    scopes,
    tags,
    isFederated,
    issuerError,
    rulesError,
    subject,
    audience,
  });

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    // Enter in a text input submits the form whatever the buttons say, so the guard is here too.
    if (!canSubmit || update.isPending) {
      return;
    }

    update.mutate(
      {
        params: { path: { clientId: baseline.clientId } },
        body: diff,
      },
      {
        onSuccess: () => {
          toast.success(isFederated ? "Federated identity updated" : "OAuth client updated");
          onDone();
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Description"
        required={false}
        value={description}
        placeholder="Terraform, CI deploys…"
        onChange={(event) => {
          setDescription(event.target.value);
        }}
      />
      {isFederated ? (
        <FederatedIdentityFields
          issuer={issuer}
          audience={audience}
          subject={subject}
          rules={rules}
          issuerError={issuerError}
          rulesError={rulesError}
          defaultAudience={baseline.audience}
          onIssuerChange={setIssuer}
          onAudienceChange={setAudience}
          onSubjectChange={setSubject}
          onRulesChange={setRules}
        />
      ) : null}
      <ScopesAndTagsFields
        me={me}
        scopes={scopes}
        tags={tags}
        existingScopes={baseline.scopes}
        onScopesChange={setScopes}
        onTagsChange={setTags}
      />
      <DialogError message={update.isError ? errorMessage(update.error) : undefined} />
      <DialogFooter submitDisabled={!canSubmit || update.isPending}>
        <DialogClose render={<Button variant="secondary">Cancel</Button>} />
        <Button type="submit" variant="primary" disabled={!canSubmit} loading={update.isPending}>
          Save
        </Button>
      </DialogFooter>
    </form>
  );
}
