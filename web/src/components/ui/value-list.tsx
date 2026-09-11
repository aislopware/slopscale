import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

/** One value of a list, or a value with how it reads: in the code face, or stepped back. */
export type ValueItem =
  | string
  | {
      readonly value: string;
      /** Shown as code: a route, a scope as the API spells it. */
      readonly mono?: boolean;
      /** Stepped back to subtle, such as a network that is switched off. */
      readonly muted?: boolean;
    };

const asIs = (value: string): string => value;

function itemOf(item: ValueItem): Exclude<ValueItem, string> {
  return typeof item === "string" ? { value: item } : item;
}

export interface ValueListProps {
  readonly items: readonly ValueItem[];
  /** What stands where the list would be when it is empty. */
  readonly empty?: string;
  /** How many values show before the rest are counted; every one when absent. */
  readonly max?: number;
  /** Every value in the code face; an item's own flag still wins. */
  readonly mono?: boolean;
  /** Names a value for the reader, such as a scope by its console name. */
  readonly label?: (value: string) => string;
  /**
   * Cuts each value at the list's width instead of wrapping it, for a cell that holds free text
   * rather than a name. The caller bounds the width, or the column grows to the longest value.
   */
  readonly truncate?: boolean;
  readonly className?: string;
}

/**
 * Identifiers as text, one per line: groups, scopes, routes, ports. A row of pills said the same
 * thing louder and had to truncate to fit, which hid the part that told two values apart. A long
 * value wraps within the line rather than widening the column it sits in. A tag is `Tag` from
 * `ui/tag.tsx` and a domain is `Domain` from `ui/domain.tsx`: the one identifier that is a label by
 * nature, and the one that is a name.
 */
export function ValueList({
  items,
  empty = "None",
  max,
  mono = false,
  label = asIs,
  truncate = false,
  className,
}: ValueListProps): ReactElement {
  if (items.length === 0) {
    return <span className="text-kumo-subtle">{empty}</span>;
  }

  const shown = max === undefined ? items : items.slice(0, max);
  const hidden = items.slice(shown.length).map((item) => label(itemOf(item).value));

  return (
    <ul className={cn("flex min-w-0 flex-col gap-0.5", className)}>
      {shown.map((entry) => {
        const item = itemOf(entry);

        return (
          <li
            key={item.value}
            title={truncate ? label(item.value) : undefined}
            className={cn(
              "min-w-0",
              truncate ? "truncate" : "[overflow-wrap:anywhere]",
              item.muted === true ? "text-kumo-subtle" : "text-kumo-default",
              (item.mono ?? mono) && "font-mono text-[0.9em]",
            )}
          >
            {label(item.value)}
          </li>
        );
      })}
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
