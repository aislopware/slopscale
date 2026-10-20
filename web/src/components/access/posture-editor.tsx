import { Input } from "@cloudflare/kumo/components/input";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import type { AccessRule, Posture } from "~/api/queries.ts";
import { ExpressionEditor, firstExpressionError } from "~/components/access/expression-editor.tsx";
import { useAccessMutations } from "~/components/access/mutations.ts";
import type { AccessMutations } from "~/components/access/mutations.ts";
import { DeletePostureDialog } from "~/components/access/posture-dialogs.tsx";
import { ExampleTiles, ScheduleFields } from "~/components/access/posture-fields.tsx";
import { isClock, isWeekday, parseExpressions } from "~/components/access/posture-model.ts";
import type { Weekday } from "~/components/access/posture-model.ts";
import { EditorPage, FieldGrid, useDraft } from "~/components/policy/editor-page.tsx";
import { SettingToggle } from "~/components/policy/rule-builder.tsx";
import { fieldError } from "~/components/ui/field-error.ts";
import { Section } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";
import { useDebounced } from "~/lib/use-debounced.ts";

type PostureBody = Parameters<AccessMutations["createPosture"]["mutate"]>[0]["body"];

/** How long typing pauses before the draft goes to the server; the editor checks syntax itself. */
const checkDelayMs = 400;

const noErrors: readonly string[] = [];

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

type Change = (patch: Partial<Draft>) => void;

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

/** What is wrong with each part of the draft, written for the line under it. */
interface DraftErrors {
  readonly name?: string;
  readonly conditions?: string;
  readonly days?: string;
}

function draftErrors(draft: Draft): DraftErrors {
  return {
    ...(draft.name.trim() === "" ? { name: "Name is required." } : {}),
    ...(parseExpressions(draft.expressions).length === 0 && !draft.scheduled
      ? { conditions: "Add at least one expression, or turn the schedule on." }
      : {}),
    ...(draft.scheduled && draft.days.length === 0 ? { days: "Pick at least one day." } : {}),
  };
}

interface ExpressionCheck {
  /** The expressions of the draft, one per non-empty line. */
  readonly expressions: string[];
  /** The server's verdict per line, kept only while it is about the current draft. */
  readonly serverErrors: readonly string[];
  /** The first thing wrong with the expressions, from the editor or the server. */
  readonly error: string | null;
}

/** Sends the expressions to the server once typing pauses, for the verdicts the editor shows. */
function useExpressionCheck(text: string): ExpressionCheck {
  const expressions = parseExpressions(text);
  const settled = parseExpressions(useDebounced(text, checkDelayMs));
  const check = useQuery({
    ...api.queryOptions("post", "/api/v1/posture/check", { body: { expressions: settled } }),
    enabled: settled.length > 0,
    placeholderData: keepPreviousData,
  });
  const current =
    settled.length === expressions.length &&
    settled.every((line, index) => line === expressions[index]);
  const serverErrors = current ? (check.data?.errors ?? noErrors) : noErrors;

  return { expressions, serverErrors, error: firstExpressionError(text, serverErrors) };
}

/**
 * The expressions a machine must pass and the ready-made ones as tiles. A new posture starts from
 * the tiles, as the dashboard's posture checks start from a type picker, with the raw expressions
 * after them; an existing one leads with its expressions and offers the tiles to add more.
 */
function ConditionsSection({
  draft,
  creating,
  serverErrors,
  error,
  geoIpAvailable,
  onChange,
}: {
  readonly draft: Draft;
  readonly creating: boolean;
  readonly serverErrors: readonly string[];
  readonly error: string | undefined;
  readonly geoIpAvailable: boolean;
  readonly onChange: Change;
}): ReactElement {
  const tiles = (
    <ExampleTiles
      title={creating ? "Select an attribute" : "Add an expression"}
      {...(creating
        ? {
            description:
              "Each one adds its expression to the editor below, where you can change it.",
          }
        : {})}
      geoIpAvailable={geoIpAvailable}
      onPick={(expression) => {
        onChange({
          expressions:
            draft.expressions.trim() === ""
              ? expression
              : `${draft.expressions.replace(/\n+$/v, "")}\n${expression}`,
        });
      }}
    />
  );

  return (
    <Section
      title="Conditions"
      description="Every expression must hold for a machine to pass. A rule that requires the posture admits only machines that pass."
      bodyClassName="flex flex-col gap-5 p-4"
    >
      {creating ? tiles : null}
      <ExpressionEditor
        label="Expressions"
        description="One per line. Use node:… for what the client reports, custom:… for machine attributes and ip:… for where it connects."
        value={draft.expressions}
        placeholder={"node:tsVersion >= '1.80'\nnode:os IN ['macos', 'windows']"}
        serverErrors={serverErrors}
        onChange={(expressions) => {
          onChange({ expressions });
        }}
      />
      {error === undefined ? null : (
        <p className="-mt-3 text-sm leading-snug text-kumo-danger">{error}</p>
      )}
      {creating ? null : tiles}
    </Section>
  );
}

function BasicSection({
  draft,
  nameError,
  onChange,
}: {
  readonly draft: Draft;
  readonly nameError: string | undefined;
  readonly onChange: Change;
}): ReactElement {
  return (
    <Section title="Basic information" bodyClassName="p-4">
      <FieldGrid>
        <Input
          label="Posture name"
          value={draft.name}
          spellCheck={false}
          autoComplete="off"
          placeholder="Managed laptops"
          {...fieldError(nameError)}
          onChange={(event) => {
            onChange({ name: event.target.value });
          }}
        />
        <Input
          label="Description"
          required={false}
          value={draft.description}
          placeholder="Current client on a known serial number"
          onChange={(event) => {
            onChange({ description: event.target.value });
          }}
        />
      </FieldGrid>
    </Section>
  );
}

export interface PostureEditorProps {
  /** The posture to edit; absent when creating one. */
  readonly posture?: Posture | undefined;
  readonly rules: readonly AccessRule[];
  /** Whether the server can answer ip:country, which needs a GeoIP database. */
  readonly geoIpAvailable: boolean;
  readonly canEdit: boolean;
}

/**
 * A posture on a page of its own, as the dashboard's posture check editor is: its name, the
 * expressions a machine must pass with a grid of ready-made ones to start from, the weekly window,
 * and the save bar at the foot of the window. The server checks the expressions as they are typed
 * and its verdict lands on the line it is about.
 */
export function PostureEditor({
  posture,
  rules,
  geoIpAvailable,
  canEdit,
}: PostureEditorProps): ReactElement {
  const navigate = useNavigate();
  const mutations = useAccessMutations();
  const mutation = posture === undefined ? mutations.createPosture : mutations.updatePosture;
  const { draft, dirty, submitted, update, markSubmitted } = useDraft(draftFrom(posture), mutation);
  const [deleting, setDeleting] = useState(false);
  const check = useExpressionCheck(draft.expressions);
  const errors = draftErrors(draft);
  const shown = submitted ? errors : {};
  const toList = (): void => {
    void navigate({ to: "/policy/postures", ignoreBlocker: true });
  };

  function save(): boolean {
    markSubmitted();

    if (
      Object.keys(errors).length > 0 ||
      check.error !== null ||
      (draft.scheduled && (!isClock(draft.start) || !isClock(draft.end)))
    ) {
      return false;
    }

    const body = bodyFrom(draft, check.expressions);
    const done = {
      onSuccess: (): void => {
        toast.success(posture === undefined ? "Posture created" : "Posture saved");
        toList();
      },
    };

    if (posture === undefined) {
      mutations.createPosture.mutate({ body }, done);
    } else {
      mutations.updatePosture.mutate({ params: { path: { id: posture.id } }, body }, done);
    }

    return true;
  }

  return (
    <EditorPage
      noun="posture"
      creating={posture === undefined}
      canEdit={canEdit}
      error={mutation.isError ? errorMessage(mutation.error) : undefined}
      pending={mutation.isPending}
      dirty={dirty}
      onSubmit={save}
      onCancel={() => {
        void navigate({ to: "/policy/postures" });
      }}
      onDelete={() => {
        setDeleting(true);
      }}
      dialog={
        posture === undefined ? null : (
          <DeletePostureDialog
            posture={posture}
            rules={rules}
            open={deleting}
            onOpenChange={setDeleting}
            mutations={mutations}
            onDeleted={toList}
          />
        )
      }
    >
      <BasicSection draft={draft} nameError={shown.name} onChange={update} />
      <ConditionsSection
        draft={draft}
        creating={posture === undefined}
        serverErrors={check.serverErrors}
        error={shown.conditions}
        geoIpAvailable={geoIpAvailable}
        onChange={update}
      />
      <Section title="Schedule" bodyClassName="p-4">
        <SettingToggle
          title="Only during a weekly window"
          description="Outside the window the posture fails, so rules that require it deny traffic."
          checked={draft.scheduled}
          onCheckedChange={(scheduled) => {
            update({ scheduled });
          }}
        >
          {draft.scheduled ? (
            <ScheduleFields draft={draft} daysError={shown.days} onChange={update} />
          ) : null}
        </SettingToggle>
      </Section>
    </EditorPage>
  );
}
