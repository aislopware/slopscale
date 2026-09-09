import { isGoFloat } from "~/lib/posture/number.ts";

/** The operators, longest first so `<=` wins over `<` and `NOT IN` over `IN`. */
export const operators = [
  "NOT IN",
  "IS SET",
  "NOT SET",
  "<=",
  ">=",
  "==",
  "!=",
  "<",
  ">",
  "IN",
] as const;

export type Operator = (typeof operators)[number];

export type TokenKind =
  | "prefix"
  | "attribute"
  | "operator"
  | "string"
  | "number"
  | "bool"
  | "bracket"
  | "comma"
  | "space"
  | "invalid";

export interface Token {
  readonly kind: TokenKind;
  readonly from: number;
  readonly to: number;
  readonly text: string;
}

export function isSpace(char: string): boolean {
  return /\s/v.test(char);
}

export function isAttributeChar(char: string): boolean {
  return /[\w:.\-]/v.test(char);
}

/**
 * Whether the text at `at` starts with the operator. Word operators match regardless of case and
 * must end at a word boundary, so `INX` is not `IN`.
 */
export function hasOperator(text: string, at: number, op: Operator): boolean {
  const slice = text.slice(at, at + op.length);

  if (slice.length < op.length || slice.toUpperCase() !== op) {
    return false;
  }

  const next = text[at + op.length];

  return !(/[a-zA-Z]/v.test(op) && next !== undefined && isAttributeChar(next));
}

/**
 * Splits an expression into coloured tokens, whether or not it parses: an editor colours what it
 * can and the parser says what is wrong. Every character lands in exactly one token.
 */
export function tokenize(text: string): Token[] {
  const tokens: Token[] = [];
  let at = 0;

  const push = (kind: TokenKind, to: number): void => {
    tokens.push({ kind, from: at, to, text: text.slice(at, to) });
    at = to;
  };

  while (at < text.length) {
    const char = text[at] ?? "";

    if (isSpace(char)) {
      let to = at;

      while (to < text.length && isSpace(text[to] ?? "")) {
        to += 1;
      }

      push("space", to);
    } else if (char === "[" || char === "]") {
      push("bracket", at + 1);
    } else if (char === ",") {
      push("comma", at + 1);
    } else if (char === "'" || char === '"') {
      push(...quotedToken(text, at));
    } else if (tokens.length === 0 && isAttributeChar(char)) {
      const to = attributeEnd(text, at);
      const colon = text.slice(at, to).indexOf(":");

      if (colon > 0) {
        push("prefix", at + colon + 1);
      }

      push("attribute", to);
    } else {
      const op = operatorAt(text, at);

      if (op === undefined) {
        push(...bareToken(text, at));
      } else {
        push("operator", at + op.length);
      }
    }
  }

  return tokens;
}

function operatorAt(text: string, at: number): Operator | undefined {
  return operators.find((candidate) => hasOperator(text, at, candidate));
}

function attributeEnd(text: string, from: number): number {
  let to = from;

  while (to < text.length && isAttributeChar(text[to] ?? "")) {
    to += 1;
  }

  return to;
}

function quotedToken(text: string, from: number): [TokenKind, number] {
  const quote = text[from];
  let at = from + 1;

  while (at < text.length) {
    const char = text[at];

    at += 1;

    if (char === "\\") {
      at += 1;
    } else if (char === quote) {
      return ["string", at];
    }
  }

  return ["invalid", text.length];
}

function bareToken(text: string, from: number): [TokenKind, number] {
  let to = from;

  while (to < text.length) {
    const char = text[to] ?? "";

    if (char === "," || char === "]" || char === "[" || isSpace(char)) {
      break;
    }

    to += 1;
  }

  if (to === from) {
    to = from + 1;
  }

  const word = text.slice(from, to).toLowerCase();

  if (word === "true" || word === "false") {
    return ["bool", to];
  }

  return [isGoFloat(word) ? "number" : "invalid", to];
}
