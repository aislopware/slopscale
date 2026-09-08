import { syntaxTree } from "@codemirror/language";
import { hoverTooltip } from "@codemirror/view";
import type { Tooltip } from "@codemirror/view";
import type { SyntaxNode } from "@lezer/common";

import { collectNames } from "~/components/policy/lint.ts";
import { autogroupDocs, sectionInfo, sections } from "~/components/policy/schema.ts";
import type { KeyInfo } from "~/components/policy/schema.ts";
import { parseHujson, placeAt } from "~/lib/hujson/ast.ts";
import type { JsonNode, PathStep } from "~/lib/hujson/ast.ts";
import { attributeInfo } from "~/lib/posture/attributes.ts";

interface Doc {
  readonly name: string;
  readonly text: string;
}

function keyDoc(path: readonly PathStep[], name: string): Doc | null {
  const [section, second] = path;
  let keys: readonly KeyInfo[] | undefined = sections;

  if (section !== undefined) {
    const info = typeof section === "string" ? sectionInfo(section) : undefined;
    const inside = path.length === 1 ? info?.shape === "object" : typeof second === "number";

    keys = inside ? info?.keys : undefined;
  }

  const info = keys?.find((key) => key.name === name);

  return info === undefined ? null : { name: info.name, text: info.doc };
}

function sectionEntry(root: JsonNode, section: string, name: string): JsonNode | undefined {
  const object =
    root.kind === "object"
      ? root.entries.find((entry) => entry.key.name === section)?.value
      : undefined;

  return object?.kind === "object"
    ? object.entries.find((entry) => entry.key.name === name)?.value
    : undefined;
}

function listOf(node: JsonNode | undefined): string[] {
  return node?.kind === "array"
    ? node.items.flatMap((item) => (item.kind === "string" ? [item.value] : []))
    : [];
}

function count(number: number, noun: string): string {
  return `${number} ${noun}${number === 1 ? "" : "s"}`;
}

/** What a string means: an autogroup, a defined name, or an attribute inside a posture. */
function stringDoc(root: JsonNode, path: readonly PathStep[], value: string): Doc | null {
  const autogroup = autogroupDocs[value];

  if (autogroup !== undefined) {
    return { name: value, text: autogroup };
  }

  const names = collectNames(root);

  if (value.startsWith("group:") && names.groups.has(value)) {
    return {
      name: value,
      text: `A group of ${count(listOf(sectionEntry(root, "groups", value)).length, "user")}.`,
    };
  }

  if (value.startsWith("tag:") && names.tags.has(value)) {
    return {
      name: value,
      text: `Owned by ${listOf(sectionEntry(root, "tagOwners", value)).join(", ") || "nobody"}.`,
    };
  }

  if (value.startsWith("posture:") && names.postures.has(value)) {
    return {
      name: value,
      text: listOf(sectionEntry(root, "postures", value)).join("\n") || "No expressions.",
    };
  }

  if (path[0] === "postures") {
    const info = attributeInfo(value.split(" ")[0] ?? "");

    return info === null || info === undefined ? null : { name: info.name, text: info.doc };
  }

  const host = names.hosts.has(value) ? sectionEntry(root, "hosts", value) : undefined;

  return host?.kind === "string" ? { name: value, text: `A host for ${host.value}.` } : null;
}

function docAt(root: JsonNode, node: SyntaxNode, text: string): Doc | null {
  if (node.name === "PropertyName") {
    const place = placeAt(root, node.from);

    return place.key === undefined ? null : keyDoc(place.path, place.key.name);
  }

  if (node.name === "String") {
    const place = placeAt(root, node.from);
    const raw = text.slice(node.from + 1, node.to - (text[node.to - 1] === '"' ? 1 : 0));

    return stringDoc(root, place.path, raw);
  }

  return null;
}

function render(doc: Doc): HTMLElement {
  const dom = document.createElement("div");
  const name = document.createElement("div");
  const body = document.createElement("div");

  dom.className = "cm-policy-hover";
  name.className = "cm-policy-hover-name";
  name.textContent = doc.name;
  body.textContent = doc.text;
  dom.append(name, body);

  return dom;
}

/** Explains the key or the name under the pointer, from the schema and the file itself. */
export const policyHover = hoverTooltip((view, pos): Tooltip | null => {
  const node = syntaxTree(view.state).resolveInner(pos, 1);

  if (node.name !== "PropertyName" && node.name !== "String") {
    return null;
  }

  const text = view.state.doc.toString();
  const doc = docAt(parseHujson(text).root, node, text);

  return doc === null
    ? null
    : { pos: node.from, end: node.to, above: true, create: () => ({ dom: render(doc) }) };
});
