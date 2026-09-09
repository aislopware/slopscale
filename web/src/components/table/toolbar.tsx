import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

/**
 * The row above a table Frame, on the page rather than on the band: search on the left, filters
 * beside it, the primary action on the right. It is the page's controls, not the table's, so it
 * sits outside the frame. A page stacks its blocks 24px apart; the toolbar belongs with the table
 * under it, so it pulls the frame up to 12px.
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
    <div className={cn("-mb-3 flex flex-wrap items-start gap-2", className)}>
      <div className="flex min-w-0 flex-1 basis-64 flex-wrap items-center gap-2">{children}</div>
      {actions === undefined ? null : (
        <div className="flex min-h-9 shrink-0 items-center gap-2">{actions}</div>
      )}
    </div>
  );
}

/**
 * "Showing 3 of 12" on the band under a table panel, with room for paging controls on the right.
 * Its 20px inset starts the text where the text of the first cell above it starts.
 */
export function TableFooter({
  children,
  actions,
}: {
  readonly children: ReactNode;
  readonly actions?: ReactNode;
}): ReactElement {
  return (
    <div className="flex items-center justify-between gap-3 px-5 py-1.5 text-sm text-kumo-subtle">
      <span>{children}</span>
      {actions === undefined ? null : <span className="flex items-center gap-2">{actions}</span>}
    </div>
  );
}
