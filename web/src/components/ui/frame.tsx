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
 * overflow-hidden, because clip does not make a scroll container and sticky table headers keep
 * working inside it.
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
 * A row of text on the band above or below the panel, such as a title or a count. The 12px inset
 * plus the frame's 4px puts the text where the text of a table cell starts.
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
