import { Input } from "@cloudflare/kumo/components/input";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { AccessRule, Group, Posture } from "~/api/queries.ts";
import { isSelf } from "~/components/access/model.ts";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { RuleConditions, RuleSettings } from "~/components/access/rule-conditions.tsx";
import { DeleteRuleDialog } from "~/components/access/rule-dialogs.tsx";
import { ruleBody, ruleDraftErrors, ruleDraftFrom } from "~/components/access/rule-draft.ts";
import type { RuleDraft, RuleDraftErrors } from "~/components/access/rule-draft.ts";
import { RuleHujsonEditor, RuleModeSwitch } from "~/components/access/rule-hujson-editor.tsx";
import type { RuleMode } from "~/components/access/rule-hujson-editor.tsx";
import {
  changedConditions,
  parseRuleSnippet,
  ruleSnippet,
} from "~/components/access/rule-hujson.ts";
import { previewColumns } from "~/components/access/rule-preview.tsx";
import { EditorPage, FieldGrid, useDraft } from "~/components/policy/editor-page.tsx";
import { BuilderHeading, RulePreview, SettingToggle } from "~/components/policy/rule-builder.tsx";
import { fieldError } from "~/components/ui/field-error.ts";
import { Section } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";
import type { EditorProblem } from "~/lib/editor/problems.ts";

export interface RuleEditorProps {
  /** The rule to edit; absent when creating one. */
  readonly rule?: AccessRule | undefined;
  readonly rules: readonly AccessRule[];
  readonly groups: readonly Group[];
  /** Postures a rule may require of its sources. */
  readonly postures: readonly Posture[];
  /** Whether the policy file restricts traffic on its own, which changes what a rule means. */
  readonly policyFileEnforces: boolean;
  readonly canEdit: boolean;
}

type Change = (patch: Partial<RuleDraft>) => void;

/** The If column's mode and, in HuJSON, the snippet being edited and what stops it parsing. */
interface ConditionsView {
  readonly mode: RuleMode;
  readonly snippet: string;
  readonly problems: readonly EditorProblem[];
  readonly readOnly: boolean;
  readonly handleModeChange: (mode: RuleMode) => void;
  readonly handleSnippetChange: (text: string) => void;
}

const noProblems: readonly EditorProblem[] = [];

interface ConditionsViewOptions {
  readonly draft: RuleDraft;
  readonly groups: readonly Group[];
  readonly postures: readonly Posture[];
  readonly update: Change;
  readonly readOnly: boolean;
}

/**
 * The rule's conditions in one of two forms, chosen above them: the condition rows, or the grant
 * they compile to as HuJSON. Switching to HuJSON writes the snippet from the draft; switching back
 * is refused while the snippet does not parse, so the error stays in view where it can be fixed.
 */
function useConditionsView({
  draft,
  groups,
  postures,
  update,
  readOnly,
}: ConditionsViewOptions): ConditionsView {
  const [mode, setMode] = useState<RuleMode>("visual");
  const [snippet, setSnippet] = useState("");
  const [problems, setProblems] = useState<readonly EditorProblem[]>(noProblems);

  return {
    mode,
    snippet,
    problems,
    readOnly,
    handleModeChange: (next) => {
      if (next === mode) {
        return;
      }

      if (next === "hujson") {
        setSnippet(ruleSnippet(draft, groups, postures));
        setProblems(noProblems);
        setMode("hujson");

        return;
      }

      const result = parseRuleSnippet(snippet, groups, postures);

      if (!result.ok) {
        setProblems(result.problems);

        return;
      }

      update(changedConditions(draft, result.patch));
      setMode("visual");
    },
    handleSnippetChange: (text) => {
      setSnippet(text);

      const result = parseRuleSnippet(text, groups, postures);

      setProblems(result.ok ? noProblems : result.problems);

      if (result.ok) {
        const patch = changedConditions(draft, result.patch);

        if (Object.keys(patch).length > 0) {
          update(patch);
        }
      }
    },
  };
}

/** The two builder columns: the conditions under "If", the action and settings under "Then". */
function ScopeSection({
  draft,
  groups,
  postures,
  errors,
  toOwnMachines,
  policyFileEnforces,
  view,
  onChange,
}: {
  readonly view: ConditionsView;
  readonly draft: RuleDraft;
  readonly groups: readonly Group[];
  readonly postures: readonly Posture[];
  readonly errors: RuleDraftErrors;
  readonly toOwnMachines: boolean;
  readonly policyFileEnforces: boolean;
  readonly onChange: Change;
}): ReactElement {
  return (
    <Section title="Scope" bodyClassName="grid lg:grid-cols-2">
      <div className="flex min-w-0 flex-col gap-6 p-4">
        <BuilderHeading
          logic="if"
          title="Conditions"
          description={
            policyFileEnforces
              ? "Matching connections are allowed on top of what the policy file allows."
              : "Matching connections are allowed. Once a rule is enabled, whatever no rule allows is blocked."
          }
        />
        <RuleModeSwitch mode={view.mode} onModeChange={view.handleModeChange} />
        {view.mode === "visual" ? (
          <RuleConditions
            draft={draft}
            groups={groups}
            postures={postures}
            errors={errors}
            onChange={onChange}
          />
        ) : (
          <RuleHujsonEditor
            value={view.snippet}
            onChange={view.handleSnippetChange}
            problems={view.problems}
            fieldErrors={[errors.sources, errors.destinations, errors.ports].filter(
              (error) => error !== undefined,
            )}
            readOnly={view.readOnly}
          />
        )}
      </div>
      <div className="min-w-0 border-t border-kumo-line p-4 lg:border-t-0 lg:border-l">
        <div className="flex flex-col gap-6 lg:sticky lg:top-[calc(var(--app-header-height,0px)+1rem)]">
          <BuilderHeading logic="then" title="Action and settings" />
          <RuleSettings
            draft={draft}
            toOwnMachines={toOwnMachines}
            errors={errors}
            onChange={onChange}
          />
        </div>
      </div>
    </Section>
  );
}

/** The rule's name, its note and whether it is enforced once saved. */
function DetailsSection({
  draft,
  errors,
  onChange,
}: {
  readonly draft: RuleDraft;
  readonly errors: RuleDraftErrors;
  readonly onChange: Change;
}): ReactElement {
  return (
    <Section title="Details" bodyClassName="flex flex-col gap-6 p-4">
      <FieldGrid>
        <Input
          label="Rule name"
          value={draft.name}
          spellCheck={false}
          autoComplete="off"
          placeholder="Engineering to production"
          {...fieldError(errors.name)}
          onChange={(event) => {
            onChange({ name: event.target.value });
          }}
        />
        <Input
          label="Description"
          required={false}
          value={draft.description}
          placeholder="SSH and HTTPS from engineers' laptops"
          onChange={(event) => {
            onChange({ description: event.target.value });
          }}
        />
      </FieldGrid>
      <SettingToggle
        title="Set rule as active"
        description="When off, the rule is kept but not enforced until you turn it on."
        checked={draft.enabled}
        onCheckedChange={(enabled) => {
          onChange({ enabled });
        }}
      />
    </Section>
  );
}

/**
 * A rule as the dashboard's firewall policy builder lays one out: the conditions under "If" (who
 * opens the connection, what it reaches and the posture its machine must meet) as condition rows
 * or, behind the Visual | HuJSON switch, as the grant they compile to, the action and settings
 * under "Then" beside them, a live preview of the rule in three columns, the name and state under
 * Details, and the save bar pinned to the bottom of the window. Fields are checked as the operator
 * saves, each under its own row; the server's refusal stays in a banner at the top until the next
 * edit.
 */
export function RuleEditor({
  rule,
  rules,
  groups,
  postures,
  policyFileEnforces,
  canEdit,
}: RuleEditorProps): ReactElement {
  const navigate = useNavigate();
  const mutations = useAccessMutations();
  const mutation = rule === undefined ? mutations.createRule : mutations.updateRule;
  const { draft, dirty, submitted, update, markSubmitted } = useDraft(
    ruleDraftFrom(rule),
    mutation,
  );
  const [deleting, setDeleting] = useState(false);
  const view = useConditionsView({ draft, groups, postures, update, readOnly: !canEdit });
  const errors = ruleDraftErrors(draft);
  const shown = submitted ? errors : {};
  const toOwnMachines = groups.some(
    (group) => isSelf(group) && draft.destinations.includes(group.id),
  );
  const toList = (): void => {
    // After a save or a delete the draft no longer matters, so the leave guard stands aside.
    void navigate({ to: "/policy/rules", ignoreBlocker: true });
  };

  function save(): boolean {
    markSubmitted();

    if (Object.keys(errors).length > 0 || view.problems.length > 0) {
      return false;
    }

    const body = ruleBody(draft, toOwnMachines);
    const done = {
      onSuccess: (): void => {
        toast.success(rule === undefined ? "Rule created" : "Rule saved");
        toList();
      },
    };

    if (rule === undefined) {
      mutations.createRule.mutate({ body }, done);
    } else {
      mutations.updateRule.mutate({ params: { path: { id: rule.id } }, body }, done);
    }

    return true;
  }

  return (
    <EditorPage
      noun="rule"
      creating={rule === undefined}
      canEdit={canEdit}
      error={mutation.isError ? errorMessage(mutation.error) : undefined}
      pending={mutation.isPending}
      dirty={dirty}
      onSubmit={save}
      onCancel={() => {
        void navigate({ to: "/policy/rules" });
      }}
      onDelete={() => {
        setDeleting(true);
      }}
      dialog={
        rule === undefined ? null : (
          <DeleteRuleDialog
            rule={rule}
            opensTailnet={
              rule.enabled &&
              !policyFileEnforces &&
              rules.filter((candidate) => candidate.enabled).length === 1
            }
            open={deleting}
            onOpenChange={setDeleting}
            mutations={mutations}
            onDeleted={toList}
          />
        )
      }
    >
      <ScopeSection
        draft={draft}
        groups={groups}
        postures={postures}
        errors={shown}
        toOwnMachines={toOwnMachines}
        policyFileEnforces={policyFileEnforces}
        view={view}
        onChange={update}
      />
      <Section
        title="Preview"
        description="What the rule does once it is saved."
        bodyClassName="bg-kumo-elevated"
      >
        <RulePreview columns={previewColumns(draft, { groups, postures, toOwnMachines })} />
      </Section>
      <DetailsSection draft={draft} errors={shown} onChange={update} />
    </EditorPage>
  );
}
