import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import { CopyButton, CopyText } from "~/components/ui/copy-text.tsx";

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
 * Label/value rows divided by lines, the dashboard's account-details rows: a 13px subtle label on
 * the left, the value right-aligned, 36px a row. Mono values are a CopyText: click to copy, the
 * icon showing on hover.
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
            "flex min-h-9 min-w-0 items-center justify-between gap-4 border-t border-kumo-line px-4 py-2 text-sm first:border-t-0",
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
                    ? "*:[overflow-wrap:anywhere]"
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

export interface KeyValue {
  readonly label: ReactNode;
  readonly value: ReactNode;
  /** Puts a copy button beside the value that copies this text. */
  readonly copy?: string;
  /** The copy button's accessible name: "Copy tailnet ID". */
  readonly copyLabel?: string;
  /**
   * A small control at the end of the value, such as the pencil that renames a machine in place (a
   * ghost `size="sm"` square button).
   */
  readonly control?: ReactNode;
  /**
   * Gives the value the whole row and lets it wrap instead of cutting it, for a value that is a
   * form of its own (the rename box) or runs to several lines.
   */
  readonly wrap?: boolean;
  readonly key?: string;
}

/**
 * The dashboard's stacked key-value list (a zone's API card, Zero Trust's details rail): each pair
 * a 13px subtle label over the 13px value, rows divided by lines, and a 26px ghost copy button at
 * the end of a copyable value such as an ID, or a control there such as a rename pencil.
 */
export function KeyValueList({
  items,
  className,
}: {
  readonly items: readonly KeyValue[];
  readonly className?: string;
}): ReactElement {
  return (
    <dl className={cn("flex flex-col divide-y divide-kumo-line px-4", className)}>
      {items.map((item, index) => (
        <div key={item.key ?? index} className="grid gap-1 py-3">
          <dt className="truncate text-sm text-kumo-subtle">{item.label}</dt>
          <dd className="flex min-w-0 items-center justify-between gap-2 text-sm text-kumo-default">
            <span className={cn("min-w-0", item.wrap === true ? "flex-1" : "truncate")}>
              {item.value}
            </span>
            {item.copy === undefined ? null : (
              <CopyButton value={item.copy} label={item.copyLabel ?? `Copy ${item.copy}`} />
            )}
            {item.control === undefined ? null : (
              <span className="flex shrink-0 items-center">{item.control}</span>
            )}
          </dd>
        </div>
      ))}
    </dl>
  );
}
