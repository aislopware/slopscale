import { autocompletion } from "@codemirror/autocomplete";
import type { Completion, CompletionContext, CompletionResult } from "@codemirror/autocomplete";
import { LanguageSupport, StreamLanguage } from "@codemirror/language";
import { linter } from "@codemirror/lint";
import type { Diagnostic } from "@codemirror/lint";
import type { Extension, Line } from "@codemirror/state";
import type { EditorView } from "@codemirror/view";

import { lintDelay } from "~/lib/editor/problems.ts";
import { knownAttributes } from "~/lib/posture/attributes.ts";
import type { AttributeInfo } from "~/lib/posture/attributes.ts";
import { checkExpression } from "~/lib/posture/check.ts";
import { tokenize } from "~/lib/posture/tokens.ts";
import type { Token, TokenKind } from "~/lib/posture/tokens.ts";

/** The highlight style tag each token gets; the editor theme colours the tags. */
const tokenStyles: Readonly<Record<TokenKind, string | null>> = {
  prefix: "namespace",
  attribute: "variableName",
  operator: "keyword",
  string: "string",
  number: "number",
  bool: "bool",
  bracket: "squareBracket",
  comma: "separator",
  space: null,
  invalid: "invalid",
};

interface LineState {
  tokens: Token[];
  index: number;
}

/**
 * Posture expressions as a CodeMirror language: one expression per line, tokenised by the same
 * splitter the read-only rendering uses, so the editor and the table colour a line alike.
 */
export const postureLanguage = StreamLanguage.define<LineState>({
  name: "posture",
  startState: () => ({ tokens: [], index: 0 }),
  token: (stream, state) => {
    if (stream.sol()) {
      state.tokens = tokenize(stream.string);
      state.index = 0;
    }

    const token = state.tokens[state.index];

    if (token === undefined) {
      stream.skipToEnd();

      return null;
    }

    state.index += 1;
    stream.pos = token.to;

    return tokenStyles[token.kind];
  },
});

/** Problems from the parser and the checks, one line at a time, as the editor shows them. */
export function postureDiagnostics(view: EditorView): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];

  for (let number = 1; number <= view.state.doc.lines; number += 1) {
    const line = view.state.doc.line(number);

    const problems = line.text.trim() === "" ? [] : checkExpression(line.text);

    for (const problem of problems) {
      diagnostics.push({
        from: line.from + problem.from,
        to: line.from + Math.max(problem.to, problem.from + 1),
        severity: problem.severity,
        message: problem.message,
      });
    }
  }

  return diagnostics;
}

const attributeCompletions: readonly Completion[] = knownAttributes.map((attribute) => ({
  label: attribute.name,
  detail: attribute.type,
  info: attribute.doc,
  type: "variable",
}));

const operatorCompletions: readonly Completion[] = [
  { label: "==", detail: "equals", type: "keyword" },
  { label: "!=", detail: "does not equal", type: "keyword" },
  { label: ">=", detail: "at least, by version or number", type: "keyword" },
  { label: "<=", detail: "at most", type: "keyword" },
  { label: ">", detail: "above", type: "keyword" },
  { label: "<", detail: "below", type: "keyword" },
  { label: "IN", detail: "one of a list", type: "keyword", apply: "IN [" },
  { label: "NOT IN", detail: "none of a list", type: "keyword", apply: "NOT IN [" },
  { label: "IS SET", detail: "has any value", type: "keyword" },
  { label: "NOT SET", detail: "has no value", type: "keyword" },
];

const valueCompletions = (values: readonly string[]): Completion[] =>
  values.map((value) => ({ label: `'${value}'`, type: "text" }));

const boolCompletions: readonly Completion[] = [
  { label: "true", type: "constant" },
  { label: "false", type: "constant" },
];

function valueOptions(known: AttributeInfo | undefined): readonly Completion[] {
  if (known?.type === "bool") {
    return boolCompletions;
  }

  return known?.values === undefined ? [] : valueCompletions(known.values);
}

/** The tokens before the caret, split into the ones settled and the word the caret is still on. */
interface Caret {
  readonly line: Line;
  readonly settled: readonly Token[];
  readonly word: Token | undefined;
  /** Where a completion starts: the word's start, or the caret. */
  readonly from: number;
  readonly explicit: boolean;
}

function caretAt(context: CompletionContext): Caret {
  const line = context.state.doc.lineAt(context.pos);
  const before = line.text.slice(0, context.pos - line.from);
  const tokens = tokenize(before).filter((token) => token.kind !== "space");
  const last = tokens.at(-1);
  const word = last !== undefined && last.to === before.length ? last : undefined;

  return {
    line,
    settled: word === undefined ? tokens : tokens.slice(0, -1),
    word,
    from: line.from + (word?.from ?? before.length),
    explicit: context.explicit,
  };
}

function completeAttribute(caret: Caret, prefix: Token | undefined): CompletionResult | null {
  if (!caret.explicit && caret.word === undefined) {
    return null;
  }

  return {
    from: prefix === undefined ? caret.from : caret.line.from + prefix.from,
    options: [...attributeCompletions],
    validFor: /^[\w:.\-]*$/v,
  };
}

function completeValue(caret: Caret, name: string): CompletionResult | null {
  const known = knownAttributes.find((candidate) => candidate.name === name);
  const options = valueOptions(known);

  return options.length === 0
    ? null
    : { from: caret.from, options: [...options], validFor: /^['\w]*$/v };
}

/** What fits at the caret: an attribute at the start of a line, then an operator, then a value. */
function completePosture(context: CompletionContext): CompletionResult | null {
  const caret = caretAt(context);
  const { settled } = caret;
  const attribute = settled.find((token) => token.kind === "attribute");
  const prefix = settled.find((token) => token.kind === "prefix");

  if (settled.length === 0 || (settled.length === 1 && prefix !== undefined)) {
    return completeAttribute(caret, prefix);
  }

  if (attribute !== undefined && !settled.some((token) => token.kind === "operator")) {
    return { from: caret.from, options: [...operatorCompletions], validFor: /^[\w<>=!]*$/v };
  }

  return completeValue(caret, `${prefix?.text ?? ""}${attribute?.text ?? ""}`);
}

/** The language, its diagnostics and its completions, for the expression editor. */
export function posture(): Extension {
  return [
    new LanguageSupport(postureLanguage),
    linter(postureDiagnostics, { delay: lintDelay }),
    autocompletion({ override: [completePosture], icons: false }),
  ];
}
