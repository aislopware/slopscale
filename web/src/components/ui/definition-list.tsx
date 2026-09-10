import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import { CopyText } from "~/components/ui/copy-text.tsx";

export interface Definition {
  readonly label: ReactNode;
  readonly value: ReactNode;
  /** Renders the value as a click-to-copy monospace run putting this text on the clipboard. */
  readonly copy?: string;
  /** Gives the row both columns of a two-column list, for a value a half row would cut off. */
  readonly wide?: boolean;
  readonly key?: string;
}

/**
 * Label/value rows with hairlines between them: the console's way to show facts about a resource.
 * Mono values are a CopyText: click to copy, with the icon always visible so every value ends at
 * the same edge.
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
    <dl className={cn("grid", columns === 2 ? "xl:grid-cols-2" : "grid-cols-1", className)}>
      {items.map((item, index) => (
        <div
          key={item.key ?? index}
          className={cn(
            "flex min-w-0 items-baseline justify-between gap-4 border-t border-kumo-hairline px-5 py-2.5 first:border-t-0",
            columns === 2 && "xl:nth-[2]:border-t-0",
            columns === 2 && item.wide === true && "xl:col-span-2",
          )}
        >
          <dt className="shrink-0 text-kumo-subtle">{item.label}</dt>
          <dd className="flex min-w-0 flex-1 items-center justify-end text-right text-kumo-default">
            {item.copy === undefined ? (
              <span className={cn("min-w-0", wrap ? "break-all" : "truncate")}>{item.value}</span>
            ) : (
              <CopyText
                value={typeof item.value === "string" ? item.value : item.copy}
                {...(typeof item.value === "string" ? {} : { display: item.value })}
                copy={item.copy}
                wrap={wrap}
              />
            )}
          </dd>
        </div>
      ))}
    </dl>
  );
}
