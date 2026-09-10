import type { ReactElement, ReactNode } from "react";

/**
 * Title row of every page: sentence-case h1, an optional one-line description, an optional `meta`
 * line of small facts (counts, who is signed in, the state of the thing) and right-aligned actions.
 * Related text stays close (gap-1); the page content below gets the larger gap. Nothing sits above
 * the title: a state or a label there reads as a kicker, and the title carries its own weight.
 */
export function PageHeader({
  title,
  description,
  meta,
  actions,
}: {
  readonly title: ReactNode;
  readonly description?: ReactNode;
  /** Small muted facts under the title, e.g. "3 machines · 2 connected". */
  readonly meta?: ReactNode;
  readonly actions?: ReactNode;
}): ReactElement {
  return (
    <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
      <div className="flex min-w-0 flex-col gap-1">
        <h1 className="truncate text-2xl font-semibold text-kumo-strong">{title}</h1>
        {description === undefined ? null : (
          <p className="max-w-prose text-kumo-subtle">{description}</p>
        )}
        {meta === undefined ? null : (
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-kumo-subtle">
            {meta}
          </div>
        )}
      </div>
      {actions === undefined ? null : (
        // Wraps rather than shrinks: a row of three controls (a back link, a field and a button)
        // is wider than a phone, and squeezing them would cut their labels instead of stacking.
        <div className="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2">
          {actions}
        </div>
      )}
    </div>
  );
}
