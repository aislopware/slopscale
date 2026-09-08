import { DeleteResource } from "@cloudflare/kumo";
import { Button } from "@cloudflare/kumo/components/button";
import { Checkbox } from "@cloudflare/kumo/components/checkbox";
import { Input, Textarea } from "@cloudflare/kumo/components/input";
import { Switch } from "@cloudflare/kumo/components/switch";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { api } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import type { AccessRule, Posture } from "~/api/queries.ts";
import type { AccessMutations } from "~/components/access/mutations.ts";
import {
  expressionExamples,
  isClock,
  isWeekday,
  parseExpressions,
  rulesUsingPosture,
  weekdayLabels,
  weekdays,
} from "~/components/access/posture-model.ts";
import type { Weekday } from "~/components/access/posture-model.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";

type PostureBody = Parameters<AccessMutations["createPosture"]["mutate"]>[0]["body"];

export interface PostureDialogProps {
  /** The posture to edit; absent when creating one. */
  readonly posture?: Posture | undefined;
  /** Whether the server can answer ip:country, which needs a GeoIP database. */
  readonly geoIpAvailable: boolean;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
}

/** Creates a posture or edits one; the form mounts with the dialog so it starts from the record. */
export function PostureDialog(props: PostureDialogProps): ReactElement {
  const editing = props.posture !== undefined;

  return (
    <DialogRoot open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        size="lg"
        title={editing ? "Edit posture" : "New posture"}
        description="Conditions a machine must meet before a rule that names this posture lets its traffic through. Every expression must hold. A schedule limits the posture to a weekly window."
      >
        <PostureForm {...props} />
      </DialogContent>
    </DialogRoot>
  );
}

interface Draft {
  readonly name: string;
  readonly description: string;
  readonly expressions: string;
  readonly scheduled: boolean;
  readonly days: readonly Weekday[];
  readonly start: string;
  readonly end: string;
  readonly timezone: string;
}

const workdays: readonly Weekday[] = ["mon", "tue", "wed", "thu", "fri"];

function draftFrom(posture: Posture | undefined): Draft {
  const schedule = posture?.schedule;

  return {
    name: posture?.name ?? "",
    description: posture?.description ?? "",
    expressions: posture?.expressions.join("\n") ?? "",
    scheduled: schedule !== undefined,
    days: schedule === undefined ? workdays : schedule.days.filter(isWeekday),
    start: schedule?.start ?? "09:00",
    end: schedule?.end ?? "18:00",
    timezone: schedule?.timezone ?? localTimezone(),
  };
}

function localTimezone(): string {
  try {
    return new Intl.DateTimeFormat().resolvedOptions().timeZone;
  } catch {
    return "";
  }
}

/** The request body for the draft; the schedule is sent only when it is on. */
function bodyFrom(draft: Draft, expressions: string[]): PostureBody {
  return {
    name: draft.name.trim(),
    description: draft.description.trim(),
    expressions,
    ...(draft.scheduled
      ? {
          schedule: {
            days: [...draft.days],
            start: draft.start,
            end: draft.end,
            timezone: draft.timezone.trim(),
          },
        }
      : {}),
  };
}

/** Why the draft cannot be saved yet, or null when it can. */
function draftIssue(draft: Draft): string | null {
  if (draft.name.trim() === "") {
    return "incomplete";
  }

  if (parseExpressions(draft.expressions).length === 0 && !draft.scheduled) {
    return "A posture needs at least one expression or a schedule.";
  }

  if (
    draft.scheduled &&
    (draft.days.length === 0 || !isClock(draft.start) || !isClock(draft.end))
  ) {
    return "A schedule needs at least one day and HH:MM times.";
  }

  return null;
}

function PostureForm({
  posture,
  geoIpAvailable,
  onOpenChange,
  mutations,
}: Omit<PostureDialogProps, "open">): ReactElement {
  const [draft, setDraft] = useState<Draft>(() => draftFrom(posture));
  const mutation = posture === undefined ? mutations.createPosture : mutations.updatePosture;
  const update = (patch: Partial<Draft>): void => {
    setDraft((current) => ({ ...current, ...patch }));
  };

  const expressions = parseExpressions(draft.expressions);
  const check = useQuery({
    ...api.queryOptions("post", "/api/v1/posture/check", { body: { expressions } }),
    enabled: expressions.length > 0,
    placeholderData: keepPreviousData,
  });
  const errors = check.data?.errors ?? [];
  const firstError = errors.findIndex((message) => message !== "");
  const issue = draftIssue(draft);

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    const body = bodyFrom(draft, expressions);
    const done = {
      onSuccess: (): void => {
        toast.success(posture === undefined ? "Posture created" : "Posture updated");
        onOpenChange(false);
      },
    };

    if (posture === undefined) {
      mutations.createPosture.mutate({ body }, done);
    } else {
      mutations.updatePosture.mutate({ params: { path: { id: posture.id } }, body }, done);
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Name"
        value={draft.name}
        spellCheck={false}
        autoComplete="off"
        placeholder="Managed laptops"
        onChange={(event) => {
          update({ name: event.target.value });
        }}
      />
      <Input
        label="Description"
        required={false}
        value={draft.description}
        placeholder="Current client on a known serial number"
        onChange={(event) => {
          update({ description: event.target.value });
        }}
      />
      <Textarea
        label="Expressions"
        description="One per line. Attributes are node:… from what the client reports, custom:… set on the machine, and ip:… from where it connects."
        value={draft.expressions}
        placeholder={"node:tsVersion >= '1.80'\nnode:os IN ['macos', 'windows']"}
        spellCheck={false}
        autoResize
        minRows={3}
        maxRows={10}
        onChange={(event) => {
          update({ expressions: event.target.value });
        }}
        {...(firstError === -1
          ? {}
          : { error: `Line ${firstError + 1}: ${errors[firstError] ?? ""}` })}
      />
      <Examples
        geoIpAvailable={geoIpAvailable}
        onPick={(expression) => {
          update({
            expressions:
              draft.expressions.trim() === ""
                ? expression
                : `${draft.expressions.replace(/\n+$/v, "")}\n${expression}`,
          });
        }}
      />
      <Switch.Group>
        <Switch.Legend>Schedule</Switch.Legend>
        <Switch
          checked={draft.scheduled}
          onCheckedChange={(scheduled) => {
            update({ scheduled });
          }}
          label={
            <span className="flex flex-col gap-0.5">
              <span className="font-medium text-kumo-default">Only during a weekly window</span>
              <span className="text-xs text-kumo-subtle">
                Outside the window the posture does not hold, so the rules that name it close.
              </span>
            </span>
          }
        />
      </Switch.Group>
      {draft.scheduled ? <ScheduleFields draft={draft} onChange={update} /> : null}
      <DialogError message={mutation.isError ? errorMessage(mutation.error) : undefined} />
      <FormFooter
        label={posture === undefined ? "Create posture" : "Save"}
        pending={mutation.isPending}
        disabled={issue !== null || firstError !== -1}
      />
    </form>
  );
}

function Examples({
  geoIpAvailable,
  onPick,
}: {
  readonly geoIpAvailable: boolean;
  readonly onPick: (expression: string) => void;
}): ReactElement {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="text-xs text-kumo-subtle">Add an example:</span>
      {expressionExamples.map((example) => {
        const needsGeo = example.expression.startsWith("ip:country");

        return (
          <Button
            key={example.label}
            type="button"
            variant="outline"
            size="xs"
            disabled={needsGeo && !geoIpAvailable}
            title={
              needsGeo && !geoIpAvailable
                ? "Needs policy.geoip_database in the server configuration."
                : example.expression
            }
            onClick={() => {
              onPick(example.expression);
            }}
          >
            {example.label}
          </Button>
        );
      })}
    </div>
  );
}

function ScheduleFields({
  draft,
  onChange,
}: {
  readonly draft: Draft;
  readonly onChange: (patch: Partial<Draft>) => void;
}): ReactElement {
  const toggle = (day: Weekday, checked: boolean): void => {
    onChange({
      days: weekdays.filter((known) => (known === day ? checked : draft.days.includes(known))),
    });
  };

  return (
    <div className="flex flex-col gap-4 rounded-lg border border-kumo-line p-4">
      <div className="flex flex-wrap gap-x-4 gap-y-2">
        {weekdays.map((day) => (
          <Checkbox
            key={day}
            label={weekdayLabels[day]}
            checked={draft.days.includes(day)}
            onCheckedChange={(checked) => {
              toggle(day, checked);
            }}
          />
        ))}
      </div>
      <div className="grid gap-4 sm:grid-cols-3">
        <Input
          label="From"
          value={draft.start}
          placeholder="09:00"
          spellCheck={false}
          {...(isClock(draft.start) ? {} : { error: "HH:MM" })}
          onChange={(event) => {
            onChange({ start: event.target.value });
          }}
        />
        <Input
          label="To"
          description="An end before the start wraps past midnight."
          value={draft.end}
          placeholder="18:00"
          spellCheck={false}
          {...(isClock(draft.end) ? {} : { error: "HH:MM" })}
          onChange={(event) => {
            onChange({ end: event.target.value });
          }}
        />
        <Input
          label="Time zone"
          required={false}
          description="An IANA name such as Europe/Berlin. Empty means UTC."
          value={draft.timezone}
          placeholder="Asia/Ho_Chi_Minh"
          spellCheck={false}
          onChange={(event) => {
            onChange({ timezone: event.target.value });
          }}
        />
      </div>
    </div>
  );
}

export function DeletePostureDialog({
  posture,
  rules,
  open,
  onOpenChange,
  mutations,
}: {
  readonly posture: Posture;
  readonly rules: readonly AccessRule[];
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
}): ReactElement {
  const { deletePosture } = mutations;
  const using = rulesUsingPosture(rules, posture);

  // The server refuses to delete a posture a rule names, so say which rules instead of asking
  // for a confirmation that could only fail.
  if (using.length > 0) {
    return (
      <DialogRoot open={open} onOpenChange={onOpenChange}>
        <DialogContent
          size="sm"
          title="Posture in use"
          description={`${posture.name} is required by ${using.length === 1 ? "a rule" : `${using.length} rules`}. Edit or delete them first.`}
        >
          <ul className="flex flex-col gap-1 text-kumo-default">
            {using.map((rule) => (
              <li key={rule.id} className="truncate">
                <Link
                  to="/policy"
                  search={{ q: rule.name }}
                  className="hover:text-kumo-link"
                  onClick={() => {
                    onOpenChange(false);
                  }}
                >
                  {rule.name}
                </Link>
              </li>
            ))}
          </ul>
          <DialogFooter>
            <DialogClose render={<Button variant="secondary">Close</Button>} />
          </DialogFooter>
        </DialogContent>
      </DialogRoot>
    );
  }

  return (
    <DeleteResource
      open={open}
      onOpenChange={onOpenChange}
      resourceType="posture"
      resourceName={posture.name}
      deleteButtonText="Delete posture"
      isDeleting={deletePosture.isPending}
      {...(deletePosture.isError ? { errorMessage: errorMessage(deletePosture.error) } : {})}
      onDelete={() => {
        deletePosture.mutate(
          { params: { path: { id: posture.id } } },
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
