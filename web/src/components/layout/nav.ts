import type { Icon } from "@phosphor-icons/react";
import {
  ClockCounterClockwiseIcon,
  DesktopIcon,
  GearSixIcon,
  KeyIcon,
  ShieldCheckIcon,
  SquaresFourIcon,
  UsersIcon,
} from "@phosphor-icons/react";

import type { Me, Scope } from "~/auth/me.ts";
import { can } from "~/auth/me.ts";

export type NavPath = "/" | "/machines" | "/users" | "/keys" | "/policy" | "/settings" | "/audit";

export interface NavItem {
  readonly to: NavPath;
  readonly label: string;
  readonly icon: Icon;
  /** Hidden without this scope; members without any scope still get their machines. */
  readonly scope?: Scope;
  readonly exact?: boolean;
  /** Which live count the sidebar shows next to the item. */
  readonly badge?: "pendingNodes" | "pendingUsers";
}

export interface NavGroup {
  readonly label?: string;
  readonly items: readonly NavItem[];
}

export const navGroups: readonly NavGroup[] = [
  { items: [{ to: "/", label: "Overview", icon: SquaresFourIcon, exact: true }] },
  {
    label: "Network",
    items: [
      {
        to: "/machines",
        label: "Machines",
        icon: DesktopIcon,
        scope: "devices:core:read",
        badge: "pendingNodes",
      },
      { to: "/users", label: "Users", icon: UsersIcon, scope: "users:read", badge: "pendingUsers" },
      { to: "/keys", label: "Keys", icon: KeyIcon },
    ],
  },
  {
    label: "Control",
    items: [
      { to: "/policy", label: "Access controls", icon: ShieldCheckIcon, scope: "policy_file:read" },
      { to: "/settings", label: "Settings", icon: GearSixIcon, scope: "feature_settings:read" },
      {
        to: "/audit",
        label: "Audit log",
        icon: ClockCounterClockwiseIcon,
        scope: "logs:configuration:read",
      },
    ],
  },
];

export function visibleGroups(me: Me): NavGroup[] {
  const groups: NavGroup[] = [];

  for (const group of navGroups) {
    const items = group.items.filter((item) => item.scope === undefined || can(me, item.scope));

    if (items.length > 0) {
      groups.push(group.label === undefined ? { items } : { label: group.label, items });
    }
  }

  return groups;
}

export function isActive(item: NavItem, pathname: string): boolean {
  return item.exact === true ? pathname === item.to : pathname.startsWith(item.to);
}
