import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

import { seededColours } from "~/lib/hue.ts";
import { splitTag, tagHueSteps } from "~/lib/tag.ts";

const sizes = {
  /** For the second line of a table cell and a dense list. */
  sm: "h-5 px-1.5 text-xs",
  /** For a page header, a definition list and a form. */
  base: "h-6 px-2 text-sm",
} as const;

export type TagSize = keyof typeof sizes;

/** The chip's colours for a tag: the same tag is the same hue on every page. */
export function tagColours(tag: string): ReturnType<typeof seededColours> {
  return seededColours(tag, tagHueSteps);
}

/**
 * The words of a tag with the `tag:` prefix stepped back, so a row of tags reads by its names and
 * the prefix still says what kind of thing it is. Used inside `Tag` and inside a picker's chips.
 */
export function TagText({ tag }: { readonly tag: string }): ReactElement {
  const { prefix, name } = splitTag(tag);

  return (
    <span className="truncate">
      {prefix === "" ? null : <span className="opacity-60">{prefix}</span>}
      {name}
    </span>
  );
}

/**
 * An ACL tag as a chip in a tint seeded by its name, so `tag:prod` is the same colour in every
 * table, page and picker and two tags tell apart before they are read. A tag is the one identifier
 * the console draws as a chip: it is a label by nature, the policy hands it out and the machines
 * wear it, whereas a domain or a route is a value and stays text. The corner radius is the
 * controls', not a pill's, so it sits with the badges and inputs around it.
 */
export function Tag({
  tag,
  size = "base",
  className,
}: {
  readonly tag: string;
  readonly size?: TagSize;
  readonly className?: string;
}): ReactElement {
  return (
    <span
      title={tag}
      className={cn(
        "inline-flex max-w-full min-w-0 shrink items-center rounded-md leading-none font-medium whitespace-nowrap ring ring-kumo-line ring-inset",
        sizes[size],
        className,
      )}
      style={tagColours(tag)}
    >
      <TagText tag={tag} />
    </span>
  );
}

/**
 * A machine's or a key's tags, wrapped in a row. In a table `max` keeps the row to a line and
 * counts the rest, with the names one hover away.
 */
export function TagList({
  tags,
  size = "base",
  max,
  empty = "None",
  className,
}: {
  readonly tags: readonly string[];
  readonly size?: TagSize;
  /** How many tags show before the rest are counted; every one when absent. */
  readonly max?: number;
  /** What stands where the list would be when it is empty. */
  readonly empty?: string;
  readonly className?: string;
}): ReactElement {
  if (tags.length === 0) {
    return <span className="text-kumo-subtle">{empty}</span>;
  }

  const shown = max === undefined ? tags : tags.slice(0, max);
  const hidden = tags.slice(shown.length);

  return (
    <ul className={cn("flex min-w-0 flex-wrap items-center gap-1", className)}>
      {shown.map((tag) => (
        <li key={tag} className="flex min-w-0">
          <Tag tag={tag} size={size} />
        </li>
      ))}
      {hidden.length === 0 ? null : (
        <li className="text-xs text-kumo-subtle">
          <Tooltip content={hidden.join(", ")}>
            <span>{`+${hidden.length} more`}</span>
          </Tooltip>
        </li>
      )}
    </ul>
  );
}
