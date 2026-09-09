import { Combobox } from "@cloudflare/kumo/components/combobox";
import type { ReactElement } from "react";

import type { AccessGraphNode } from "~/api/schema.gen.ts";
import { ownerLabel } from "~/components/access-graph/model.ts";

/**
 * The machine the page is about. Clearing it puts the whole tailnet back, so the placeholder says
 * what the empty picker means rather than asking for something.
 */
export function MachinePicker({
  nodes,
  value,
  onValueChange,
}: {
  readonly nodes: readonly AccessGraphNode[];
  /** The chosen machine's id; empty for the whole tailnet. */
  readonly value: string;
  readonly onValueChange: (nodeId: string) => void;
}): ReactElement {
  const items = nodes.toSorted((left, right) => left.name.localeCompare(right.name));
  const selected = items.find((node) => node.id === value) ?? null;

  return (
    <Combobox<AccessGraphNode>
      items={items}
      value={selected}
      isItemEqualToValue={(item, chosen) => item.id === chosen.id}
      itemToStringLabel={(item) => item.name}
      onValueChange={(next) => {
        onValueChange(next === null ? "" : next.id);
      }}
    >
      <Combobox.TriggerInput
        className="w-full"
        aria-label="Machine"
        placeholder="Every machine"
        clearLabel="Show every machine"
      />
      <Combobox.Content className="max-h-72 overflow-y-auto">
        <Combobox.Empty>No machine matches</Combobox.Empty>
        <Combobox.List>
          {(item: AccessGraphNode) => (
            <Combobox.Item key={item.id} value={item}>
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="truncate">{item.name}</span>
                <span className="truncate text-xs text-kumo-subtle">{ownerLabel(item)}</span>
              </span>
            </Combobox.Item>
          )}
        </Combobox.List>
      </Combobox.Content>
    </Combobox>
  );
}
