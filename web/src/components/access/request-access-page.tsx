import { Textarea } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { useNavigate } from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";

import { errorMessage } from "~/api/error.ts";
import type { AccessRequestOptions } from "~/api/queries.ts";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { durationChoices } from "~/components/access/request-model.ts";
import { fieldError } from "~/components/ui/field-error.ts";
import { ChoiceChips, FormPage, FormSection, useDraft } from "~/components/ui/form-page.tsx";
import { toast } from "~/components/ui/toast.ts";
import { minuteSeconds } from "~/lib/time.ts";

const backPath = "/access";
const everyMachine = "every";
const reasonRows = 3;
// The shortest choice is the default: access asked for in a hurry is the
// common case, and a request that is too short is extended, not revoked.
const defaultMinutes = 30;
const defaultSeconds = defaultMinutes * minuteSeconds;

const durationOptions = durationChoices.map((choice) => ({
  value: String(choice.seconds),
  label: choice.label,
}));

/** Why asking is not possible, or nothing when it is. */
export function askReason(hasUser: boolean, hasGroups: boolean): string | undefined {
  if (!hasUser) {
    return "These credentials are not a user";
  }

  return hasGroups ? undefined : "No group takes requests right now";
}

interface Draft {
  readonly groupId: string;
  readonly nodeId: string;
  readonly seconds: string;
  readonly reason: string;
}

type Change = (patch: Partial<Draft>) => void;

/**
 * Asking to join a requestable group for a while, on a page of its own: the group, which machines,
 * for how long and why, with a sentence saying what the approver will see. Sending goes back to My
 * access, where the request waits.
 */
export function RequestAccessForm({
  options,
}: {
  readonly options: AccessRequestOptions;
}): ReactElement {
  const navigate = useNavigate();
  const { createRequest } = useAccessMutations();
  const { draft, dirty, submitted, update, markSubmitted } = useDraft<Draft>(
    {
      groupId: options.groups[0]?.id ?? "",
      nodeId: everyMachine,
      seconds: String(defaultSeconds),
      reason: "",
    },
    createRequest,
  );
  const groupIssue = draft.groupId === "" ? "Choose the group to join." : undefined;

  function send(): boolean {
    markSubmitted();

    if (groupIssue !== undefined) {
      return false;
    }

    createRequest.mutate(
      {
        body: {
          groupId: draft.groupId,
          durationSeconds: Number(draft.seconds),
          reason: draft.reason.trim(),
          ...(draft.nodeId === everyMachine ? {} : { nodeId: draft.nodeId }),
        },
      },
      {
        onSuccess: () => {
          toast.success("Request sent");
          void navigate({ to: backPath, ignoreBlocker: true });
        },
      },
    );

    return true;
  }

  return (
    <FormPage
      noun="request"
      creating
      canEdit
      error={createRequest.isError ? errorMessage(createRequest.error) : undefined}
      errorTitle="The request was not sent"
      pending={createRequest.isPending}
      dirty={dirty}
      onSubmit={send}
      cancelTo={backPath}
      submitLabel="Send request"
    >
      <WhatSection
        options={options}
        draft={draft}
        issue={submitted ? groupIssue : undefined}
        onChange={update}
      />
      <FormSection title="Duration" info="The access ends on its own when the time is up.">
        <ChoiceChips
          label="Duration"
          value={draft.seconds}
          options={durationOptions}
          onValueChange={(seconds) => {
            update({ seconds });
          }}
        />
      </FormSection>
      <FormSection title="Reason" optional info="The approver sees this when deciding.">
        <Textarea
          aria-label="Reason"
          value={draft.reason}
          placeholder="Why you need access"
          minRows={reasonRows}
          onChange={(event) => {
            update({ reason: event.target.value });
          }}
        />
      </FormSection>
      <RequestSummary options={options} draft={draft} />
    </FormPage>
  );
}

/** The group to join and the machines that join it. */
function WhatSection({
  options,
  draft,
  issue,
  onChange,
}: {
  readonly options: AccessRequestOptions;
  readonly draft: Draft;
  readonly issue: string | undefined;
  readonly onChange: Change;
}): ReactElement {
  // A group nobody described says nothing, rather than a line of filler.
  const described = options.groups.find((group) => group.id === draft.groupId)?.description ?? "";

  return (
    <FormSection title="Access" info="Only groups an administrator marked requestable are listed.">
      <Select
        className="w-full max-w-md"
        label="Group"
        // Under the field rather than in the option: the popup is only as wide
        // as the trigger, and a sentence in an option pushes it past the page.
        description={described === "" ? undefined : described}
        value={draft.groupId}
        onValueChange={(value) => {
          onChange({ groupId: value ?? "" });
        }}
        renderValue={(value) => groupName(options, value)}
        {...fieldError(issue)}
      >
        {options.groups.map((group) => (
          <Select.Option key={group.id} value={group.id}>
            {group.name}
          </Select.Option>
        ))}
      </Select>
      <Select
        className="w-full max-w-md"
        label="Machine"
        description="One machine, or every machine you own."
        value={draft.nodeId}
        onValueChange={(value) => {
          onChange({ nodeId: value ?? everyMachine });
        }}
        renderValue={(value) => machineName(options, value)}
      >
        <Select.Option value={everyMachine}>Every machine you own</Select.Option>
        {options.nodes.map((node) => (
          <Select.Option key={node.id} value={node.id}>
            {node.name}
          </Select.Option>
        ))}
      </Select>
    </FormSection>
  );
}

function groupName(options: AccessRequestOptions, id: string): string {
  return options.groups.find((group) => group.id === id)?.name ?? "Choose a group";
}

function machineName(options: AccessRequestOptions, id: string): string {
  return id === everyMachine
    ? "Every machine you own"
    : (options.nodes.find((node) => node.id === id)?.name ?? id);
}

/** One fact of the summary sentence, darker than the words around it. */
function Fact({ children }: { readonly children: ReactNode }): ReactElement {
  return <span className="font-medium text-kumo-default">{children}</span>;
}

/** The request in one sentence, as the approver will read it. */
function RequestSummary({
  options,
  draft,
}: {
  readonly options: AccessRequestOptions;
  readonly draft: Draft;
}): ReactElement | null {
  if (draft.groupId === "") {
    return null;
  }

  const duration =
    durationOptions.find((option) => option.value === draft.seconds)?.label ?? draft.seconds;

  return (
    <p aria-live="polite" className="px-1 text-kumo-subtle">
      {draft.nodeId === everyMachine ? (
        "Every machine you own"
      ) : (
        <Fact>{machineName(options, draft.nodeId)}</Fact>
      )}{" "}
      joins <Fact>{groupName(options, draft.groupId)}</Fact> for <Fact>{duration}</Fact> once an
      approver agrees.
    </p>
  );
}
