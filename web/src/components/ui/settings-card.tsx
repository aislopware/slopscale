import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import { cardClass } from "~/components/ui/card.tsx";

/**
 * The dashboard's settings card: one white card of rows, each a setting with its title, a subtle
 * description and its control on the right (a Switch, or a secondary button). `compact` is the
 * denser card of a settings page (worker settings): 12px corners and 10px rows with the label
 * alone, for settings whose name says enough.
 */
export function SettingsCard({
  children,
  compact = false,
  className,
}: {
  readonly children: ReactNode;
  readonly compact?: boolean;
  readonly className?: string;
}): ReactElement {
  return (
    <div
      data-compact={compact ? "" : undefined}
      className={cn(
        "group/settings flex min-w-0 flex-col overflow-hidden",
        compact ? "rounded-xl bg-kumo-base ring ring-kumo-line" : cardClass,
        className,
      )}
    >
      {children}
    </div>
  );
}

/** One setting in a `SettingsCard`. */
export function SettingsRow({
  title,
  description,
  control,
  children,
}: {
  readonly title: ReactNode;
  readonly description?: ReactNode;
  /** What changes the setting, on the right: a Switch, a Select, a secondary button. */
  readonly control?: ReactNode;
  /** More of the setting under its description, such as a field that only applies while it is on. */
  readonly children?: ReactNode;
}): ReactElement {
  return (
    <article
      className={cn(
        "flex flex-col gap-3 border-t border-kumo-line first:border-t-0",
        "py-4 pr-6 pl-4 group-data-compact/settings:py-2.5 group-data-compact/settings:pr-2.5 group-data-compact/settings:pl-5",
      )}
    >
      {/* The control stays beside the description at every width, as the dashboard's rows keep a
          Switch on the right of a phone's screen; the text wraps instead. */}
      <div className="flex items-start justify-between gap-4 sm:gap-6">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex min-h-lh flex-wrap items-center gap-2">
            <h3 className="font-medium text-kumo-default">{title}</h3>
          </div>
          {description === undefined ? null : (
            <div className="max-w-[750px] text-pretty text-kumo-subtle">{description}</div>
          )}
        </div>
        {control === undefined ? null : (
          <div className="flex min-h-lh shrink-0 items-center">{control}</div>
        )}
      </div>
      {children}
    </article>
  );
}

/**
 * A place something can be added that is empty: a dashed recessed slot with a subtle sentence and a
 * secondary "Add" on the right ("No queue consumers configured + Add").
 */
export function EmptySlot({
  children,
  action,
  className,
}: {
  readonly children: ReactNode;
  readonly action?: ReactNode;
  readonly className?: string;
}): ReactElement {
  return (
    <div
      className={cn(
        "flex min-h-12 items-center justify-between gap-3 rounded-lg border border-dashed border-kumo-line bg-kumo-recessed py-2 pr-2 pl-4 text-sm text-kumo-subtle",
        className,
      )}
    >
      <span className="min-w-0">{children}</span>
      {action === undefined ? null : <span className="shrink-0">{action}</span>}
    </div>
  );
}
