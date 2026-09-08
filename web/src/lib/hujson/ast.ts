import type { SyntaxNode, Tree } from "@lezer/common";

import { hujsonLanguage } from "~/lib/hujson/language.ts";

/** Where a node sits in the document, as offsets. */
export interface Span {
  readonly from: number;
  readonly to: number;
}

export interface JsonKey extends Span {
  readonly name: string;
}

export interface JsonEntry {
  readonly key: JsonKey;
  readonly value: JsonNode;
}

export interface JsonObject extends Span {
  readonly kind: "object";
  readonly entries: readonly JsonEntry[];
}

export interface JsonArray extends Span {
  readonly kind: "array";
  readonly items: readonly JsonNode[];
}

export interface JsonString extends Span {
  readonly kind: "string";
  readonly value: string;
}

export interface JsonNumber extends Span {
  readonly kind: "number";
  readonly value: number;
}

export interface JsonBool extends Span {
  readonly kind: "bool";
  readonly value: boolean;
}

export interface JsonNull extends Span {
  readonly kind: "null";
}

/** Text the parser could not place: a value is missing or something unexpected stands there. */
export interface JsonError extends Span {
  readonly kind: "error";
}

export type JsonNode =
  | JsonObject
  | JsonArray
  | JsonString
  | JsonNumber
  | JsonBool
  | JsonNull
  | JsonError;

/** A syntax error, with where it is and what to say about it. */
export interface SyntaxIssue extends Span {
  readonly message: string;
}

export interface Parsed {
  /** The document's value; an error node when the document is empty or does not start with one. */
  readonly root: JsonNode;
  readonly issues: readonly SyntaxIssue[];
}

/** Turns a string literal's source into its value, or the raw inside when the escapes are broken. */
function stringValue(source: string): string {
  try {
    const parsed: unknown = JSON.parse(source);

    return typeof parsed === "string" ? parsed : source;
  } catch {
    return source.slice(1, source.endsWith('"') && source.length > 1 ? -1 : undefined);
  }
}

const errorPreview = 12;

function issueOf(node: SyntaxNode, text: string): SyntaxIssue {
  if (node.from === node.to) {
    return {
      from: node.from,
      to: node.to,
      message:
        node.from >= text.length
          ? "Unexpected end of file"
          : "Missing a value, a comma or a closing bracket here",
    };
  }

  const skipped = text.slice(node.from, Math.min(node.to, node.from + errorPreview));

  return {
    from: node.from,
    to: node.to,
    message: `Unexpected "${skipped}${node.to - node.from > errorPreview ? "…" : ""}"`,
  };
}

function collectIssues(tree: Tree, text: string): SyntaxIssue[] {
  const issues: SyntaxIssue[] = [];

  tree.iterate({
    enter: (node) => {
      if (node.type.isError) {
        issues.push(issueOf(node.node, text));
      }
    },
  });

  return issues;
}

function keyOf(node: SyntaxNode, text: string): JsonKey {
  return { from: node.from, to: node.to, name: stringValue(text.slice(node.from, node.to)) };
}

const valueNames = new Set(["Object", "Array", "String", "Number", "True", "False", "Null"]);

/** The ":" after a property name is a sibling too; the value is what follows it. */
function skipPunctuation(node: SyntaxNode): SyntaxNode {
  let current: SyntaxNode | null = node;

  while (current !== null && !valueNames.has(current.name) && !current.type.isError) {
    current = current.nextSibling;
  }

  return current ?? node;
}

function valueChildren(node: SyntaxNode): SyntaxNode[] {
  const children: SyntaxNode[] = [];

  for (let child = node.firstChild; child !== null; child = child.nextSibling) {
    if (valueNames.has(child.name)) {
      children.push(child);
    }
  }

  return children;
}

type Converter = (node: SyntaxNode, text: string, span: Span) => JsonNode;

function convertObject(node: SyntaxNode, text: string, span: Span): JsonNode {
  const entries: JsonEntry[] = [];

  for (const property of node.getChildren("Property")) {
    const name = property.getChild("PropertyName");

    if (name !== null) {
      const valueNode = name.nextSibling;
      const value: JsonNode =
        valueNode === null || valueNode.type.isError
          ? { kind: "error", from: name.to, to: property.to }
          : convert(skipPunctuation(valueNode), text);

      entries.push({ key: keyOf(name, text), value });
    }
  }

  return { kind: "object", ...span, entries };
}

const converters: Readonly<Record<string, Converter>> = {
  Object: convertObject,
  Array: (node, text, span) => ({
    kind: "array",
    ...span,
    items: valueChildren(node).map((child) => convert(child, text)),
  }),
  String: (node, text, span) => ({
    kind: "string",
    ...span,
    value: stringValue(text.slice(node.from, node.to)),
  }),
  Number: (node, text, span) => ({
    kind: "number",
    ...span,
    value: Number(text.slice(node.from, node.to)),
  }),
  True: (_node, _text, span) => ({ kind: "bool", ...span, value: true }),
  False: (_node, _text, span) => ({ kind: "bool", ...span, value: false }),
  Null: (_node, _text, span) => ({ kind: "null", ...span }),
};

function convert(node: SyntaxNode, text: string): JsonNode {
  const span = { from: node.from, to: node.to };
  const converter = converters[node.name];

  return converter === undefined ? { kind: "error", ...span } : converter(node, text, span);
}

/**
 * Parses a HuJSON document into a tree with positions. The parser recovers from errors, so a draft
 * mid-edit still yields the parts that parse, with the broken spots as error nodes and issues.
 */
export function parseHujson(text: string): Parsed {
  const tree = hujsonLanguage.parser.parse(text);
  const [top] = valueChildren(tree.topNode);

  return {
    root: top === undefined ? { kind: "error", from: 0, to: text.length } : convert(top, text),
    issues: collectIssues(tree, text),
  };
}

/** One step down the tree: an object key or an array index. */
export type PathStep = string | number;

export interface Place {
  /** The keys and indices from the root down to the innermost value around the position. */
  readonly path: readonly PathStep[];
  /** The innermost value around the position. */
  readonly node: JsonNode;
  /** The key the position is on, when it is on a property name rather than a value. */
  readonly key?: JsonKey;
}

function contains(span: Span, pos: number): boolean {
  return span.from <= pos && pos <= span.to;
}

/** Where a position is in the tree: the value around it and the path down to it. */
export function placeAt(root: JsonNode, pos: number): Place {
  const path: PathStep[] = [];
  let node = root;

  for (;;) {
    if (node.kind === "object") {
      const entry = node.entries.find(
        (candidate) => contains(candidate.key, pos) || contains(candidate.value, pos),
      );

      if (entry === undefined) {
        return { path, node };
      }

      if (contains(entry.key, pos) && !contains(entry.value, pos)) {
        return { path, node, key: entry.key };
      }

      path.push(entry.key.name);
      node = entry.value;
    } else if (node.kind === "array") {
      const index = node.items.findIndex((item) => contains(item, pos));

      if (index === -1) {
        return { path, node };
      }

      path.push(index);
      node = node.items[index] ?? node;
    } else {
      return { path, node };
    }
  }
}
