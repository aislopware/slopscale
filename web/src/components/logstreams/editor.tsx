import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { LogStream } from "~/api/queries.ts";
import { DeleteLogStreamDialog } from "~/components/logstreams/dialogs.tsx";
import {
  destinationOption,
  destinationOptions,
  toDestination,
  urlError,
} from "~/components/logstreams/model.ts";
import type { Destination } from "~/components/logstreams/model.ts";
import { useLogStreamMutations } from "~/components/logstreams/mutations.ts";
import { SettingToggle } from "~/components/policy/rule-builder.tsx";
import { fieldError } from "~/components/ui/field-error.ts";
import { FormPage, FormSection, nameFieldClass, useDraft } from "~/components/ui/form-page.tsx";
import { toast } from "~/components/ui/toast.ts";

export const logStreamsPath = "/integrations/log-streams";

interface Draft {
  readonly name: string;
  readonly destination: Destination;
  readonly url: string;
  readonly token: string;
  readonly enabled: boolean;
}

function draftFrom(stream: LogStream | undefined): Draft {
  return {
    name: stream?.name ?? "",
    destination: toDestination(stream?.destination ?? "http"),
    url: stream?.url ?? "",
    token: "",
    enabled: stream?.enabled ?? true,
  };
}

interface Issues {
  readonly name?: string;
  readonly url?: string;
  readonly token?: string;
}

/**
 * What is wrong with each field, or nothing; the page shows them after the first save. A stream
 * that already holds a credential keeps it when the field is left empty.
 */
function draftIssues(draft: Draft, keepsToken: boolean): Issues {
  const option = destinationOption(draft.destination);
  const urlIssue = draft.url.trim() === "" ? "Enter the URL to ship to." : urlError(draft.url);
  const tokenMissing = option.tokenRequired && draft.token.trim() === "" && !keepsToken;

  return {
    ...(draft.name.trim() === "" ? { name: "Give the stream a name." } : {}),
    ...(urlIssue === null ? {} : { url: urlIssue }),
    ...(tokenMissing ? { token: `${option.label} needs its ${option.tokenLabel}.` } : {}),
  };
}

export interface LogStreamEditorProps {
  /** The stream to edit; absent when creating one. */
  readonly stream?: LogStream | undefined;
  readonly canEdit: boolean;
}

/**
 * A log stream on a page of its own, for creating one or editing it: its name, where the audit log
 * goes and with which credential, and whether it ships. Saving goes back to the list.
 */
export function LogStreamEditor({ stream, canEdit }: LogStreamEditorProps): ReactElement {
  const navigate = useNavigate();
  const mutations = useLogStreamMutations();
  const mutation = stream === undefined ? mutations.create : mutations.update;
  const { draft, dirty, submitted, update, markSubmitted } = useDraft(draftFrom(stream), mutation);
  const [deleting, setDeleting] = useState(false);
  const keepsToken = stream?.hasToken ?? false;
  const issues = draftIssues(draft, keepsToken);
  const shown = submitted ? issues : {};

  const toList = (): void => {
    void navigate({ to: logStreamsPath, ignoreBlocker: true });
  };

  function save(): boolean {
    markSubmitted();

    if (Object.keys(issues).length > 0) {
      return false;
    }

    const body = {
      name: draft.name.trim(),
      destination: draft.destination,
      url: draft.url.trim(),
      token: draft.token.trim(),
      enabled: draft.enabled,
    };
    const done = {
      onSuccess: (): void => {
        toast.success(stream === undefined ? "Log stream created" : "Log stream saved");
        toList();
      },
    };

    if (stream === undefined) {
      mutations.create.mutate({ body }, done);
    } else {
      mutations.update.mutate({ params: { path: { id: stream.id } }, body }, done);
    }

    return true;
  }

  return (
    <FormPage
      noun="log stream"
      creating={stream === undefined}
      canEdit={canEdit}
      error={mutation.isError ? errorMessage(mutation.error) : undefined}
      errorTitle={
        stream === undefined ? "The log stream was not created" : "The log stream was not saved"
      }
      pending={mutation.isPending}
      dirty={dirty}
      onSubmit={save}
      cancelTo={logStreamsPath}
      onDelete={() => {
        setDeleting(true);
      }}
      dialog={
        stream === undefined ? null : (
          <DeleteLogStreamDialog
            stream={stream}
            open={deleting}
            onOpenChange={setDeleting}
            mutations={mutations}
            onDeleted={toList}
          />
        )
      }
    >
      <FormSection title="Log stream details">
        <Input
          label="Name"
          className={nameFieldClass}
          value={draft.name}
          spellCheck={false}
          autoComplete="off"
          placeholder="SIEM"
          {...fieldError(shown.name)}
          onChange={(event) => {
            update({ name: event.target.value });
          }}
        />
      </FormSection>
      <DestinationSection draft={draft} issues={shown} keepsToken={keepsToken} onChange={update} />
      <FormSection title="Status">
        <SettingToggle
          title="Enabled"
          description="A disabled stream keeps its settings and counters and ships nothing."
          checked={draft.enabled}
          onCheckedChange={(enabled) => {
            update({ enabled });
          }}
        />
      </FormSection>
    </FormPage>
  );
}

type Change = (patch: Partial<Draft>) => void;

/**
 * Where the entries go: the destination's shape, its URL and its credential. The URL says what is
 * wrong with it once the field is left; an empty one waits for the save.
 */
function DestinationSection({
  draft,
  issues,
  keepsToken,
  onChange,
}: {
  readonly draft: Draft;
  readonly issues: Issues;
  readonly keepsToken: boolean;
  readonly onChange: Change;
}): ReactElement {
  const [left, setLeft] = useState(false);
  const option = destinationOption(draft.destination);
  const format = left ? urlError(draft.url) : null;

  return (
    <FormSection
      title="Destination"
      info="Every audit log entry is posted to the destination in batches, in the shape it expects."
    >
      <Select
        className="w-full"
        label="Destination"
        value={draft.destination}
        renderValue={(value: Destination | null) =>
          value === null ? "" : destinationOption(value).label
        }
        onValueChange={(value: Destination | null) => {
          if (value !== null) {
            onChange({ destination: value });
          }
        }}
      >
        {destinationOptions.map((item) => (
          <Select.Option key={item.value} value={item.value}>
            <span className="flex flex-col gap-0.5">
              <span className="font-medium text-kumo-default">{item.label}</span>
              <span className="text-sm text-kumo-subtle">{item.description}</span>
            </span>
          </Select.Option>
        ))}
      </Select>
      <Input
        label="URL"
        type="url"
        value={draft.url}
        spellCheck={false}
        autoComplete="off"
        placeholder={option.placeholder}
        {...fieldError(issues.url ?? format ?? undefined)}
        onChange={(event) => {
          onChange({ url: event.target.value });
        }}
        onBlur={() => {
          setLeft(true);
        }}
      />
      <Input
        label={option.tokenLabel}
        type="password"
        required={option.tokenRequired && !keepsToken}
        value={draft.token}
        autoComplete="off"
        placeholder={keepsToken ? "Unchanged" : ""}
        description={
          keepsToken
            ? "Leave blank to keep the stored credential."
            : "Stored on the server and never shown again."
        }
        {...fieldError(issues.token)}
        onChange={(event) => {
          onChange({ token: event.target.value });
        }}
      />
    </FormSection>
  );
}
