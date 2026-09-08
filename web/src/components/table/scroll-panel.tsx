import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import { useOverflowEdges } from "~/components/table/overflow.ts";
import { FramePanel } from "~/components/ui/frame.tsx";

/** As tall as a table's rows may get before the panel scrolls them under a pinned header. */
const scrollHeightClass = "lg:max-h-[70vh]";

export interface TableScrollProps {
  /**
   * The table. It is called with whether the columns run past the panel, so a pinned column can
   * draw its edge only while something is scrolled behind it.
   */
  readonly children: (overflowing: boolean) => ReactNode;
  /** Rendered inside the panel but outside the scroll container, such as an empty state. */
  readonly below?: ReactNode;
  /**
   * Whether the rows scroll inside the panel with the header pinned to the top, from `lg` up: a
   * phone has no room for a window inside the page, so there the page keeps scrolling. A table
   * shorter than the cap never reaches it.
   */
  readonly scroll?: boolean;
  /** Whether the table pins a column to its right edge, which then draws that edge itself. */
  readonly pinnedRight?: boolean;
}

/**
 * The scroll container a table sits in. It lives inside the panel rather than being the panel, so
 * the table's header sticks to the top of the rows rather than to the page, and anything below it
 * stays put however far the columns run. The edge a column ran past is faded, so a table that
 * scrolls sideways looks like it does.
 */
export function TableScroll({
  children,
  below,
  scroll = true,
  pinnedRight = false,
}: TableScrollProps): ReactElement {
  const { ref, edges } = useOverflowEdges();

  return (
    <div className="relative min-w-0">
      <div ref={ref} className={cn("overflow-auto", scroll && scrollHeightClass)}>
        {children(edges.left || edges.right)}
      </div>
      {below}
      {edges.left ? <ScrollFade side="left" /> : null}
      {edges.right && !pinnedRight ? <ScrollFade side="right" /> : null}
    </div>
  );
}

/** A `TableScroll` as the panel of a Frame, which is how a table page draws its table. */
export function TableScrollPanel(props: TableScrollProps): ReactElement {
  return (
    <FramePanel>
      <TableScroll {...props} />
    </FramePanel>
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
