import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

/**
 * The white panel every Cloudflare surface is made of: Kumo's base fill, a 1px line ring and at
 * most `shadow-xs`. For a panel that is not a component of its own, such as a linked tile.
 */
export const cardClass = "rounded-lg bg-kumo-base shadow-xs ring ring-kumo-line";

/**
 * A plain card: the white panel on the page. It clips with overflow-clip rather than
 * overflow-hidden, so the corners stay round without the card becoming a scroll container.
 */
export function Card({
  children,
  className,
}: {
  readonly children: ReactNode;
  readonly className?: string | undefined;
}): ReactElement {
  return (
    <div className={cn("flex min-w-0 flex-col overflow-clip", cardClass, className)}>
      {children}
    </div>
  );
}
