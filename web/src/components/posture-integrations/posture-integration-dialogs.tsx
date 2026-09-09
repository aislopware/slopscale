import { DeleteResource } from "@cloudflare/kumo";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { Switch } from "@cloudflare/kumo/components/switch";
import { ArrowSquareOutIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { PostureIntegration, PostureProvider } from "~/api/queries.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import {
  TestConnection,
  useCredentialCheck,
} from "~/components/posture-integrations/credential-check.tsx";
import {
  draftFrom,
  draftIssue,
  fieldInputType,
  fieldLabel,
  fieldPlaceholder,
  getDraftFieldValue,
  isSecretField,
  providerLabel,
  toCheckBody,
  toProviderType,
  toRequestBody,
} from "~/components/posture-integrations/model.ts";
import type { Draft } from "~/components/posture-integrations/model.ts";
import type { PostureIntegrationMutations } from "~/components/posture-integrations/mutations.ts";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";

export interface PostureIntegrationDialogProps {
  readonly integration?: PostureIntegration;
  readonly providers: readonly PostureProvider[];
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: PostureIntegrationMutations;
}

export function PostureIntegrationDialog(props: PostureIntegrationDialogProps): ReactElement {
  const editing = props.integration !== undefined;
  const title = editing ? "Edit posture integration" : "New posture integration";

  return (
    <DialogRoot open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        size="lg"
        title={title}
        description="Sync device posture attributes from an endpoint security or device management service."
      >
        <PostureIntegrationForm {...props} />
      </DialogContent>
    </DialogRoot>
  );
}

function PostureIntegrationForm({
  integration,
  providers,
  onOpenChange,
  mutations,
}: Omit<PostureIntegrationDialogProps, "open">): ReactElement {
  const editing = integration !== undefined;
  const defaultProvider =
    providers.find((item) => item.provider === integration?.provider) ?? providers[0];
  const [draft, setDraft] = useState<Draft>(() => draftFrom(integration, defaultProvider));
  const check = useCredentialCheck(mutations.check);

  const selectedProvider =
    providers.find((item) => item.provider === draft.provider) ?? defaultProvider;
  const hasSecret = integration?.hasSecret ?? false;
  const mutation = editing ? mutations.update : mutations.create;
  const issue = draftIssue(draft, selectedProvider, { editing, hasSecret });

  const update = (patch: Partial<Draft>): void => {
    setDraft((current) => ({ ...current, ...patch }));
    check.reset();
  };

  const onProviderChange = (value: string | null): void => {
    if (value === null) {
      return;
    }
    const nextProvider = providers.find((item) => item.provider === value);
    update({
      provider: toProviderType(value),
      baseUrl: nextProvider?.baseUrlDefault ?? "",
    });
  };

  function testConnection(): void {
    check.test(toCheckBody(draft, selectedProvider, editing ? integration?.id : undefined));
  }

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    // Enter in a text input submits the form whatever the buttons say, so the guard is here too.
    if (issue !== null || mutation.isPending) {
      return;
    }

    const body = toRequestBody(draft, selectedProvider);

    if (integration === undefined) {
      mutations.create.mutate(
        { body },
        {
          onSuccess: () => {
            toast.success("Posture integration created");
            onOpenChange(false);
          },
        },
      );
    } else {
      mutations.update.mutate(
        { params: { path: { id: integration.id } }, body },
        {
          onSuccess: () => {
            toast.success("Posture integration updated");
            onOpenChange(false);
          },
        },
      );
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      {editing ? null : (
        <ProviderSelect
          providers={providers}
          selected={draft.provider}
          onSelect={onProviderChange}
        />
      )}
      <Input
        label="Name"
        value={draft.name}
        placeholder={selectedProvider?.label ?? "Integration name"}
        onChange={(event) => {
          update({ name: event.target.value });
        }}
      />
      <EnabledSwitch
        checked={draft.enabled}
        onChange={(enabled) => {
          update({ enabled });
        }}
      />
      <ProviderFields
        provider={selectedProvider}
        draft={draft}
        editing={editing}
        hasSecret={hasSecret}
        onChange={update}
      />
      <TestConnection status={check.status} disabled={issue !== null} onTest={testConnection} />
      {selectedProvider === undefined ? null : <HelpAndAttributes provider={selectedProvider} />}
      <DialogError message={mutation.isError ? errorMessage(mutation.error) : undefined} />
      <FormFooter
        label={editing ? "Save" : "Create posture integration"}
        pending={mutation.isPending}
        disabled={issue !== null}
      />
    </form>
  );
}

function ProviderSelect({
  providers,
  selected,
  onSelect,
}: {
  readonly providers: readonly PostureProvider[];
  readonly selected: string;
  readonly onSelect: (value: string | null) => void;
}): ReactElement {
  return (
    <Select
      className="w-full"
      label="Provider"
      value={selected}
      renderValue={(value: string | null) =>
        value === null ? "" : providerLabel(providers, value)
      }
      onValueChange={onSelect}
    >
      {providers.map((item) => (
        <Select.Option key={item.provider} value={item.provider}>
          <span className="flex flex-col gap-0.5">
            <span className="font-medium text-kumo-default">{item.label}</span>
            <span className="font-mono text-xs text-kumo-subtle">{item.prefix}</span>
          </span>
        </Select.Option>
      ))}
    </Select>
  );
}

function EnabledSwitch({
  checked,
  onChange,
}: {
  readonly checked: boolean;
  readonly onChange: (checked: boolean) => void;
}): ReactElement {
  return (
    <Switch.Group>
      <Switch.Legend>Status</Switch.Legend>
      <Switch
        checked={checked}
        onCheckedChange={onChange}
        label={
          <span className="flex flex-col gap-0.5">
            <span className="font-medium text-kumo-default">Enabled</span>
            <span className="text-xs text-kumo-subtle">
              Sync machine attributes from this provider. Disabling removes the attributes it wrote.
            </span>
          </span>
        }
      />
    </Switch.Group>
  );
}

function ProviderFields({
  provider,
  draft,
  editing,
  hasSecret,
  onChange,
}: {
  readonly provider: PostureProvider | undefined;
  readonly draft: Draft;
  readonly editing: boolean;
  readonly hasSecret: boolean;
  readonly onChange: (patch: Partial<Draft>) => void;
}): ReactElement | null {
  if (provider === undefined) {
    return null;
  }

  return (
    <>
      {provider.fields.map((field) => (
        <FieldInput
          key={field}
          field={field}
          draft={draft}
          provider={provider}
          editing={editing}
          hasSecret={hasSecret}
          onChange={onChange}
        />
      ))}
    </>
  );
}

function FieldInput({
  field,
  draft,
  provider,
  editing,
  hasSecret,
  onChange,
}: {
  readonly field: string;
  readonly draft: Draft;
  readonly provider: PostureProvider;
  readonly editing: boolean;
  readonly hasSecret: boolean;
  readonly onChange: (patch: Partial<Draft>) => void;
}): ReactElement {
  const secret = isSecretField(field);
  const keeps = editing && hasSecret && secret;
  const inputType = fieldInputType(field);
  const placeholder = fieldPlaceholder(field, provider.baseUrlDefault, keeps);
  const value = getDraftFieldValue(draft, field);

  return (
    <Input
      label={fieldLabel(field)}
      type={inputType}
      value={value}
      autoComplete="off"
      placeholder={placeholder}
      description={keeps ? "Stored, leave empty to keep" : undefined}
      onChange={(event) => {
        onChange({ [field]: event.target.value });
      }}
    />
  );
}

function HelpAndAttributes({ provider }: { readonly provider: PostureProvider }): ReactElement {
  return (
    <div className="flex flex-col gap-3 pt-1">
      {provider.help === "" ? null : (
        <div>
          <a
            href={provider.help}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1 text-xs text-kumo-link hover:underline"
          >
            Setup guide for {provider.label}
            <ArrowSquareOutIcon className="size-3" />
          </a>
        </div>
      )}
      {provider.attributes.length === 0 ? null : (
        <div className="flex flex-col gap-1.5 rounded-lg bg-kumo-tint p-3 ring ring-kumo-line">
          <span className="text-xs font-medium text-kumo-subtle">
            Attributes written ({provider.attributes.length})
          </span>
          <div className="flex flex-wrap gap-1.5">
            {provider.attributes.map((attr) => (
              <span
                key={attr.name}
                className="inline-flex items-center gap-1 rounded bg-kumo-base px-2 py-0.5 text-xs text-kumo-default ring ring-kumo-line"
                title={attr.description}
              >
                <span className="font-mono text-[0.9em]">{attr.name}</span>
                <span className="font-sans text-kumo-subtle">({attr.type})</span>
              </span>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

export function DeletePostureIntegrationDialog({
  integration,
  open,
  onOpenChange,
  mutations,
}: {
  readonly integration: PostureIntegration;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: PostureIntegrationMutations;
}): ReactElement {
  const { remove } = mutations;

  return (
    <DeleteResource
      open={open}
      onOpenChange={onOpenChange}
      resourceType="posture integration"
      resourceName={integration.name}
      deleteButtonText="Delete integration"
      isDeleting={remove.isPending}
      {...(remove.isError ? { errorMessage: errorMessage(remove.error) } : {})}
      onDelete={() => {
        remove.mutate(
          { params: { path: { id: integration.id } } },
          {
            onSuccess: () => {
              onOpenChange(false);
            },
          },
        );
      }}
    />
  );
}
