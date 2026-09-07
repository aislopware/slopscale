import type { ReactElement, ReactNode } from "react";

/**
 * Title row of every page: sentence-case h1, an optional one-line description, an optional `meta`
 * line of small facts (counts, who is signed in) and right-aligned actions. Related text stays
 * close (gap-1); the page content below gets the larger gap.
 */
export function PageHeader({
  title,
  description,
  meta,
  actions,
  eyebrow,
}: {
  readonly title: ReactNode;
  readonly description?: ReactNode;
  /** Small muted facts under the title, e.g. "3 machines · 2 connected". */
  readonly meta?: ReactNode;
  readonly actions?: ReactNode;
  /** A line above the title: a status badge row, tags, a back link. */
  readonly eyebrow?: ReactNode;
}): ReactElement {
  return (
    <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
      <div className="flex min-w-0 flex-col gap-1">
        {eyebrow === undefined ? null : <div className="flex items-center gap-2">{eyebrow}</div>}
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
        <div className="flex shrink-0 items-center gap-2">{actions}</div>
      )}
    </div>
  );
}
