import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import { cardClass } from "~/components/ui/card.tsx";

/**
 * A figure on a card, as the dashboard's usage tiles show one: a 12px subtle label (with an info
 * trigger beside it when the figure needs explaining), the value in 24px semibold and an optional
 * line under it. `tone` colours the value when the figure is bad news.
 */
export function StatTile({
  label,
  info,
  value,
  hint,
  tone = "default",
  className,
}: {
  readonly label: ReactNode;
  /** An explanation trigger beside the label, such as a `HoverPopover` around an info icon. */
  readonly info?: ReactNode;
  readonly value: ReactNode;
  readonly hint?: ReactNode;
  readonly tone?: "default" | "danger" | "warning";
  readonly className?: string;
}): ReactElement {
  return (
    <div className={cn(cardClass, "flex min-w-0 flex-col gap-1 p-4", className)}>
      <span className="flex items-center gap-1 text-xs text-kumo-subtle">
        {label}
        {info}
      </span>
      <span
        className={cn(
          "truncate text-2xl font-semibold tabular-nums",
          tone === "danger" && "text-kumo-danger",
          tone === "warning" && "text-kumo-warning",
          tone === "default" && "text-kumo-default",
        )}
      >
        {value}
      </span>
      {hint === undefined ? null : <span className="text-xs text-kumo-subtle">{hint}</span>}
    </div>
  );
}

/** A row of stat tiles, 16px apart, as many to a row as there is room for at 180px each. */
export function StatGrid({
  children,
  className,
}: {
  readonly children: ReactNode;
  readonly className?: string;
}): ReactElement {
  return (
    <div className={cn("grid grid-cols-[repeat(auto-fit,minmax(180px,1fr))] gap-4", className)}>
      {children}
    </div>
  );
}
