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
import { formatDuration, minuteSeconds } from "~/lib/time.ts";

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

function durationLabel(seconds: string): string {
  return (
    durationOptions.find((option) => option.value === seconds)?.label ??
    formatDuration(Number(seconds))
  );
}

type GroupOption = AccessRequestOptions["groups"][number];

function findGroup(options: AccessRequestOptions, id: string): GroupOption | undefined {
  return options.groups.find((group) => group.id === id);
}

/**
 * The durations a group takes: the presets up to its cap, and the cap itself when it is none of
 * them, so a group capped below the shortest preset still offers something.
 */
export function durationOptionsFor(
  group: GroupOption | undefined,
): { value: string; label: string }[] {
  const cap = group?.maxRequestSeconds ?? 0;

  if (cap === 0) {
    return durationOptions;
  }

  const within = durationOptions.filter((option) => Number(option.value) <= cap);

  return within.some((option) => Number(option.value) === cap)
    ? within
    : [...within, { value: String(cap), label: formatDuration(cap) }];
}

/** The duration kept when the group changes: the same one if the group takes it, else its longest. */
function clampSeconds(seconds: string, group: GroupOption | undefined): string {
  const choices = durationOptionsFor(group);

  return choices.some((option) => option.value === seconds)
    ? seconds
    : (choices.at(-1)?.value ?? seconds);
}

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
      seconds: clampSeconds(String(defaultSeconds), options.groups[0]),
      reason: "",
    },
    createRequest,
  );
  const group = findGroup(options, draft.groupId);
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
        onSuccess: ({ request }) => {
          // A group that approves by itself answers with the grant already in effect.
          if (request.status === "approved") {
            toast.success(
              "Access granted",
              `You are in ${groupName(options, request.groupId)} now, for ${durationLabel(String(request.durationSeconds))}.`,
            );
          } else {
            toast.success("Request sent");
          }

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
          options={durationOptionsFor(group)}
          onValueChange={(seconds) => {
            update({ seconds });
          }}
        />
        {group?.autoApprove === true ? (
          <p className="text-sm text-kumo-subtle">{grantedText(group)}</p>
        ) : null}
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
  const described = findGroup(options, draft.groupId)?.description ?? "";

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
          // A group with a shorter cap would refuse the duration chosen for the last one.
          onChange({
            groupId: value ?? "",
            seconds: clampSeconds(draft.seconds, findGroup(options, value ?? "")),
          });
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
  return findGroup(options, id)?.name ?? "Choose a group";
}

/** What asking a group that approves by itself gets: access at once, up to its cap if it has one. */
function grantedText(group: GroupOption): string {
  const cap = group.maxRequestSeconds ?? 0;

  return cap === 0
    ? "Granted immediately."
    : `Granted immediately, for up to ${durationLabel(String(cap))}.`;
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

  const duration = durationLabel(draft.seconds);
  const immediate = findGroup(options, draft.groupId)?.autoApprove === true;

  return (
    <p aria-live="polite" className="px-1 text-kumo-subtle">
      {draft.nodeId === everyMachine ? (
        "Every machine you own"
      ) : (
        <Fact>{machineName(options, draft.nodeId)}</Fact>
      )}{" "}
      joins <Fact>{groupName(options, draft.groupId)}</Fact> for <Fact>{duration}</Fact>{" "}
      {immediate ? "as soon as you send the request." : "once an approver agrees."}
    </p>
  );
}
