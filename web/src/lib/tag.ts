/** The prefix every ACL tag carries; the policy, the API and the client all spell it this way. */
export const tagPrefix = "tag:";

/** How many hues the tag palette has; see `hueOf` in `~/lib/hue.ts`. */
export const tagHueSteps = 12;

/**
 * The server takes a name that starts with a letter; the rest may be letters, digits, `.`, `_`,
 * `-`.
 */
const namePattern = /^[a-z][a-z0-9._-]*$/u;
/** Commas and whitespace, the two ways a list of tags gets pasted. */
const separators = /[\s,]+/u;

/** The prefix and the name apart, so the prefix can be drawn lighter than the name. */
export function splitTag(tag: string): { readonly prefix: string; readonly name: string } {
  return tag.startsWith(tagPrefix)
    ? { prefix: tagPrefix, name: tag.slice(tagPrefix.length) }
    : { prefix: "", name: tag };
}

/** What the operator typed as the server wants it: trimmed, lower-case, with the prefix. */
export function normalizeTag(text: string): string {
  const trimmed = text.trim().toLowerCase();

  if (trimmed === "") {
    return "";
  }

  return trimmed.startsWith(tagPrefix) ? trimmed : `${tagPrefix}${trimmed}`;
}

/** Why a tag is wrong, or null when the server will take it. Give it a normalized tag. */
export function tagError(tag: string): string | null {
  const { name } = splitTag(tag);

  if (name === "") {
    return "Enter a tag name, such as tag:server.";
  }

  if (/\s/u.test(name)) {
    return "A tag cannot contain spaces.";
  }

  if (!/^[a-z]/u.test(name)) {
    return "A tag name starts with a letter.";
  }

  if (!namePattern.test(name)) {
    return "A tag name is lower-case letters, digits, dots, dashes and underscores.";
  }

  return null;
}

/** A pasted list ("tag:ci, tag:prod" or one per line) as tags, each once, in the order given. */
export function parseTagList(text: string): string[] {
  const tags: string[] = [];

  for (const piece of text.split(separators)) {
    const tag = normalizeTag(piece);

    if (tag !== "" && !tags.includes(tag)) {
      tags.push(tag);
    }
  }

  return tags;
}

/** Every tag the sources mention, each once, in alphabetical order. */
export function knownTags(sources: readonly (readonly string[])[]): string[] {
  return [...new Set(sources.flat().filter((tag) => tag.startsWith(tagPrefix)))].toSorted(
    (left, right) => left.localeCompare(right),
  );
}
