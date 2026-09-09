import { fieldVariants } from "@cloudflare/kumo/components/field";
import { Label } from "@cloudflare/kumo/components/label";
import { cn } from "@cloudflare/kumo/utils";
import { useEffect, useId, useMemo, useRef } from "react";
import type { ReactElement } from "react";

import { CodeEditor } from "~/components/ui/code-editor.tsx";
import type { EditorProblem } from "~/lib/editor/problems.ts";
import { checkExpression } from "~/lib/posture/check.ts";
import { posture } from "~/lib/posture/language.ts";

const extensions = posture();

/** A line of the draft and the expression on it, for mapping the server's answers back. */
interface DraftLine {
  readonly from: number;
  readonly to: number;
  readonly text: string;
  /** The index among the non-blank lines, which is what the server was sent. */
  readonly index: number;
}

function draftLines(text: string): DraftLine[] {
  const lines: DraftLine[] = [];
  let from = 0;
  let index = 0;

  for (const line of text.split("\n")) {
    const blank = line.trim() === "";

    lines.push({ from, to: from + line.length, text: line, index: blank ? -1 : index });
    from += line.length + 1;

    if (!blank) {
      index += 1;
    }
  }

  return lines;
}

/**
 * The server's verdict on each expression, on the line it came from. The editor already shows the
 * same parse errors the moment they are typed, so a line the parser refused is left to it.
 */
function serverLineProblems(text: string, errors: readonly string[]): EditorProblem[] {
  const problems: EditorProblem[] = [];

  for (const line of draftLines(text)) {
    const message = line.index === -1 ? "" : (errors[line.index] ?? "");
    const parsedHere = checkExpression(line.text).every((problem) => problem.severity !== "error");

    if (message !== "" && parsedHere) {
      problems.push({ from: line.from, to: line.to, severity: "error", message });
    }
  }

  return problems;
}

/** The first thing wrong, for the line under the field: what the editor found, else the server. */
export function firstExpressionError(text: string, errors: readonly string[]): string | null {
  for (const line of draftLines(text)) {
    const own = checkExpression(line.text).find((problem) => problem.severity === "error");
    const message = own?.message ?? (line.index === -1 ? "" : (errors[line.index] ?? ""));

    if (line.index !== -1 && message !== "") {
      return `Line ${line.from === 0 ? 1 : text.slice(0, line.from).split("\n").length}: ${message}`;
    }
  }

  return null;
}

export interface ExpressionEditorProps {
  readonly label: string;
  readonly description: string;
  readonly value: string;
  readonly onChange: (next: string) => void;
  readonly placeholder?: string;
  /** What the server said about each expression, "" when it accepted one. */
  readonly serverErrors: readonly string[];
}

/**
 * Posture expressions, one per line, in an editor the size of a field: every token is coloured, the
 * parser underlines a mistake as it is typed, attributes and operators complete, and the server's
 * answer lands on the line it is about. The field is laid out by hand because Kumo's Field ties its
 * label and messages to a form control through Base UI, and CodeMirror's editable element is not
 * one: the ids here do that tying, and a click on the label focuses the editor.
 */
export function ExpressionEditor({
  label,
  description,
  value,
  onChange,
  placeholder = "",
  serverErrors,
}: ExpressionEditorProps): ReactElement {
  const problems = useMemo(() => serverLineProblems(value, serverErrors), [value, serverErrors]);
  const error = firstExpressionError(value, serverErrors);
  const id = useId();
  const editorId = `${id}-editor`;
  const noteId = `${id}-note`;
  const field = useRef<HTMLDivElement>(null);
  const attributes = useMemo(
    () => ({ id: editorId, "aria-describedby": noteId, "aria-invalid": String(error !== null) }),
    [editorId, noteId, error],
  );

  // A label's "for" only reaches form controls, so the click is wired to the editor by hand.
  useEffect(() => {
    const element = field.current?.querySelector("label");
    const focus = (): void => {
      field.current?.querySelector<HTMLElement>(".cm-content")?.focus();
    };

    element?.addEventListener("click", focus);

    return (): void => {
      element?.removeEventListener("click", focus);
    };
  }, []);

  return (
    <div ref={field} className={fieldVariants()}>
      <Label htmlFor={editorId}>{label}</Label>
      <div
        className={cn(
          "rounded-md bg-kumo-base ring ring-kumo-line focus-within:ring-[1.5px] focus-within:ring-kumo-focus/50",
          error === null ? "" : "ring-kumo-danger focus-within:ring-kumo-danger",
        )}
      >
        <CodeEditor
          minimal
          value={value}
          onChange={onChange}
          placeholder={placeholder}
          extensions={extensions}
          problems={problems}
          attributes={attributes}
          aria-label={label}
        />
      </div>
      {error === null ? (
        <p id={noteId} className="text-sm leading-snug text-kumo-subtle">
          {description}
        </p>
      ) : (
        <p id={noteId} role="alert" className="text-sm leading-snug text-kumo-danger">
          {error}
        </p>
      )}
    </div>
  );
}
