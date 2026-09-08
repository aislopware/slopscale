import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

import { tokenize } from "~/lib/posture/tokens.ts";
import type { TokenKind } from "~/lib/posture/tokens.ts";

/** The same weights the editor gives its tokens, so an expression reads alike in a table. */
const tokenClasses: Readonly<Record<TokenKind, string>> = {
  prefix: "text-kumo-subtle",
  attribute: "text-kumo-default",
  operator: "font-semibold text-kumo-default",
  string: "text-kumo-link",
  number: "text-kumo-success",
  bool: "text-kumo-warning",
  bracket: "text-kumo-subtle",
  comma: "text-kumo-subtle",
  space: "",
  invalid: "text-kumo-danger underline decoration-wavy underline-offset-3",
};

/** A posture expression coloured by token, for a table cell or a badge. */
export function ExpressionText({
  text,
  className,
}: {
  readonly text: string;
  readonly className?: string;
}): ReactElement {
  return (
    <code className={cn("font-mono", className)}>
      {tokenize(text).map((token) => (
        <span key={token.from} className={tokenClasses[token.kind]}>
          {token.text}
        </span>
      ))}
    </code>
  );
}

function valueClass(value: unknown): string {
  if (typeof value === "string") {
    return tokenClasses.string;
  }

  if (typeof value === "number") {
    return tokenClasses.number;
  }

  return typeof value === "boolean" ? tokenClasses.bool : tokenClasses.attribute;
}

function literal(value: unknown): string {
  if (typeof value === "string") {
    return `'${value}'`;
  }

  return typeof value === "number" || typeof value === "boolean"
    ? String(value)
    : JSON.stringify(value);
}

/** A custom attribute the way a posture would name it: `custom:oncall = true`, coloured alike. */
export function AttributeText({
  name,
  value,
  className,
}: {
  readonly name: string;
  readonly value: unknown;
  readonly className?: string;
}): ReactElement {
  const colon = name.indexOf(":");
  const prefix = colon === -1 ? "" : name.slice(0, colon + 1);
  const values = Array.isArray(value) ? value : [value];

  return (
    <code className={cn("font-mono", className)}>
      <span className={tokenClasses.prefix}>{prefix}</span>
      <span className={tokenClasses.attribute}>{name.slice(prefix.length)}</span>
      <span className={tokenClasses.operator}> = </span>
      {values.map((item, index) => (
        // A value has no id of its own; the position is what tells two equal items apart.
        // eslint-disable-next-line react/no-array-index-key
        <span key={index}>
          {index === 0 ? null : <span className={tokenClasses.comma}>, </span>}
          <span className={valueClass(item)}>{literal(item)}</span>
        </span>
      ))}
    </code>
  );
}
