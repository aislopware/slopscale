import type { EditorProblem } from "~/lib/editor/problems.ts";

const hujsonPlace = /hujson: line (?<line>\d+), column (?<column>\d+)/v;
const quoted = /"(?<name>[^"\n]+)"/gv;

function lineStart(text: string, line: number): number {
  let at = 0;

  for (let number = 1; number < line; number += 1) {
    const next = text.indexOf("\n", at);

    if (next === -1) {
      return at;
    }

    at = next + 1;
  }

  return at;
}

function lineEnd(text: string, from: number): number {
  const next = text.indexOf("\n", from);

  return next === -1 ? text.length : next;
}

/**
 * Where in the draft the server's refusal points. A syntax error names its line and column; a
 * policy error usually quotes the name it objects to, which is found in the text; anything else is
 * pinned to the first line so the gutter still shows it.
 */
export function serverProblems(text: string, message: string): EditorProblem[] {
  const place = hujsonPlace.exec(message)?.groups;

  if (place !== undefined) {
    const from = Math.min(
      text.length,
      lineStart(text, Number(place["line"])) + Number(place["column"]) - 1,
    );

    return [{ from, to: Math.min(text.length, from + 1), severity: "error", message }];
  }

  for (const match of message.matchAll(quoted)) {
    const name = match.groups?.["name"] ?? "";
    const literal = `"${name}"`;
    const at = text.indexOf(literal);

    if (at !== -1) {
      return [{ from: at, to: at + literal.length, severity: "error", message }];
    }
  }

  return [{ from: 0, to: lineEnd(text, 0), severity: "error", message }];
}
