import type { ReactElement, ReactNode } from "react";

/**
 * Title row of every page: sentence-case h1 with an optional one-line description and right-aligned
 * actions. Related text stays close (gap-1); the page content below gets the larger gap.
 */
export function PageHeader({
  title,
  description,
  actions,
}: {
  readonly title: string;
  readonly description?: ReactNode;
  readonly actions?: ReactNode;
}): ReactElement {
  return (
    <div className="flex flex-wrap items-start justify-between gap-4">
      <div className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold text-kumo-default">{title}</h1>
        {description === undefined ? null : (
          <p className="max-w-prose text-kumo-subtle">{description}</p>
        )}
      </div>
      {actions === undefined ? null : <div className="flex items-center gap-2">{actions}</div>}
    </div>
  );
}
