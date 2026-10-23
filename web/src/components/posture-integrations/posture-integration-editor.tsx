import { Input } from "@cloudflare/kumo/components/input";
import { ArrowSquareOutIcon } from "@phosphor-icons/react";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { PostureIntegration, PostureProvider } from "~/api/queries.ts";
import { SettingToggle } from "~/components/policy/rule-builder.tsx";
import {
  TestConnection,
  useCredentialCheck,
} from "~/components/posture-integrations/credential-check.tsx";
import type { CredentialCheck } from "~/components/posture-integrations/credential-check.tsx";
import {
  draftFrom,
  draftIssues,
  fieldInputType,
  fieldLabel,
  fieldPlaceholder,
  getDraftFieldValue,
  isSecretField,
  toCheckBody,
  toRequestBody,
} from "~/components/posture-integrations/model.ts";
import type { Draft } from "~/components/posture-integrations/model.ts";
import { usePostureIntegrationMutations } from "~/components/posture-integrations/mutations.ts";
import { DeletePostureIntegrationDialog } from "~/components/posture-integrations/posture-integration-dialogs.tsx";
import { fieldError } from "~/components/ui/field-error.ts";
import { FormPage, FormSection, nameFieldClass, useDraft } from "~/components/ui/form-page.tsx";
import { toast } from "~/components/ui/toast.ts";

export const postureIntegrationsPath = "/integrations/posture";

type Issues = Readonly<Record<string, string>>;

export interface PostureIntegrationEditorProps {
  /** The integration to edit; absent when creating one. */
  readonly integration?: PostureIntegration | undefined;
  /** The provider the integration reads from, chosen before the form opens. */
  readonly provider: PostureProvider;
  readonly canEdit: boolean;
}

/**
 * A posture integration on a page of its own, for creating one with the provider already chosen or
 * editing one: its name, the provider's credentials with a test of them, the attributes it writes,
 * and whether it syncs. Saving goes back to the list.
 */
export function PostureIntegrationEditor({
  integration,
  provider,
  canEdit,
}: PostureIntegrationEditorProps): ReactElement {
  const navigate = useNavigate();
  const mutations = usePostureIntegrationMutations();
  const mutation = integration === undefined ? mutations.create : mutations.update;
  const draftState = useDraft(draftFrom(integration, provider), mutation);
  const { draft, dirty, submitted, markSubmitted } = draftState;
  const check = useCredentialCheck(mutations.check);
  const [deleting, setDeleting] = useState(false);
  const editing = integration !== undefined;
  const hasSecret = integration?.hasSecret ?? false;
  const issues = draftIssues(draft, provider, { editing, hasSecret });
  const shown = submitted ? issues : {};
  const complete = Object.keys(issues).length === 0;

  // A test result belongs to the credentials it was sent with, so any edit forgets it.
  const update = (patch: Partial<Draft>): void => {
    draftState.update(patch);
    check.reset();
  };

  const toList = (): void => {
    void navigate({ to: postureIntegrationsPath, ignoreBlocker: true });
  };

  function testConnection(): void {
    markSubmitted();

    if (complete) {
      check.test(toCheckBody(draft, provider, integration?.id));
    }
  }

  function save(): boolean {
    markSubmitted();

    if (!complete) {
      return false;
    }

    const body = toRequestBody(draft, provider);
    const done = {
      onSuccess: (): void => {
        toast.success(
          integration === undefined ? "Posture integration created" : "Posture integration saved",
        );
        toList();
      },
    };

    if (integration === undefined) {
      mutations.create.mutate({ body }, done);
    } else {
      mutations.update.mutate({ params: { path: { id: integration.id } }, body }, done);
    }

    return true;
  }

  return (
    <FormPage
      noun="integration"
      creating={!editing}
      canEdit={canEdit}
      error={mutation.isError ? errorMessage(mutation.error) : undefined}
      errorTitle={editing ? "The integration was not saved" : "The integration was not created"}
      pending={mutation.isPending}
      dirty={dirty}
      onSubmit={save}
      cancelTo={postureIntegrationsPath}
      onDelete={() => {
        setDeleting(true);
      }}
      dialog={
        integration === undefined ? null : (
          <DeletePostureIntegrationDialog
            integration={integration}
            open={deleting}
            onOpenChange={setDeleting}
            mutations={mutations}
            onDeleted={toList}
          />
        )
      }
    >
      <FormSection title="Integration details">
        <Input
          label="Name"
          className={nameFieldClass}
          value={draft.name}
          spellCheck={false}
          autoComplete="off"
          placeholder={provider.label}
          {...fieldError(shown["name"])}
          onChange={(event) => {
            update({ name: event.target.value });
          }}
        />
      </FormSection>
      <ConnectionSection
        provider={provider}
        draft={draft}
        issues={shown}
        keepsSecret={editing && hasSecret}
        check={check}
        canTest={canEdit}
        onTest={testConnection}
        onChange={update}
      />
      <AttributesSection provider={provider} />
      <FormSection title="Status">
        <SettingToggle
          title="Enabled"
          description="Syncs machine attributes from this provider. Disabling removes the attributes it wrote."
          checked={draft.enabled}
          onCheckedChange={(enabled) => {
            update({ enabled });
          }}
        />
      </FormSection>
    </FormPage>
  );
}

/**
 * The provider's own fields, the link to its setup guide, and the test of the credentials with its
 * answer beside the button. A secret already stored may stay blank on an edit.
 */
function ConnectionSection({
  provider,
  draft,
  issues,
  keepsSecret,
  check,
  canTest,
  onTest,
  onChange,
}: {
  readonly provider: PostureProvider;
  readonly draft: Draft;
  readonly issues: Issues;
  readonly keepsSecret: boolean;
  readonly check: CredentialCheck;
  readonly canTest: boolean;
  readonly onTest: () => void;
  readonly onChange: (patch: Partial<Draft>) => void;
}): ReactElement {
  return (
    <FormSection
      title="Connection"
      info={`The credentials the server reads devices from ${provider.label} with. Secrets are stored on the server and never shown again.`}
      actions={
        provider.help === "" ? undefined : (
          <a
            href={provider.help}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1 text-kumo-link hover:underline"
          >
            Setup guide
            <ArrowSquareOutIcon aria-hidden className="size-3.5" />
          </a>
        )
      }
    >
      {provider.fields.map((field) => {
        const keeps = keepsSecret && isSecretField(field);

        return (
          <Input
            key={field}
            label={fieldLabel(field)}
            type={fieldInputType(field)}
            value={getDraftFieldValue(draft, field)}
            spellCheck={false}
            autoComplete="off"
            placeholder={fieldPlaceholder(field, provider.baseUrlDefault, keeps)}
            required={!keeps}
            {...(keeps ? { description: "Leave blank to keep the stored secret." } : {})}
            {...fieldError(issues[field])}
            onChange={(event) => {
              onChange({ [field]: event.target.value });
            }}
          />
        );
      })}
      <TestConnection status={check.status} disabled={!canTest} onTest={onTest} />
    </FormSection>
  );
}

/** What the integration writes on each machine it matches, read-only. */
function AttributesSection({
  provider,
}: {
  readonly provider: PostureProvider;
}): ReactElement | null {
  if (provider.attributes.length === 0) {
    return null;
  }

  return (
    <FormSection
      title="Attributes written"
      info={`Each synced machine gets these attributes under ${provider.prefix}, ready for postures to check.`}
    >
      <ul className="flex flex-wrap gap-1.5">
        {provider.attributes.map((attr) => (
          <li
            key={attr.name}
            className="inline-flex items-center gap-1 rounded bg-kumo-tint px-2 py-0.5 text-xs text-kumo-default ring ring-kumo-line"
            title={attr.description}
          >
            <span className="font-mono text-[0.9em]">{attr.name}</span>
            <span className="text-kumo-subtle">({attr.type})</span>
          </li>
        ))}
      </ul>
    </FormSection>
  );
}
