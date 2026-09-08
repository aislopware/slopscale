import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

/** The classes of an inset panel, for panels that are not a LayerCard, such as a linked tile. */
export const framePanelClass = "rounded-lg bg-kumo-base shadow-xs ring ring-kumo-line";

/**
 * The console's framed surface. A tinted band with a hairline ring and 4px of padding holds one or
 * more inset panels. The band carries what belongs to the panel but is not its content, such as a
 * title, a toolbar or a row count. The outer radius is 12px and the inner 8px, so with the 4px
 * padding the corners are concentric.
 */
export function Frame({
  children,
  className,
}: {
  readonly children: ReactNode;
  readonly className?: string | undefined;
}): ReactElement {
  return (
    <div
      className={cn(
        "flex min-w-0 flex-col rounded-xl bg-kumo-elevated p-1 ring ring-kumo-hairline",
        className,
      )}
    >
      {children}
    </div>
  );
}

/**
 * The inset panel inside a Frame. It is a plain LayerCard clipped with overflow-clip rather than
 * overflow-hidden, so the corners stay round without the panel itself becoming a scroll container:
 * a table brings its own scroll container inside it, which is what its sticky header sticks to.
 */
export function FramePanel({
  children,
  className,
}: {
  readonly children: ReactNode;
  readonly className?: string | undefined;
}): ReactElement {
  return <LayerCard className={cn("min-w-0 overflow-clip", className)}>{children}</LayerCard>;
}

/**
 * A table drawn as the panel of a Frame. Its outer cells step in to 20px, where the text of a band
 * row and of a `SectionRow` starts, so the first column lines up with the title above it while the
 * columns between keep Kumo's 12px. A cell that spans the row, such as an expanded row or an empty
 * state, keeps the padding its own class asks for.
 */
export const frameTableClass =
  "[&_td:first-child:not([colspan])]:pl-5 [&_th:first-child]:pl-5 [&_td:last-child:not([colspan])]:pr-5 [&_th:last-child]:pr-5";

/**
 * A body row of a `frameTableClass` table. Kumo's default row variant stripes every second row;
 * these drop the stripe and highlight on hover instead, which is enough to follow one row across a
 * wide table and leaves a sticky column one background to match. The hover rules carry the `even`
 * modifier as well, so they outrank the stripe they replace instead of relying on source order.
 */
export const frameTableRowClass =
  "even:bg-kumo-base even:[--kumo-table-row-bg:var(--color-kumo-base)] hover:bg-kumo-tint hover:[--kumo-table-row-bg:var(--color-kumo-tint)] even:hover:bg-kumo-tint even:hover:[--kumo-table-row-bg:var(--color-kumo-tint)]";

/**
 * A row of text on the band above or below the panel, such as a title or a count. Its inset plus
 * the frame's 4px must put the text where the panel's own text starts: 12px for a panel that draws
 * its content flush, px-5 for the 20px of a `SectionRow` or a `frameTableClass` table.
 */
export function FrameBand({
  children,
  className,
}: {
  readonly children: ReactNode;
  readonly className?: string | undefined;
}): ReactElement {
  return <div className={cn("px-3 py-1.5", className)}>{children}</div>;
}
