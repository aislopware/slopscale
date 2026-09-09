/**
 * Posture expressions, as the server's `hscontrol/posture` package parses them: an attribute, an
 * operator and a value, such as `node:os IN ['macos', 'linux']` or `custom:oncall == true`. This is
 * a port of that parser with positions kept, so an editor can colour every token and underline the
 * exact spot the server would refuse. The server stays the authority; the messages are its own.
 */

import { ExpressionFailureError } from "~/lib/posture/failure.ts";
import { parseGoFloat } from "~/lib/posture/number.ts";
import { hasOperator, isAttributeChar, isSpace, operators } from "~/lib/posture/tokens.ts";
import type { Operator } from "~/lib/posture/tokens.ts";

export const prefixes = [
  "node",
  "custom",
  "ip",
  "falcon",
  "sentinelOne",
  "intune",
  "jamfPro",
  "kandji",
  "kolide",
] as const;

export const errors = {
  empty: "posture expression is empty",
  attribute: "posture expression must start with an attribute such as node:os",
  operator: "posture expression has no operator",
  value: "posture expression has no value",
  trailing: "posture expression has text after the value",
  list: "IN and NOT IN need a list such as ['a', 'b'] of values",
  scalar: "this operator takes a single value, not a list",
  ordered: "<, <=, > and >= compare numbers or version strings",
  unterminated: "unterminated string in posture expression",
  prefix:
    "attribute prefix must be one of node, custom, ip or an integration's: falcon, sentinelOne, intune, jamfPro, kandji, kolide",
} as const;

export interface ExpressionError {
  readonly message: string;
  readonly from: number;
  readonly to: number;
}

export type Value =
  | { readonly kind: "string"; readonly value: string; readonly from: number; readonly to: number }
  | { readonly kind: "number"; readonly value: number; readonly from: number; readonly to: number }
  | { readonly kind: "bool"; readonly value: boolean; readonly from: number; readonly to: number }
  | {
      readonly kind: "list";
      readonly items: readonly Value[];
      readonly from: number;
      readonly to: number;
    };

export interface Expression {
  readonly attribute: string;
  readonly attributeSpan: { readonly from: number; readonly to: number };
  readonly operator: Operator;
  readonly value?: Value;
}

export type ParseResult =
  | { readonly ok: true; readonly expression: Expression }
  | { readonly ok: false; readonly error: ExpressionError };

function fail(message: string, from: number, to: number): never {
  throw new ExpressionFailureError({ message, from, to: Math.max(from, to) });
}

class Parser {
  readonly text: string;
  at = 0;

  constructor(text: string) {
    this.text = text;
  }

  skipSpace(): void {
    while (this.at < this.text.length && isSpace(this.text[this.at] ?? "")) {
      this.at += 1;
    }
  }

  done(): boolean {
    this.skipSpace();

    return this.at >= this.text.length;
  }

  attribute(): { readonly name: string; readonly from: number; readonly to: number } {
    this.skipSpace();

    const start = this.at;

    while (this.at < this.text.length && isAttributeChar(this.text[this.at] ?? "")) {
      this.at += 1;
    }

    const name = this.text.slice(start, this.at);
    const colon = name.indexOf(":");

    if (colon <= 0) {
      fail(errors.attribute, start, Math.max(this.at, start + 1));
    }

    const prefix = name.slice(0, colon);

    if (!prefixes.some((known) => known === prefix)) {
      fail(`${errors.prefix}, got "${prefix}"`, start, start + colon);
    }

    return { name, from: start, to: this.at };
  }

  operator(): Operator {
    this.skipSpace();

    for (const op of operators) {
      if (hasOperator(this.text, this.at, op)) {
        this.at += op.length;

        return op;
      }
    }

    return fail(errors.operator, this.at, this.text.length);
  }

  value(): Value {
    this.skipSpace();

    if (this.at >= this.text.length) {
      fail(errors.value, this.at, this.text.length);
    }

    const char = this.text[this.at];

    if (char === "[") {
      return this.list();
    }

    if (char === "'" || char === '"') {
      return this.quoted();
    }

    return this.bare();
  }

  list(): Value {
    const from = this.at;

    this.at += 1;

    const items: Value[] = [];

    for (;;) {
      this.skipSpace();

      if (this.at >= this.text.length) {
        fail(errors.value, from, this.text.length);
      }

      if (this.text[this.at] === "]") {
        this.at += 1;

        return { kind: "list", items, from, to: this.at };
      }

      if (items.length > 0) {
        if (this.text[this.at] !== ",") {
          fail(errors.value, this.at, this.at + 1);
        }

        this.at += 1;
        this.skipSpace();
      }

      const item = this.value();

      if (item.kind === "list") {
        fail(errors.scalar, item.from, item.to);
      }

      items.push(item);
    }
  }

  quoted(): Value {
    const from = this.at;
    const quote = this.text[this.at];
    let value = "";

    this.at += 1;

    while (this.at < this.text.length) {
      const char = this.text[this.at] ?? "";

      this.at += 1;

      if (char === "\\" && this.at < this.text.length) {
        value += this.text[this.at] ?? "";
        this.at += 1;
      } else if (char === quote) {
        return { kind: "string", value, from, to: this.at };
      } else {
        value += char;
      }
    }

    return fail(errors.unterminated, from, this.text.length);
  }

  bare(): Value {
    const from = this.at;

    while (this.at < this.text.length) {
      const char = this.text[this.at] ?? "";

      if (char === "," || char === "]" || isSpace(char)) {
        break;
      }

      this.at += 1;
    }

    const word = this.text.slice(from, this.at);
    const lower = word.toLowerCase();

    if (lower === "") {
      fail(errors.value, from, from + 1);
    }

    if (lower === "true" || lower === "false") {
      return { kind: "bool", value: lower === "true", from, to: this.at };
    }

    const number = parseGoFloat(word);

    if (number === null) {
      fail(
        `${errors.value}: "${word}" is not a number, true, false or a quoted string`,
        from,
        this.at,
      );
    }

    return { kind: "number", value: number, from, to: this.at };
  }
}

const orderedOperators = new Set<Operator>(["<", "<=", ">", ">="]);
const listOperators = new Set<Operator>(["IN", "NOT IN"]);

function parseValue(parser: Parser, expression: Expression): Expression {
  const value = parser.value();

  if (listOperators.has(expression.operator)) {
    if (value.kind !== "list") {
      fail(errors.list, value.from, value.to);
    }
  } else if (value.kind === "list") {
    fail(errors.scalar, value.from, value.to);
  } else if (value.kind === "bool" && orderedOperators.has(expression.operator)) {
    fail(errors.ordered, value.from, value.to);
  }

  return { ...expression, value };
}

/** Parses one expression; positions in the result and the error are offsets into `text`. */
export function parseExpression(text: string): ParseResult {
  const parser = new Parser(text);

  try {
    if (parser.done()) {
      fail(errors.empty, 0, text.length);
    }

    const attribute = parser.attribute();
    const operator = parser.operator();
    let expression: Expression = {
      attribute: attribute.name,
      attributeSpan: { from: attribute.from, to: attribute.to },
      operator,
    };

    if (operator !== "IS SET" && operator !== "NOT SET") {
      expression = parseValue(parser, expression);
    }

    if (!parser.done()) {
      fail(errors.trailing, parser.at, text.length);
    }

    return { ok: true, expression };
  } catch (error) {
    if (error instanceof ExpressionFailureError) {
      return { ok: false, error: error.detail };
    }

    throw error;
  }
}
