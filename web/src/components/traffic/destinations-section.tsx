import { Select } from "@cloudflare/kumo/components/select";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import type { ReactElement, ReactNode } from "react";

import type { DestinationGrouping, TrafficDestination } from "~/api/traffic.ts";
import { DestinationsTable } from "~/components/traffic/destinations-table.tsx";
import { destinationTabs, toDestinationGrouping } from "~/components/traffic/search.ts";
import { Section } from "~/components/ui/section.tsx";

/** The groupings a section offers: every view of "where", none of "who". */
const whereTabs = destinationTabs
  .filter((tab) => tab.value !== "node")
  .map((tab) => ({ value: tab.value, label: tab.label }));

/** A "where" grouping for a section; the "who" groupings fall back to hosts. */
export function whereGrouping(by: DestinationGrouping): DestinationGrouping {
  return by === "node" || by === "reporter" ? "host" : by;
}

/** The grouping as segments, or as a select on a phone, where six segments do not fit across. */
export function GroupingPicker({
  tabs,
  value,
  onChange,
}: {
  readonly tabs: readonly { value: DestinationGrouping; label: string }[];
  readonly value: DestinationGrouping;
  readonly onChange: (by: DestinationGrouping) => void;
}): ReactElement {
  const pick = (next: string | null): void => {
    onChange(toDestinationGrouping(next ?? ""));
  };

  return (
    <>
      <Tabs
        className="hidden sm:flex"
        variant="segmented"
        aria-label="Group destinations by"
        tabs={[...tabs]}
        value={value}
        onValueChange={pick}
      />
      <Select
        className="w-40 sm:hidden"
        aria-label="Group destinations by"
        items={[...tabs]}
        value={value}
        onValueChange={pick}
      />
    </>
  );
}

/** Destinations under a title, with the grouping as segments on the band. */
export function DestinationsSection({
  title,
  description,
  rows,
  groupBy,
  whole,
  footer,
  oneMachine = false,
  onGroupChange,
  onPick,
}: {
  readonly title: string;
  readonly description: string;
  readonly rows: readonly TrafficDestination[];
  readonly groupBy: DestinationGrouping;
  readonly whole: number;
  readonly footer?: ReactNode;
  readonly oneMachine?: boolean;
  readonly onGroupChange: (by: DestinationGrouping) => void;
  readonly onPick: (row: TrafficDestination) => void;
}): ReactElement {
  return (
    <Section
      title={title}
      description={description}
      actions={<GroupingPicker tabs={whereTabs} value={groupBy} onChange={onGroupChange} />}
      panel={false}
    >
      <DestinationsTable
        rows={rows}
        groupBy={groupBy}
        whole={whole}
        footer={footer}
        oneMachine={oneMachine}
        onPick={onPick}
      />
    </Section>
  );
}
