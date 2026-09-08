import { linter } from "@codemirror/lint";
import type { Diagnostic } from "@codemirror/lint";
import { StateEffect, StateField } from "@codemirror/state";
import type { Extension, Transaction } from "@codemirror/state";

/** A problem in the document, by offsets, from whatever found it: a linter, the server, a parser. */
export interface EditorProblem {
  readonly from: number;
  readonly to: number;
  readonly severity: "error" | "warning" | "info";
  readonly message: string;
}

/**
 * How long the editor waits after a change before it asks its linters again. Every linter in an
 * editor must ask for the same delay, because CodeMirror refuses two different ones.
 */
export const lintDelay = 150;

/** Replaces the problems handed to the editor from outside. */
export const setProblems = StateEffect.define<readonly EditorProblem[]>();

function toDiagnostic(problem: EditorProblem): Diagnostic {
  return {
    from: problem.from,
    to: Math.max(problem.from, problem.to),
    severity: problem.severity,
    message: problem.message,
  };
}

function mapThrough(diagnostics: readonly Diagnostic[], transaction: Transaction): Diagnostic[] {
  return diagnostics.map((diagnostic) => {
    const from = transaction.changes.mapPos(diagnostic.from);

    return {
      ...diagnostic,
      from,
      to: Math.max(from, transaction.changes.mapPos(diagnostic.to, 1)),
    };
  });
}

/**
 * The problems that came from outside the editor, such as the server's answer to a check. They
 * follow the text through later edits until the next batch replaces them.
 */
const externalProblemsField = StateField.define<readonly Diagnostic[]>({
  create: () => [],
  update: (value, transaction) => {
    const set = transaction.effects.findLast((effect) => effect.is(setProblems));

    if (set !== undefined && set.is(setProblems)) {
      return set.value.map(toDiagnostic);
    }

    return transaction.docChanged ? mapThrough(value, transaction) : value;
  },
});

/** Shows the problems pushed in with {@link setProblems} as diagnostics, beside any linter's own. */
export function externalProblems(): Extension {
  return [
    externalProblemsField,
    linter((view) => [...view.state.field(externalProblemsField)], {
      delay: lintDelay,
      needsRefresh: (update) =>
        update.transactions.some((transaction) =>
          transaction.effects.some((effect) => effect.is(setProblems)),
        ),
    }),
  ];
}
