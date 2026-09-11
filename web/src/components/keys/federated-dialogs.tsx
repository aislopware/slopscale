import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { CheckCircleIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import type { ReactElement, SubmitEvent } from "react";
import { useState } from "react";

import { errorMessage } from "~/api/error.ts";
import { serverInfoQuery } from "~/api/queries.ts";
import type { OAuthClient } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { ClaimRulesEditor } from "~/components/keys/claim-rules-editor.tsx";
import { useCreatedKey } from "~/components/keys/created-key.tsx";
import {
  claimRulesToRecord,
  githubActionsSnippet,
  normalizeServerUrl,
  subjectHint,
  tokenExchangeCommand,
  validateClaimRules,
  validateIssuerUrl,
} from "~/components/keys/federated.ts";
import type { ClaimRuleEntry } from "~/components/keys/federated.ts";
import { useOAuthClientMutations } from "~/components/keys/mutations.ts";
import { needsTags, scopeItems } from "~/components/keys/scopes.ts";
import { useKnownTags } from "~/components/tags/use-known-tags.ts";
import { Code } from "~/components/ui/code.tsx";
import { CopyText } from "~/components/ui/copy-text.tsx";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { TagField } from "~/components/ui/tag-field.tsx";

export function CreateFederatedIdentityDialog({
  me,
  open,
  onOpenChange,
}: {
  readonly me: Me;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  const [created, setCreated] = useCreatedKey<OAuthClient>(open);
  const serverInfo = useQuery({ ...serverInfoQuery, enabled: can(me, "feature_settings:read") });
  const serverUrl = normalizeServerUrl(serverInfo.data?.serverUrl);

  const close = (): void => {
    onOpenChange(false);
  };

  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="lg"
        title={created === null ? "New federated identity" : "Federated identity created"}
        description={
          created === null
            ? "A federated identity lets CI workflows and cloud workloads trade OIDC tokens for short-lived v2 API tokens."
            : undefined
        }
      >
        {created === null ? (
          <CreateFederatedIdentityForm me={me} defaultAudience={serverUrl} onCreated={setCreated} />
        ) : (
          <CreatedFederatedIdentity client={created} serverUrl={serverUrl} onDone={close} />
        )}
      </DialogContent>
    </DialogRoot>
  );
}

export function FederatedIdentityFields({
  issuer,
  audience,
  subject,
  rules,
  issuerError,
  rulesError,
  defaultAudience,
  onIssuerChange,
  onAudienceChange,
  onSubjectChange,
  onRulesChange,
}: {
  readonly issuer: string;
  readonly audience: string;
  readonly subject: string;
  readonly rules: readonly ClaimRuleEntry[];
  readonly issuerError: string | undefined;
  readonly rulesError: string | undefined;
  readonly defaultAudience: string;
  readonly onIssuerChange: (value: string) => void;
  readonly onAudienceChange: (value: string) => void;
  readonly onSubjectChange: (value: string) => void;
  readonly onRulesChange: (entries: ClaimRuleEntry[]) => void;
}): ReactElement {
  return (
    <>
      {/* The two halves of who is trusted, side by side where there is room for them: the form is
          long, and a wide dialog with one field per line pushes the claim rules off the screen. */}
      <div className="grid gap-4 md:grid-cols-2">
        <Input
          label="Issuer"
          type="url"
          required
          value={issuer}
          placeholder="https://token.actions.githubusercontent.com"
          description="The https URL of the OIDC provider that signs workload tokens."
          {...(issuerError === undefined ? {} : { error: issuerError })}
          onChange={(event) => {
            onIssuerChange(event.target.value);
          }}
        />
        <Input
          label="Audience"
          required
          value={audience}
          placeholder={defaultAudience || "https://scale.example.com"}
          description="The audience the OIDC token must carry. Default suggestion: server URL."
          onChange={(event) => {
            onAudienceChange(event.target.value);
          }}
        />
      </div>
      <Input
        label="Subject"
        required
        value={subject}
        placeholder="repo:org/repo:ref:refs/heads/main"
        description={subjectHint(issuer)}
        onChange={(event) => {
          onSubjectChange(event.target.value);
        }}
      />
      <ClaimRulesEditor entries={rules} error={rulesError} onChange={onRulesChange} />
    </>
  );
}

function CreateFederatedIdentityForm({
  me,
  defaultAudience,
  onCreated,
}: {
  readonly me: Me;
  readonly defaultAudience: string;
  readonly onCreated: (client: OAuthClient) => void;
}): ReactElement {
  const { create } = useOAuthClientMutations();
  const [description, setDescription] = useState("");
  const [issuer, setIssuer] = useState("");
  const [audience, setAudience] = useState(defaultAudience);
  const [subject, setSubject] = useState("");
  const [rules, setRules] = useState<readonly ClaimRuleEntry[]>([]);
  const [scopes, setScopes] = useState<readonly string[]>([]);
  const [tags, setTags] = useState<readonly string[]>([]);
  const knownTags = useKnownTags(me);

  const issuerError = issuer === "" ? undefined : validateIssuerUrl(issuer);
  const rulesError = validateClaimRules(rules);
  const canSubmit =
    issuer.trim() !== "" &&
    issuerError === undefined &&
    rulesError === undefined &&
    subject.trim() !== "" &&
    audience.trim() !== "" &&
    scopes.length > 0 &&
    (!needsTags(scopes) || tags.length > 0);

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    // Enter in a text input submits the form whatever the buttons say, so the guard is here too.
    if (!canSubmit || create.isPending) {
      return;
    }

    create.mutate(
      {
        body: {
          keyType: "federated",
          description: description.trim(),
          issuer: issuer.trim(),
          audience: audience.trim(),
          subject: subject.trim(),
          customClaimRules: claimRulesToRecord(rules),
          scopes: [...scopes],
          tags: [...tags],
        },
      },
      {
        onSuccess: (data) => {
          onCreated(data.oauthClient);
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
        placeholder="GitHub Actions deploy, GitLab CI…"
        onChange={(event) => {
          setDescription(event.target.value);
        }}
      />
      <FederatedIdentityFields
        issuer={issuer}
        audience={audience}
        subject={subject}
        rules={rules}
        issuerError={issuerError}
        rulesError={rulesError}
        defaultAudience={defaultAudience}
        onIssuerChange={setIssuer}
        onAudienceChange={setAudience}
        onSubjectChange={setSubject}
        onRulesChange={setRules}
      />
      <MultiPicker
        label="Scopes"
        description="What the federated token may do. You can only grant scopes you hold."
        placeholder="Pick scopes"
        items={scopeItems(me)}
        value={scopes}
        onValueChange={setScopes}
        empty="No scope matches."
      />
      <TagField
        required={needsTags(scopes)}
        description={
          needsTags(scopes)
            ? "Machine and pre-auth key scopes need tags. Everything created by the token is owned by them."
            : "Tags the token may put on the machines and keys it creates."
        }
        placeholder="tag:ci"
        value={tags}
        suggestions={knownTags}
        onValueChange={setTags}
      />
      <DialogError message={create.isError ? errorMessage(create.error) : undefined} />
      <DialogFooter submitDisabled={!canSubmit || create.isPending}>
        <DialogClose render={<Button variant="secondary">Cancel</Button>} />
        <Button type="submit" variant="primary" disabled={!canSubmit} loading={create.isPending}>
          Create identity
        </Button>
      </DialogFooter>
    </form>
  );
}

export function CreatedFederatedIdentity({
  client,
  serverUrl,
  onDone,
}: {
  readonly client: OAuthClient;
  readonly serverUrl: string;
  readonly onDone: () => void;
}): ReactElement {
  const exchange = tokenExchangeCommand(serverUrl, client.clientId);
  const snippet = githubActionsSnippet(client.audience, serverUrl, client.clientId);

  return (
    <div className="flex flex-col gap-4">
      <Banner
        icon={<CheckCircleIcon weight="fill" />}
        title="Federated identity ready"
        description="No secret is stored. Workloads present their OIDC token from the issuer to trade for an API token."
      />
      <div className="flex flex-col gap-1.5">
        <span className="text-sm font-medium text-kumo-default">Client ID</span>
        <div className="rounded-lg bg-kumo-tint p-3 ring ring-kumo-line">
          <CopyText
            value={client.clientId}
            label="Copy client id"
            wrap
            className="w-full items-start justify-between gap-2 text-left"
          />
        </div>
      </div>
      <div className="flex flex-col gap-1.5">
        <div className="flex items-center justify-between">
          <span className="text-sm font-medium text-kumo-default">Token exchange</span>
          <CopyText value={exchange} label="Copy token exchange command" />
        </div>
        <div className="overflow-x-auto rounded-lg bg-kumo-tint p-3 ring ring-kumo-line">
          <Code className="break-all whitespace-pre-wrap">{exchange}</Code>
        </div>
      </div>
      <div className="flex flex-col gap-1.5">
        <div className="flex items-center justify-between">
          <span className="text-sm font-medium text-kumo-default">GitHub Actions snippet</span>
          <CopyText value={snippet} label="Copy GitHub Actions snippet" />
        </div>
        <div className="overflow-x-auto rounded-lg bg-kumo-tint p-3 ring ring-kumo-line">
          <Code className="break-all whitespace-pre-wrap">{snippet}</Code>
        </div>
      </div>
      <DialogFooter>
        <Button variant="primary" onClick={onDone}>
          Done
        </Button>
      </DialogFooter>
    </div>
  );
}
