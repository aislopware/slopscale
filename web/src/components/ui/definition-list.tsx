import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import { CopyButton } from "~/components/ui/copy-button.tsx";

export interface Definition {
  readonly label: ReactNode;
  readonly value: ReactNode;
  /** Renders the value in monospace and adds a hover copy button for this text. */
  readonly copy?: string;
  readonly key?: string;
}

/**
 * Label/value rows with hairlines between them: the console's way to show facts about a resource.
 * Mono values get a copy button that appears on hover, instead of input-shaped boxes.
 */
export function DefinitionList({
  items,
  columns = 1,
  wrap = false,
  className,
}: {
  readonly items: readonly Definition[];
  /** Two columns on wide screens for dense fact grids. */
  readonly columns?: 1 | 2;
  /** Let long values wrap onto several lines instead of truncating them. */
  readonly wrap?: boolean;
  readonly className?: string;
}): ReactElement {
  return (
    <dl
      className={cn("grid", columns === 2 ? "sm:grid-cols-2 sm:gap-x-8" : "grid-cols-1", className)}
    >
      {items.map((item, index) => (
        <div
          key={item.key ?? index}
          className={cn(
            "group flex min-w-0 items-baseline justify-between gap-4 border-t border-kumo-hairline px-5 py-2.5 first:border-t-0",
            columns === 2 && "sm:nth-[2]:border-t-0",
          )}
        >
          <dt className="shrink-0 text-kumo-subtle">{item.label}</dt>
          <dd className="flex min-w-0 items-center gap-1.5 text-right text-kumo-default">
            {item.copy === undefined ? (
              <span className={cn("min-w-0", wrap ? "break-all" : "truncate")}>{item.value}</span>
            ) : (
              <>
                <span
                  className={cn("min-w-0 font-mono text-[0.9em]", wrap ? "break-all" : "truncate")}
                >
                  {item.value}
                </span>
                <CopyButton
                  value={item.copy}
                  className="opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
                />
              </>
            )}
          </dd>
        </div>
      ))}
    </dl>
  );
}
