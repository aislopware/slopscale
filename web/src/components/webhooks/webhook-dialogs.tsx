import { DeleteResource } from "@cloudflare/kumo";
import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Webhook } from "~/api/queries.ts";
import { CreatedKey, useCreatedKey } from "~/components/keys/created-key.tsx";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
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
import type { WebhookMutations } from "~/components/webhooks/mutations.ts";

const secretNote = "The secret signs every delivery and is shown only once. Copy it now.";

export interface WebhookDialogProps {
  /** The webhook to edit; absent when creating one. */
  readonly webhook?: Webhook | undefined;
  readonly eventTypes: readonly string[];
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: WebhookMutations;
}

/** Creates a webhook, then shows its secret once; or edits one. The form mounts with the dialog. */
export function WebhookDialog(props: WebhookDialogProps): ReactElement {
  const editing = props.webhook !== undefined;
  const [secret, setSecret] = useCreatedKey(props.open);
  const title = editing ? "Edit webhook" : "New webhook";

  return (
    <DialogRoot open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        size="lg"
        title={secret === null ? title : "Webhook created"}
        description={
          secret === null
            ? "An endpoint the server posts events to. Generic endpoints get the signed JSON array, the rest get the message alone."
            : undefined
        }
      >
        {secret === null ? (
          <WebhookForm {...props} onCreated={setSecret} />
        ) : (
          <CreatedKey
            value={secret}
            note={secretNote}
            onDone={() => {
              props.onOpenChange(false);
            }}
          />
        )}
      </DialogContent>
    </DialogRoot>
  );
}

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

function draftIssue(draft: Draft): string | null {
  if (draft.url.trim() === "" || draft.subscriptions.length === 0) {
    return "incomplete";
  }

  return urlError(draft.url, draft.provider);
}

function eventItems(eventTypes: readonly string[]): PickerItem[] {
  return eventTypes.map((type) => ({ value: type, label: type, hint: eventHint(type) }));
}

function WebhookForm({
  webhook,
  eventTypes,
  onOpenChange,
  mutations,
  onCreated,
}: Omit<WebhookDialogProps, "open"> & {
  readonly onCreated: (secret: string) => void;
}): ReactElement {
  const [draft, setDraft] = useState<Draft>(() => draftFrom(webhook));
  const [touched, setTouched] = useState(false);
  const mutation = webhook === undefined ? mutations.create : mutations.update;
  const urlIssue = urlError(draft.url, draft.provider);
  const field = urlField(draft.provider);
  const update = (patch: Partial<Draft>): void => {
    setDraft((current) => ({ ...current, ...patch }));
  };

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

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
            onCreated(data.webhook.secret ?? "");
          },
        },
      );
    } else {
      mutations.update.mutate(
        { params: { path: { id: webhook.id } }, body },
        {
          onSuccess: () => {
            toast.success("Webhook updated");
            onOpenChange(false);
          },
        },
      );
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Description"
        required={false}
        value={draft.description}
        placeholder="Ops channel"
        onChange={(event) => {
          update({ description: event.target.value });
        }}
      />
      <Select
        className="w-full"
        label="Provider"
        value={draft.provider}
        renderValue={(value: ProviderChoice | null) =>
          providerOptions.find((option) => option.value === value)?.label ?? ""
        }
        onValueChange={(value: ProviderChoice | null) => {
          if (value !== null) {
            update({ provider: value });
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
        onChange={(event) => {
          update({ url: event.target.value });
        }}
        onBlur={() => {
          setTouched(true);
        }}
        {...(field.hint === "" ? {} : { description: field.hint })}
        {...(touched && urlIssue !== null ? { error: urlIssue } : {})}
      />
      <MultiPicker
        label="Events"
        description="Only these are delivered. A test event is sent whatever is picked here."
        placeholder="Events to deliver…"
        items={eventItems(eventTypes)}
        value={draft.subscriptions}
        onValueChange={(subscriptions) => {
          update({ subscriptions });
        }}
        empty="No event matches."
      />
      <DialogError message={mutation.isError ? errorMessage(mutation.error) : undefined} />
      <FormFooter
        label={webhook === undefined ? "Create webhook" : "Save"}
        pending={mutation.isPending}
        disabled={draftIssue(draft) !== null}
      />
    </form>
  );
}

/** Confirms, rotates, then shows the new secret once. */
export function RotateSecretDialog({
  webhook,
  open,
  onOpenChange,
  mutations,
}: {
  readonly webhook: Webhook;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: WebhookMutations;
}): ReactElement {
  const { rotate } = mutations;
  const [secret, setSecret] = useCreatedKey(open);

  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title={secret === null ? "Rotate secret" : "Secret rotated"}
        description={
          secret === null
            ? "Deliveries are signed with the new secret from now on. Update the receiver or it cannot verify them."
            : undefined
        }
      >
        {secret === null ? (
          <div className="flex flex-col gap-4">
            <DialogError message={rotate.isError ? errorMessage(rotate.error) : undefined} />
            <DialogFooter>
              <DialogClose render={<Button variant="secondary">Cancel</Button>} />
              <Button
                variant="primary"
                loading={rotate.isPending}
                onClick={() => {
                  rotate.mutate(
                    { params: { path: { id: webhook.id } } },
                    {
                      onSuccess: (data) => {
                        setSecret(data.webhook.secret ?? "");
                      },
                    },
                  );
                }}
              >
                Rotate secret
              </Button>
            </DialogFooter>
          </div>
        ) : (
          <CreatedKey
            value={secret}
            note={secretNote}
            onDone={() => {
              onOpenChange(false);
            }}
          />
        )}
      </DialogContent>
    </DialogRoot>
  );
}

export function DeleteWebhookDialog({
  webhook,
  open,
  onOpenChange,
  mutations,
}: {
  readonly webhook: Webhook;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: WebhookMutations;
}): ReactElement {
  const { remove } = mutations;

  return (
    <DeleteResource
      open={open}
      onOpenChange={onOpenChange}
      resourceType="webhook"
      resourceName={webhook.url}
      deleteButtonText="Delete webhook"
      isDeleting={remove.isPending}
      {...(remove.isError ? { errorMessage: errorMessage(remove.error) } : {})}
      onDelete={() => {
        remove.mutate(
          { params: { path: { id: webhook.id } } },
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
