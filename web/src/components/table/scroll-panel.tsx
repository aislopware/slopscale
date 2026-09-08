import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import { useOverflowEdges } from "~/components/table/overflow.ts";
import { FramePanel } from "~/components/ui/frame.tsx";

export interface TableScrollProps {
  /**
   * The table. It is called with whether the columns run past the panel, so a pinned column can
   * draw its edge only while something is scrolled behind it.
   */
  readonly children: (overflowing: boolean) => ReactNode;
  /**
   * Rendered as a panel under the header, outside the scroll container: the empty state a table
   * with no rows shows where its rows would be.
   */
  readonly below?: ReactNode;
  /** Whether the table pins a column to its right edge, which then draws that edge itself. */
  readonly pinnedRight?: boolean;
}

/**
 * The container a `frameTableClass` table sits in, on the band of a Frame. The table draws the
 * panel on its own body, so this is not a panel. The rows never scroll inside it: the page scrolls,
 * and a long table pages. Only the columns can run past the panel, sideways, and the edge they ran
 * past is faded so a table that scrolls sideways looks like it does.
 */
export function TableScroll({
  children,
  below,
  pinnedRight = false,
}: TableScrollProps): ReactElement {
  const { ref, edges } = useOverflowEdges();

  return (
    <div className="relative min-w-0">
      <div ref={ref} className="overflow-x-auto">
        {children(edges.left || edges.right)}
      </div>
      {below === null || below === undefined ? null : <FramePanel>{below}</FramePanel>}
      {edges.left ? <ScrollFade side="left" /> : null}
      {edges.right && !pinnedRight ? <ScrollFade side="right" /> : null}
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
