import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import { InfoIcon, WarningCircleIcon, WarningIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import { TrayBand, TrayPanel } from "~/components/ui/section.tsx";
import type { EditorProblem } from "~/lib/editor/problems.ts";

const iconSize = 14;

const severities: Record<EditorProblem["severity"], { icon: Icon; color: string; label: string }> =
  {
    error: { icon: WarningCircleIcon, color: "text-kumo-danger", label: "Error" },
    warning: { icon: WarningIcon, color: "text-kumo-warning", label: "Warning" },
    info: { icon: InfoIcon, color: "text-kumo-info", label: "Note" },
  };

/** Where an offset falls in the text, counted from one the way an editor's status bar counts. */
function position(text: string, offset: number): { line: number; column: number } {
  const before = text.slice(0, offset);
  const lineStart = before.lastIndexOf("\n") + 1;

  return { line: before.split("\n").length, column: offset - lineStart + 1 };
}

/**
 * The problems of the draft as a list under the editor, the way a code editor's Problems panel has
 * them: the severity's icon, the message, and the line and column on the right. Picking one moves
 * the editor's selection onto the text it is about. Errors come before warnings, each in the order
 * they appear in the file.
 */
export function ProblemsPanel({
  text,
  problems,
  onSelect,
}: {
  readonly text: string;
  readonly problems: readonly EditorProblem[];
  readonly onSelect: (problem: EditorProblem) => void;
}): ReactElement | null {
  if (problems.length === 0) {
    return null;
  }

  const order = Object.keys(severities);
  const sorted = problems.toSorted(
    (left, right) =>
      order.indexOf(left.severity) - order.indexOf(right.severity) || left.from - right.from,
  );

  return (
    <>
      <TrayBand className="text-sm">
        <h3 className="font-semibold text-kumo-default">Problems</h3>
        <span className="text-xs text-kumo-subtle">{problems.length}</span>
      </TrayBand>
      <TrayPanel>
        <ul className="max-h-48 overflow-y-auto" aria-label="Problems in the draft">
          {sorted.map((problem) => {
            const severity = severities[problem.severity];
            const Glyph = severity.icon;
            const { line, column } = position(text, problem.from);

            return (
              <li
                key={`${problem.severity}-${String(problem.from)}-${problem.message}`}
                className="not-first:border-t not-first:border-kumo-hairline"
              >
                <button
                  type="button"
                  className="grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-2 px-4 py-2 text-left text-sm hover:bg-kumo-tint focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none focus-visible:ring-inset"
                  onClick={() => {
                    onSelect(problem);
                  }}
                >
                  <span className={cn("flex h-lh items-center", severity.color)}>
                    <Glyph size={iconSize} weight="fill" aria-label={severity.label} />
                  </span>
                  <span className="[overflow-wrap:anywhere] text-kumo-default">
                    {problem.message}
                  </span>
                  <span className="font-mono text-xs leading-5 whitespace-nowrap text-kumo-subtle tabular-nums">
                    Ln {line}, Col {column}
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      </TrayPanel>
    </>
  );
}
