import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

export interface Fact {
  readonly key: string;
  readonly label: ReactNode;
  readonly value: ReactNode;
  /** Gives the fact a whole row of the grid, for a value a third of the card would cut off. */
  readonly wide?: boolean;
}

/**
 * The settings grid inside a Zero Trust detail card (a device profile's Settings): two columns,
 * three from `sm`, each fact a 12px subtle label over its 13px value, 24px across and 16px down. A
 * card of a dozen facts reads as one block this way instead of a dozen ruled rows of equal weight.
 */
export function FactGrid({
  facts,
  className,
}: {
  readonly facts: readonly Fact[];
  readonly className?: string;
}): ReactElement {
  return (
    <dl className={cn("grid grid-cols-2 gap-x-6 gap-y-4 p-4 sm:grid-cols-3", className)}>
      {facts.map((fact) => (
        <div
          key={fact.key}
          className={cn("flex min-w-0 flex-col gap-0.5", fact.wide === true && "col-span-full")}
        >
          <dt className="truncate text-xs text-kumo-subtle">{fact.label}</dt>
          <dd className="min-w-0 text-sm text-kumo-default">{fact.value}</dd>
        </div>
      ))}
    </dl>
  );
}
