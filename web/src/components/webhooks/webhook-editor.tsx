import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Webhook } from "~/api/queries.ts";
import { fieldError } from "~/components/ui/field-error.ts";
import { FormPage, FormSection, nameFieldClass, useDraft } from "~/components/ui/form-page.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import type { PickerItem } from "~/components/ui/multi-picker.tsx";
import { toast } from "~/components/ui/toast.ts";
import {
  eventHint,
  providerOptions,
  toChoice,
  toProvider,
  urlError,
  urlField,
} from "~/components/webhooks/model.ts";
import type { ProviderChoice } from "~/components/webhooks/model.ts";
import { useWebhookMutations } from "~/components/webhooks/mutations.ts";
import { DeleteWebhookDialog } from "~/components/webhooks/webhook-dialogs.tsx";

export const webhooksPath = "/integrations/webhooks";

interface Draft {
  readonly url: string;
  readonly description: string;
  readonly provider: ProviderChoice;
  readonly subscriptions: readonly string[];
}

function draftFrom(webhook: Webhook | undefined): Draft {
  return {
    url: webhook?.url ?? "",
    description: webhook?.description ?? "",
    provider: toChoice(webhook?.providerType ?? ""),
    subscriptions: webhook?.subscriptions ?? [],
  };
}

/** What is wrong with each field, or nothing; the page shows them after the first save. */
function draftIssues(draft: Draft): { readonly url?: string; readonly events?: string } {
  const urlIssue =
    draft.url.trim() === ""
      ? `Enter the ${fieldName(draft.provider)}.`
      : urlError(draft.url, draft.provider);

  return {
    ...(urlIssue === null ? {} : { url: urlIssue }),
    ...(draft.subscriptions.length === 0 ? { events: "Pick at least one event." } : {}),
  };
}

/** The address field's label inside a sentence: "the URL", "the bot URL", "the recipients". */
function fieldName(provider: ProviderChoice): string {
  const { label } = urlField(provider);

  return label === label.toUpperCase() ? label : label.charAt(0).toLowerCase() + label.slice(1);
}

function eventItems(eventTypes: readonly string[]): PickerItem[] {
  return eventTypes.map((type) => ({ value: type, label: type, hint: eventHint(type) }));
}

export interface WebhookEditorProps {
  /** The webhook to edit; absent when creating one. */
  readonly webhook?: Webhook | undefined;
  readonly eventTypes: readonly string[];
  readonly canEdit: boolean;
  /** Takes the signing secret of a webhook just created, which the page shows once. */
  readonly onCreated?: (secret: string) => void;
}

/**
 * A webhook on a page of its own, for creating one or editing it: what it is for, where it posts
 * and which events it carries. Creating hands the signing secret to the page; saving an edit goes
 * back to the list.
 */
export function WebhookEditor({
  webhook,
  eventTypes,
  canEdit,
  onCreated,
}: WebhookEditorProps): ReactElement {
  const navigate = useNavigate();
  const mutations = useWebhookMutations();
  const mutation = webhook === undefined ? mutations.create : mutations.update;
  const { draft, dirty, submitted, update, markSubmitted } = useDraft(draftFrom(webhook), mutation);
  const [deleting, setDeleting] = useState(false);
  const issues = draftIssues(draft);
  const shown = submitted ? issues : {};

  const toList = (): void => {
    void navigate({ to: webhooksPath, ignoreBlocker: true });
  };

  function save(): boolean {
    markSubmitted();

    if (Object.keys(issues).length > 0) {
      return false;
    }

    const body = {
      url: draft.url.trim(),
      description: draft.description.trim(),
      providerType: toProvider(draft.provider),
      subscriptions: [...draft.subscriptions],
    };

    if (webhook === undefined) {
      mutations.create.mutate(
        { body },
        {
          onSuccess: (data) => {
            onCreated?.(data.webhook.secret ?? "");
          },
        },
      );
    } else {
      mutations.update.mutate(
        { params: { path: { id: webhook.id } }, body },
        {
          onSuccess: () => {
            toast.success("Webhook saved");
            toList();
          },
        },
      );
    }

    return true;
  }

  return (
    <FormPage
      noun="webhook"
      creating={webhook === undefined}
      canEdit={canEdit}
      error={mutation.isError ? errorMessage(mutation.error) : undefined}
      errorTitle={
        webhook === undefined ? "The webhook was not created" : "The webhook was not saved"
      }
      pending={mutation.isPending}
      dirty={dirty}
      onSubmit={save}
      cancelTo={webhooksPath}
      onDelete={() => {
        setDeleting(true);
      }}
      dialog={
        webhook === undefined ? null : (
          <DeleteWebhookDialog
            webhook={webhook}
            open={deleting}
            onOpenChange={setDeleting}
            mutations={mutations}
            onDeleted={toList}
          />
        )
      }
    >
      <FormSection title="Webhook details">
        <Input
          label="Description"
          required={false}
          className={nameFieldClass}
          value={draft.description}
          placeholder="Ops channel"
          description="What the webhook is for, so it can be told apart in the list."
          onChange={(event) => {
            update({ description: event.target.value });
          }}
        />
      </FormSection>
      <DestinationSection draft={draft} issue={shown.url} onChange={update} />
      <FormSection
        title="Events"
        info="Only the events picked here are delivered. A test event is sent whatever is picked."
      >
        <MultiPicker
          label="Events to deliver"
          placeholder="Search events…"
          items={eventItems(eventTypes)}
          value={draft.subscriptions}
          error={shown.events}
          onValueChange={(subscriptions) => {
            update({ subscriptions });
          }}
          empty="No event matches."
        />
      </FormSection>
    </FormPage>
  );
}

type Change = (patch: Partial<Draft>) => void;

/**
 * Where the webhook posts: the delivery shape, then the address in that shape's own words. The
 * address says what is wrong with it once the field is left; an empty one waits for the save.
 */
function DestinationSection({
  draft,
  issue,
  onChange,
}: {
  readonly draft: Draft;
  readonly issue: string | undefined;
  readonly onChange: Change;
}): ReactElement {
  const [left, setLeft] = useState(false);
  const field = urlField(draft.provider);
  const format = left ? urlError(draft.url, draft.provider) : null;

  return (
    <FormSection
      title="Destination"
      info="Generic endpoints get the signed JSON array of events. Chat, ntfy and email get the message alone."
    >
      <Select
        className="w-full"
        label="Provider"
        value={draft.provider}
        renderValue={(value: ProviderChoice | null) =>
          providerOptions.find((option) => option.value === value)?.label ?? ""
        }
        onValueChange={(value: ProviderChoice | null) => {
          if (value !== null) {
            onChange({ provider: value });
          }
        }}
      >
        {providerOptions.map((option) => (
          <Select.Option key={option.value} value={option.value}>
            <span className="flex flex-col gap-0.5">
              <span className="font-medium text-kumo-default">{option.label}</span>
              <span className="text-sm text-kumo-subtle">{option.description}</span>
            </span>
          </Select.Option>
        ))}
      </Select>
      <Input
        label={field.label}
        type={draft.provider === "email" ? "text" : "url"}
        value={draft.url}
        spellCheck={false}
        autoComplete="off"
        placeholder={field.placeholder}
        {...(field.hint === "" ? {} : { description: field.hint })}
        {...fieldError(issue ?? format ?? undefined)}
        onChange={(event) => {
          onChange({ url: event.target.value });
        }}
        onBlur={() => {
          setLeft(true);
        }}
      />
    </FormSection>
  );
}
