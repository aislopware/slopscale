import { attributeInfo, osValues } from "~/lib/posture/attributes.ts";
import type { AttributeInfo } from "~/lib/posture/attributes.ts";
import { parseExpression } from "~/lib/posture/expression.ts";
import type { Expression, Value } from "~/lib/posture/expression.ts";

export interface ExpressionProblem {
  readonly severity: "error" | "warning";
  readonly message: string;
  readonly from: number;
  readonly to: number;
}

const orderedOperators = new Set(["<", "<=", ">", ">="]);
const countryCode = /^[A-Z]{2}$/v;

function scalars(value: Value): Value[] {
  return value.kind === "list" ? [...value.items] : [value];
}

function checkKnownValues(info: AttributeInfo, value: Value, into: ExpressionProblem[]): void {
  const known = info.values;

  if (known === undefined) {
    return;
  }

  for (const item of scalars(value)) {
    if (item.kind === "string" && !known.includes(item.value)) {
      into.push({
        severity: "warning",
        message:
          info.name === "node:os"
            ? `${info.name} is one of ${osValues.join(", ")}, in lowercase`
            : `${info.name} is one of ${known.join(", ")}`,
        from: item.from,
        to: item.to,
      });
    }
  }
}

function checkValueType(info: AttributeInfo, value: Value, into: ExpressionProblem[]): void {
  for (const item of scalars(value)) {
    if (info.type === "bool" && item.kind !== "bool") {
      into.push({
        severity: "warning",
        message: `${info.name} is true or false, not a string or number`,
        from: item.from,
        to: item.to,
      });
    } else if (info.type !== "bool" && item.kind === "bool") {
      into.push({
        severity: "warning",
        message: `${info.name} is not true or false`,
        from: item.from,
        to: item.to,
      });
    } else if (info.type === "country" && item.kind === "string" && !countryCode.test(item.value)) {
      into.push({
        severity: "warning",
        message: "A country is a two-letter code in capitals, such as VN",
        from: item.from,
        to: item.to,
      });
    }
  }
}

function checkAttribute(expression: Expression, into: ExpressionProblem[]): void {
  const { attribute, attributeSpan, operator, value } = expression;
  const info = attributeInfo(attribute);

  if (attribute.startsWith("custom:")) {
    if (!/^custom:[\w\-]{1,50}$/v.test(attribute)) {
      into.push({
        severity: "warning",
        message:
          "A custom attribute name is custom: and up to 50 letters, digits, underscores or dashes",
        ...attributeSpan,
      });
    }

    return;
  }

  if (info === undefined) {
    into.push({
      severity: "warning",
      message: `${attribute} is not an attribute the server reports, so it is never set`,
      ...attributeSpan,
    });

    return;
  }

  if (value === undefined) {
    return;
  }

  if (orderedOperators.has(operator) && info.type !== "version" && info.type !== "string") {
    into.push({
      severity: "warning",
      message: `${attribute} is not a version or a number, so ${operator} never matches`,
      ...attributeSpan,
    });
  }

  checkKnownValues(info, value, into);
  checkValueType(info, value, into);
}

/**
 * Everything worth saying about one expression: the parse error the server would return, or the
 * warnings the server never gives, such as an attribute it does not report, a value node:os never
 * takes or a comparison that can never hold. Positions are offsets into the text.
 */
export function checkExpression(text: string): ExpressionProblem[] {
  const result = parseExpression(text);

  if (!result.ok) {
    return [{ severity: "error", ...result.error }];
  }

  const problems: ExpressionProblem[] = [];

  checkAttribute(result.expression, problems);

  return problems;
}
