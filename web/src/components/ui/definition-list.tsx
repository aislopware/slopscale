import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import { CopyText } from "~/components/ui/copy-text.tsx";

export interface Definition {
  readonly label: ReactNode;
  readonly value: ReactNode;
  /** Renders the value as a click-to-copy monospace run putting this text on the clipboard. */
  readonly copy?: string;
  /** Gives the row both columns of a two-column list, for a value a half row would cut off. */
  readonly wide?: boolean;
  /** Lets this value wrap onto several lines (a stacked pair, a sentence) whatever the list does. */
  readonly wrap?: boolean;
  readonly key?: string;
}

/**
 * Label/value rows with hairlines between them: the console's way to show facts about a resource.
 * Mono values are a CopyText: click to copy, with the icon always visible so every value ends at
 * the same edge.
 *
 * A value is cut with an ellipsis when the row runs out of room, but the cut is `overflow-clip`
 * with a margin rather than `overflow-hidden`: hidden clips at the box edge, which took the focus
 * ring and the hover tint off a control inside the value (a popover trigger, a small button), and a
 * ring cut on two sides reads as a rendering bug.
 */
export function DefinitionList({
  items,
  columns = 1,
  wrap = false,
  className,
}: {
  readonly items: readonly Definition[];
  /** Two columns on wide screens for dense fact grids. */
  readonly columns?: 1 | 2;
  /** Let long values wrap onto several lines instead of truncating them. */
  readonly wrap?: boolean;
  readonly className?: string;
}): ReactElement {
  return (
    <dl className={cn("grid", columns === 2 ? "xl:grid-cols-2" : "grid-cols-1", className)}>
      {items.map((item, index) => (
        <div
          key={item.key ?? index}
          className={cn(
            // Centred, not on the baseline: a control in the value (a copy button, a popover
            // trigger) is a box with its own line, and baseline alignment set it a few pixels
            // below the label. A two-line value centres its label on the pair.
            "flex min-w-0 items-center justify-between gap-4 border-t border-kumo-hairline px-5 py-2.5 first:border-t-0",
            columns === 2 && "xl:nth-[2]:border-t-0",
            columns === 2 && item.wide === true && "xl:col-span-2",
          )}
        >
          <dt className="shrink-0 text-kumo-subtle">{item.label}</dt>
          <dd className="flex min-w-0 flex-1 items-center justify-end text-right text-kumo-default">
            {item.copy === undefined ? (
              // A flex box, not a line of text, so a control inside sits centred on the row; the
              // cut goes on each child, which is where the text is.
              <span
                className={cn(
                  "flex min-w-0 items-center justify-end gap-2 *:min-w-0",
                  (item.wrap ?? wrap)
                    ? "*:break-all"
                    : "*:overflow-clip *:text-ellipsis *:whitespace-nowrap *:[overflow-clip-margin:4px]",
                )}
              >
                {/* The cut sits on the children, so a bare string gets a box of its own. */}
                {typeof item.value === "string" ? <span>{item.value}</span> : item.value}
              </span>
            ) : (
              <CopyText
                value={typeof item.value === "string" ? item.value : item.copy}
                {...(typeof item.value === "string" ? {} : { display: item.value })}
                copy={item.copy}
                wrap={item.wrap ?? wrap}
              />
            )}
          </dd>
        </div>
      ))}
    </dl>
  );
}
