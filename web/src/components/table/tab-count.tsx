import type { TabsItem } from "@cloudflare/kumo/components/tabs";
import type { ReactElement } from "react";

/** A filter tab's label with how many rows it would show, so a filter is never a blind pick. */
export function TabLabel({
  label,
  count,
}: {
  readonly label: string;
  readonly count: number;
}): ReactElement {
  return (
    <span className="flex items-center gap-1.5">
      {label}
      <span className="text-kumo-subtle tabular-nums">{count}</span>
    </span>
  );
}

/** Segmented filter tabs, each carrying the number of rows it would show. */
export function countedTabs<Value extends string>(
  tabs: readonly { readonly value: Value; readonly label: string }[],
  count: (value: Value) => number,
): TabsItem[] {
  return tabs.map(({ value, label }) => ({
    value,
    label: <TabLabel label={label} count={count(value)} />,
  }));
}
