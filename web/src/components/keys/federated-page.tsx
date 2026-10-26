import { LinkButton } from "@cloudflare/kumo/components/button";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import { serverInfoQuery } from "~/api/queries.ts";
import type { OAuthClient } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import {
  claimRulesToRecord,
  githubActionsSnippet,
  normalizeServerUrl,
  tokenExchangeCommand,
} from "~/components/keys/federated.ts";
import { useOAuthClientMutations } from "~/components/keys/mutations.ts";
import {
  AccessSections,
  DescriptionSection,
  TrustSection,
  oauthDraftFrom,
  oauthDraftIssues,
} from "~/components/keys/oauth-sections.tsx";
import { Code } from "~/components/ui/code.tsx";
import { CopyButton } from "~/components/ui/copy-text.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import {
  FormBar,
  FormLayout,
  FormPage,
  FormSection,
  useDraft,
} from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

const listPath = "/keys/oauth";

/**
 * The server URL workloads talk to: the configured one when the caller may read it, else this
 * page's.
 */
export function useServerUrl(me: Me): string {
  const serverInfo = useQuery({ ...serverInfoQuery, enabled: can(me, "feature_settings:read") });

  return normalizeServerUrl(serverInfo.data?.serverUrl);
}

/**
 * Creating a federated identity on a page of its own: its description, the trust conditions a
 * workload's token must meet, then the scopes and tags of the tokens it gets. Once it exists the
 * page swaps in place to the client id and the commands that use it; there is no secret to keep.
 */
export function CreateFederatedIdentityPage({ me }: { readonly me: Me }): ReactElement {
  const serverUrl = useServerUrl(me);
  const [created, setCreated] = useState<OAuthClient | null>(null);

  return created === null ? (
    <FormLayout>
      <PageHeader
        title="Create federated identity"
        description="A federated identity lets CI workflows and cloud workloads trade OIDC tokens for short-lived v2 API tokens."
      />
      {/* Keyed by the server URL: the audience defaults to it, and it may arrive after the page. */}
      <FederatedIdentityForm
        key={serverUrl}
        me={me}
        defaultAudience={serverUrl}
        onCreated={setCreated}
      />
    </FormLayout>
  ) : (
    <FormLayout>
      <PageHeader
        title="Federated identity created"
        description="No secret is stored. Workloads present an OIDC token from the issuer to trade for an API token."
      />
      <FormSection title="Identity" flush>
        <DefinitionList
          items={[
            {
              label: "Client ID",
              value: created.clientId,
              copy: created.clientId,
              copyLabel: "Copy client id",
            },
            { label: "Issuer", value: created.issuer },
            { label: "Subject", value: created.subject, wrap: true },
          ]}
        />
      </FormSection>
      <FederatedUsage client={created} serverUrl={serverUrl} />
      <FormBar>
        <LinkButton href={listPath} variant="primary">
          Done
        </LinkButton>
      </FormBar>
    </FormLayout>
  );
}

function FederatedIdentityForm({
  me,
  defaultAudience,
  onCreated,
}: {
  readonly me: Me;
  readonly defaultAudience: string;
  readonly onCreated: (client: OAuthClient) => void;
}): ReactElement {
  const { create } = useOAuthClientMutations();
  const { draft, dirty, submitted, update, markSubmitted } = useDraft(
    oauthDraftFrom(undefined, defaultAudience),
    create,
  );
  const issues = oauthDraftIssues(draft, true);
  const shown = submitted ? issues : {};

  function submit(): boolean {
    markSubmitted();

    if (Object.keys(issues).length > 0) {
      return false;
    }

    create.mutate(
      {
        body: {
          keyType: "federated",
          description: draft.description.trim(),
          issuer: draft.issuer.trim(),
          audience: draft.audience.trim(),
          subject: draft.subject.trim(),
          customClaimRules: claimRulesToRecord(draft.rules),
          scopes: [...draft.scopes],
          tags: [...draft.tags],
        },
      },
      {
        onSuccess: (data) => {
          onCreated(data.oauthClient);
        },
      },
    );

    return true;
  }

  return (
    <FormPage
      noun="identity"
      creating
      canEdit
      error={create.isError ? errorMessage(create.error) : undefined}
      errorTitle="The identity was not created"
      pending={create.isPending}
      dirty={dirty}
      onSubmit={submit}
      cancelTo={listPath}
    >
      <DescriptionSection
        title="Identity description"
        placeholder="GitHub Actions deploy, GitLab CI…"
        value={draft.description}
        onChange={update}
      />
      <TrustSection
        draft={draft}
        issues={shown}
        defaultAudience={defaultAudience}
        onChange={update}
      />
      <AccessSections me={me} draft={draft} issues={shown} onChange={update} />
    </FormPage>
  );
}

/**
 * How a workload uses the identity: the token exchange as a command and as a GitHub Actions step,
 * each copyable. Neither holds a secret, so the identity's own page shows them too.
 */
export function FederatedUsage({
  client,
  serverUrl,
}: {
  readonly client: Pick<OAuthClient, "clientId" | "audience">;
  readonly serverUrl: string;
}): ReactElement {
  const exchange = tokenExchangeCommand(serverUrl, client.clientId);
  const snippet = githubActionsSnippet(client.audience, serverUrl, client.clientId);

  return (
    <>
      <UsageSection
        title="Token exchange"
        info="Trades the workload's OIDC token for a v2 API token."
        value={exchange}
      />
      <UsageSection
        title="GitHub Actions"
        info="A workflow step that requests the OIDC token and trades it."
        value={snippet}
      />
    </>
  );
}

function UsageSection({
  title,
  info,
  value,
}: {
  readonly title: string;
  readonly info: string;
  readonly value: string;
}): ReactElement {
  return (
    <FormSection
      title={title}
      info={info}
      actions={<CopyButton value={value} label={`Copy ${title.toLowerCase()}`} />}
    >
      <div className="overflow-x-auto rounded-lg bg-kumo-tint p-3 ring ring-kumo-line">
        <Code className="break-all whitespace-pre-wrap">{value}</Code>
      </div>
    </FormSection>
  );
}
