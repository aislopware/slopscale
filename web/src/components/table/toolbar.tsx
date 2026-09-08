import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

/**
 * The first row of a table Frame, on the band above the panel: search on the left, filters beside
 * it, the primary action on the right. The controls sit flush with the panel's edges, 4px above
 * it.
 */
export function TableToolbar({
  children,
  actions,
  className,
}: {
  readonly children: ReactNode;
  readonly actions?: ReactNode;
  readonly className?: string;
}): ReactElement {
  return (
    <div className={cn("flex flex-wrap items-start gap-2 pb-1", className)}>
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">{children}</div>
      {actions === undefined ? null : (
        <div className="flex min-h-9 shrink-0 items-center gap-2">{actions}</div>
      )}
    </div>
  );
}

/**
 * "Showing 3 of 12" on the band under a table panel, with room for paging controls on the right.
 * Its text starts where the cells' text starts.
 */
export function TableFooter({
  children,
  actions,
}: {
  readonly children: ReactNode;
  readonly actions?: ReactNode;
}): ReactElement {
  return (
    <div className="flex items-center justify-between gap-3 px-3 py-1.5 text-xs text-kumo-subtle">
      <span>{children}</span>
      {actions === undefined ? null : <span className="flex items-center gap-2">{actions}</span>}
    </div>
  );
}
