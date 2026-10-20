import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

/**
 * One chip size everywhere, the tag input's (design spec §7.1): 20px tall, 13px text. A dense table
 * once drew 18px chips at 12px, smaller than the tags the operator typed in; `sm` stays as a name
 * for the callers that ask for it.
 */
const chipSize = "h-5 px-2 text-sm";
const sizes = { sm: chipSize, base: chipSize } as const;

export type TagSize = keyof typeof sizes;

/**
 * The Cloudflare dashboard's tag chip, the one its tag input draws: a 20px pill in the info tint
 * with the text in the info blue (light blue on dark blue in dark mode). Every tag is the same
 * colour; the name tells them apart. Shared with the tag field's chips, so a tag looks the same
 * being entered and being shown.
 */
export const tagChipClass = "rounded-full bg-kumo-info-tint text-kumo-info";

/**
 * The words of a tag in one colour, prefix and name alike, the way the dashboard's tag input draws
 * its chips; the name already tells two tags apart. Used inside `Tag` and inside a picker's chips.
 */
export function TagText({ tag }: { readonly tag: string }): ReactElement {
  return <span className="truncate">{tag}</span>;
}

/**
 * An ACL tag as the dashboard's tag chip. A tag is the one identifier the console draws as a chip:
 * it is a label by nature, the policy hands it out and the machines wear it, whereas a domain or a
 * route is a value and stays text.
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
        "inline-flex max-w-full min-w-0 shrink items-center leading-none whitespace-nowrap",
        tagChipClass,
        sizes[size],
        className,
      )}
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
