import type { Group, Node, User } from "~/api/queries.ts";
import { isBuiltin } from "~/components/access/model.ts";
import type { PickerItem } from "~/components/ui/multi-picker.tsx";
import { nodeName, ownerLabel, userLabel } from "~/lib/node.ts";

export function nodeItems(nodes: readonly Node[]): PickerItem[] {
  return nodes
    .map((node) => ({ value: node.id, label: nodeName(node), hint: ownerLabel(node) }))
    .toSorted((left, right) => left.label.localeCompare(right.label));
}

export function userItems(users: readonly User[]): PickerItem[] {
  return users
    .map((user) => ({
      value: user.id,
      label: userLabel(user),
      hint: user.email === "" ? user.name : user.email,
    }))
    .toSorted((left, right) => left.label.localeCompare(right.label));
}

const everyMachine = "Every machine";

function builtinHint(item: PickerItem): boolean {
  return item.hint === everyMachine;
}

/** Every group, the builtin one first, as items for a rule side or a membership picker. */
export function groupItems(groups: readonly Group[], { builtin = true } = {}): PickerItem[] {
  return groups
    .filter((group) => builtin || !isBuiltin(group))
    .map((group) => ({
      value: group.id,
      label: group.name,
      hint: isBuiltin(group) ? everyMachine : group.description,
    }))
    .toSorted((left, right) => Number(builtinHint(right)) - Number(builtinHint(left)));
}
