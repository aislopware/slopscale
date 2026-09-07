import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

/**
 * The first row of a table card: search on the left, filters beside it, the primary action on the
 * right. Sits inside the LayerCard so the controls and the rows read as one surface.
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
    <div
      className={cn(
        "flex flex-wrap items-center gap-2 border-b border-kumo-line bg-kumo-base px-3 py-2.5",
        className,
      )}
    >
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">{children}</div>
      {actions === undefined ? null : (
        <div className="flex shrink-0 items-center gap-2">{actions}</div>
      )}
    </div>
  );
}

/** "Showing 3 of 12" under a table, with room for paging controls on the right. */
export function TableFooter({
  children,
  actions,
}: {
  readonly children: ReactNode;
  readonly actions?: ReactNode;
}): ReactElement {
  return (
    <div className="flex items-center justify-between gap-3 border-t border-kumo-line px-3 py-2 text-xs text-kumo-subtle">
      <span>{children}</span>
      {actions === undefined ? null : <span className="flex items-center gap-2">{actions}</span>}
    </div>
  );
}
