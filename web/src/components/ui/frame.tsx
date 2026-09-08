import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

/**
 * The console's framed surface: a tinted band with a hairline ring, 4px of padding, and one or more
 * inset LayerCard panels. The band carries what belongs to the panel but is not its content: a
 * title, a toolbar, a "Showing 3 of 12" line. The radii are concentric, outer 12px = inner 8px +
 * the 4px padding, so the corners line up.
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
 * The inset panel inside a Frame: a plain LayerCard, clipped rather than hidden so sticky table
 * headers keep working.
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
 * A row of text on the band above or below the panel, such as a title or a count. Its 12px inset
 * plus the frame's 4px puts the text where a table cell's text starts.
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
