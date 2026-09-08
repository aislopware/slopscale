import type { Group, Node, Posture, User } from "~/api/queries.ts";
import { isBuiltin, isSelf } from "~/components/access/model.ts";
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
const sameUser = "The source's own user";

function builtinHint(item: PickerItem): boolean {
  return item.hint === everyMachine || item.hint === sameUser;
}

/**
 * Which groups a picker offers. A rule's destination takes every group; its source and a network,
 * DNS rule or pre-auth key take member sets only, so not Own machines; a membership picker takes
 * operator-made groups only.
 */
export type GroupPickerRole = "destination" | "members" | "membership";

/** The groups the role allows, the builtin ones first, as picker items. */
export function groupItems(
  groups: readonly Group[],
  role: GroupPickerRole = "members",
): PickerItem[] {
  return groups
    .filter((group) => pickable(group, role))
    .map((group) => ({ value: group.id, label: group.name, hint: hintFor(group) }))
    .toSorted((left, right) => Number(builtinHint(right)) - Number(builtinHint(left)));
}

function pickable(group: Group, role: GroupPickerRole): boolean {
  if (role === "destination") {
    return true;
  }

  return role === "members" ? !isSelf(group) : !isBuiltin(group);
}

function hintFor(group: Group): string {
  if (isSelf(group)) {
    return sameUser;
  }

  return isBuiltin(group) ? everyMachine : group.description;
}

export function postureItems(postures: readonly Posture[]): PickerItem[] {
  return postures
    .map((posture) => ({ value: posture.id, label: posture.name, hint: posture.description }))
    .toSorted((left, right) => left.label.localeCompare(right.label));
}
