import { Tabs } from "@cloudflare/kumo/components/tabs";
import { cn } from "@cloudflare/kumo/utils";
import { lintGutter } from "@codemirror/lint";
import { useId, useMemo } from "react";
import type { ReactElement } from "react";

import { problemLine } from "~/components/access/rule-hujson.ts";
import { CodeEditor } from "~/components/ui/code-editor.tsx";
import type { EditorProblem } from "~/lib/editor/problems.ts";
import { hujson } from "~/lib/hujson/language.ts";

/** How the If column shows the rule: the condition rows, or the grant they compile to. */
export type RuleMode = "visual" | "hujson";

const modes = [
  { value: "visual", label: "Visual" },
  { value: "hujson", label: "HuJSON" },
];

/** The dashboard's "Visual Builder | Wirefilter" switch, above the condition groups. */
export function RuleModeSwitch({
  mode,
  onModeChange,
}: {
  readonly mode: RuleMode;
  readonly onModeChange: (mode: RuleMode) => void;
}): ReactElement {
  return (
    <Tabs
      variant="segmented"
      tabs={modes}
      value={mode}
      listClassName="w-full [&>*]:flex-1"
      onValueChange={(value) => {
        onModeChange(value === "hujson" ? "hujson" : "visual");
      }}
    />
  );
}

/** The policy file's language, without its completions: those are for a whole file, not a grant. */
const extensions = [hujson(), lintGutter()];

export interface RuleHujsonEditorProps {
  readonly value: string;
  readonly onChange: (next: string) => void;
  /** What stops the snippet being read back: a syntax error or an entry a rule cannot carry. */
  readonly problems: readonly EditorProblem[];
  /** The draft's own field errors after a save was tried, which the visual rows would show. */
  readonly fieldErrors: readonly string[];
  readonly readOnly: boolean;
}

/**
 * The rule as the grant it adds to the policy, in the policy file's editor. Each edit that parses
 * lands in the draft at once, so the preview follows; one that does not is underlined where it goes
 * wrong and spelled out under the editor, and the page stays here until it is fixed.
 */
export function RuleHujsonEditor({
  value,
  onChange,
  problems,
  fieldErrors,
  readOnly,
}: RuleHujsonEditorProps): ReactElement {
  const noteId = useId();
  const [problem] = problems;
  const error = problem === undefined ? (fieldErrors[0] ?? null) : problemLine(value, problem);
  const attributes = useMemo(
    () => ({ "aria-describedby": noteId, "aria-invalid": String(error !== null) }),
    [noteId, error],
  );

  return (
    <div className="flex flex-col gap-1.5">
      <div
        className={cn(
          "h-48 overflow-hidden rounded-md ring focus-within:ring-[1.5px]",
          error === null
            ? "ring-kumo-line focus-within:ring-kumo-focus/50"
            : "ring-kumo-danger focus-within:ring-kumo-danger",
        )}
      >
        <CodeEditor
          value={value}
          onChange={onChange}
          readOnly={readOnly}
          extensions={extensions}
          problems={problems}
          attributes={attributes}
          aria-label="Rule as HuJSON"
        />
      </div>
      {error === null ? (
        <p id={noteId} className="text-sm leading-snug text-pretty text-kumo-subtle">
          The grant this rule adds to the policy. Groups and postures go by name; the server writes
          them by id. Both directions and expiry stay under Then.
        </p>
      ) : (
        <p id={noteId} role="alert" className="text-sm leading-snug text-kumo-danger">
          {error}
        </p>
      )}
    </div>
  );
}
