import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import { useOverflowEdges } from "~/components/table/overflow.ts";
import { cardClass } from "~/components/ui/card.tsx";

export interface TableScrollProps {
  /**
   * The table. It is called with whether the columns run past the panel, so a pinned column can
   * draw its edge only while something is scrolled behind it.
   */
  readonly children: (overflowing: boolean) => ReactNode;
  /**
   * A band above the table inside the card, outside the scroll container: the bulk bar a table
   * shows while rows are ticked.
   */
  readonly header?: ReactNode;
  /**
   * Rendered under the header, outside the scroll container: the empty state a table with no rows
   * shows where its rows would be.
   */
  readonly below?: ReactNode;
  /** Whether the table pins a column to its right edge, which then draws that edge itself. */
  readonly pinnedRight?: boolean;
  /**
   * A band at the foot of the card, inside its frame: Zero Trust's list footer ("Showing 1-2 of 2",
   * the page size and the pager) on the elevated fill under a line.
   */
  readonly footer?: ReactNode;
}

/**
 * The card a `tableClass` table sits in: the dashboard's DataTable frame, a white panel with an 8px
 * radius, a line ring and `shadow-xs`, clipped so the header's canvas fill follows the corners. The
 * rows never scroll inside it: the page scrolls, and a long table pages. Only the columns can run
 * past the card, sideways, and the edge they ran past is faded so a table that scrolls sideways
 * looks like it does.
 */
export function TableScroll({
  children,
  header,
  below,
  pinnedRight = false,
  footer,
}: TableScrollProps): ReactElement {
  const { ref, edges } = useOverflowEdges();
  const overflowing = edges.left || edges.right;

  return (
    <div className={cn("relative min-w-0 overflow-clip", cardClass)}>
      {header}
      {/* The fades cover the columns alone, not the bands above and below them. */}
      <div className="relative">
        {/* A region that scrolls sideways must be reachable from the keyboard, or the arrow keys
            never get to move it; it only joins the tab order while there is something to scroll
            to. */}
        <div
          ref={ref}
          className="overflow-x-auto outline-none focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:ring-inset"
          {...(overflowing
            ? { tabIndex: 0, role: "region", "aria-label": "Table, scrolls sideways" }
            : {})}
        >
          {children(overflowing)}
        </div>
        {edges.left ? <ScrollFade side="left" /> : null}
        {edges.right && !pinnedRight ? <ScrollFade side="right" /> : null}
      </div>
      {below === null || below === undefined ? null : (
        <div className="border-t border-kumo-line">{below}</div>
      )}
      {footer === null || footer === undefined ? null : (
        <div
          data-slot="table-footer"
          className="border-t border-kumo-line bg-kumo-elevated px-4 py-2"
        >
          {footer}
        </div>
      )}
    </div>
  );
}

function ScrollFade({ side }: { readonly side: "left" | "right" }): ReactElement {
  return (
    <div
      aria-hidden
      className={cn(
        "pointer-events-none absolute inset-y-0 w-6",
        side === "left"
          ? "left-0 bg-linear-to-r from-kumo-base to-transparent"
          : "right-0 bg-linear-to-l from-kumo-base to-transparent",
      )}
    />
  );
}
