import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { OAuthClient, User } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { FederatedUsage } from "~/components/keys/federated-page.tsx";
import { claimRulesToRecord, diffOAuthClient } from "~/components/keys/federated.ts";
import { useOAuthClientMutations } from "~/components/keys/mutations.ts";
import {
  AccessSections,
  DescriptionSection,
  TrustSection,
  oauthDraftFrom,
  oauthDraftIssues,
} from "~/components/keys/oauth-sections.tsx";
import type { OAuthDraft } from "~/components/keys/oauth-sections.tsx";
import { DeleteDialog } from "~/components/ui/confirm-dialog.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import { FormPage, FormSection, useDraft } from "~/components/ui/form-page.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { toast } from "~/components/ui/toast.ts";
import { userLabel } from "~/lib/node.ts";
import { useBaseline } from "~/lib/use-baseline.ts";

const listPath = "/keys/oauth";

export interface OAuthClientEditorProps {
  readonly client: OAuthClient;
  readonly me: Me;
  readonly canEdit: boolean;
  /** The users the creator is looked up in; empty when the caller may not list them. */
  readonly users: readonly User[];
  readonly serverUrl: string;
}

/** The PATCH body for the draft: only what differs from the record the page opened on. */
function patchFrom(baseline: OAuthClient, draft: OAuthDraft): ReturnType<typeof diffOAuthClient> {
  return diffOAuthClient(baseline, {
    description: draft.description,
    scopes: draft.scopes,
    tags: draft.tags,
    issuer: draft.issuer,
    audience: draft.audience,
    subject: draft.subject,
    customClaimRules: claimRulesToRecord(draft.rules),
  });
}

/**
 * An OAuth client or federated identity on a page of its own: what it is and when it was made at
 * the top, then its description, trust conditions, scopes and tags to edit, how a workload uses an
 * identity, and the save bar with Delete. Saving sends only what changed and goes back to the
 * list.
 */
export function OAuthClientEditor({
  client,
  me,
  canEdit,
  users,
  serverUrl,
}: OAuthClientEditorProps): ReactElement {
  const navigate = useNavigate();
  const { update: save, revoke } = useOAuthClientMutations();
  // The record the page opened on, held still: the list behind it refetches, and diffing against a
  // fresher record would turn a field nobody touched into a change to send.
  const baseline = useBaseline(client, client.clientId);
  // Held for the same reason, and because each read of the claim rules gives the rows new ids.
  const initial = useBaseline(oauthDraftFrom(baseline), baseline.clientId);
  const { draft, dirty, submitted, update, markSubmitted } = useDraft(initial, save);
  const [deleting, setDeleting] = useState(false);
  const federated = baseline.keyType === "federated";
  const noun = federated ? "identity" : "client";
  const issues = oauthDraftIssues(draft, federated);
  const shown = submitted ? issues : {};

  const toList = (): void => {
    void navigate({ to: listPath, ignoreBlocker: true });
  };

  function submit(): boolean {
    markSubmitted();

    if (Object.keys(issues).length > 0) {
      return false;
    }

    const patch = patchFrom(baseline, draft);

    // Nothing changed: there is nothing to send, and the save means "done here".
    if (Object.keys(patch).length === 0) {
      toList();

      return true;
    }

    save.mutate(
      { params: { path: { clientId: baseline.clientId } }, body: patch },
      {
        onSuccess: () => {
          toast.success(federated ? "Federated identity saved" : "OAuth client saved");
          toList();
        },
      },
    );

    return true;
  }

  return (
    <FormPage
      noun={noun}
      creating={false}
      canEdit={canEdit}
      error={save.isError ? errorMessage(save.error) : undefined}
      errorTitle={`The ${noun} was not saved`}
      pending={save.isPending}
      dirty={dirty}
      onSubmit={submit}
      cancelTo={listPath}
      onDelete={() => {
        setDeleting(true);
      }}
      dialog={
        <DeleteDialog
          open={deleting}
          onOpenChange={setDeleting}
          resourceType={federated ? "identity" : "OAuth client"}
          resourceName={baseline.clientId}
          deleteButtonText={federated ? "Delete identity" : "Delete OAuth client"}
          isDeleting={revoke.isPending}
          {...(revoke.isError ? { errorMessage: errorMessage(revoke.error) } : {})}
          onDelete={() => {
            revoke.mutate(
              { params: { path: { clientId: baseline.clientId } } },
              {
                onSuccess: () => {
                  toast.success(federated ? "Federated identity deleted" : "OAuth client revoked");
                  setDeleting(false);
                  toList();
                },
              },
            );
          }}
        />
      }
    >
      <OverviewSection client={baseline} users={users} />
      <DescriptionSection
        title={federated ? "Identity description" : "Client description"}
        placeholder={federated ? "GitHub Actions deploy, GitLab CI…" : "Terraform, CI deploys…"}
        value={draft.description}
        onChange={update}
      />
      {federated ? (
        <TrustSection draft={draft} issues={shown} defaultAudience={serverUrl} onChange={update} />
      ) : null}
      <AccessSections
        me={me}
        draft={draft}
        existingScopes={baseline.scopes}
        issues={shown}
        onChange={update}
      />
      {/* How a workload uses the identity as saved; reference, so it comes after the fields. */}
      {federated ? <FederatedUsage client={baseline} serverUrl={serverUrl} /> : null}
    </FormPage>
  );
}

/** The facts the page does not edit: the client id to copy, its kind, who made it and when. */
function OverviewSection({
  client,
  users,
}: {
  readonly client: OAuthClient;
  readonly users: readonly User[];
}): ReactElement {
  return (
    <FormSection title="Overview" flush>
      <DefinitionList
        items={[
          {
            label: "Client ID",
            value: client.clientId,
            copy: client.clientId,
            copyLabel: "Copy client id",
          },
          {
            label: "Kind",
            value: client.keyType === "federated" ? "Federated identity" : "OAuth client",
          },
          { label: "Created by", value: creatorLabel(client.userId, users) },
          { label: "Created", value: <RelativeTime value={client.createdAt} /> },
        ]}
      />
    </FormSection>
  );
}

/** The CLI made a client with no user; a user the caller may not list is named by id. */
function creatorLabel(userId: string | null, users: readonly User[]): string {
  if (userId === null || userId === "") {
    return "CLI";
  }

  const user = users.find((candidate) => candidate.id === userId);

  return user === undefined ? `User ${userId}` : userLabel(user);
}
