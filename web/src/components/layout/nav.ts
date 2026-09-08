import type { Icon } from "@phosphor-icons/react";
import {
  ClockCounterClockwiseIcon,
  DesktopIcon,
  GearSixIcon,
  GlobeIcon,
  HandWavingIcon,
  BroadcastIcon,
  KeyIcon,
  WebhooksLogoIcon,
  PathIcon,
  ShieldCheckIcon,
  SquaresFourIcon,
  TerminalWindowIcon,
  UsersIcon,
} from "@phosphor-icons/react";

import type { Me, Scope } from "~/auth/me.ts";
import { can } from "~/auth/me.ts";

export type NavPath =
  | "/"
  | "/machines"
  | "/users"
  | "/keys"
  | "/access"
  | "/policy"
  | "/dns"
  | "/relays"
  | "/networks"
  | "/webhooks"
  | "/settings"
  | "/audit"
  | "/sessions";

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

/**
 * The sidebar, grouped by what the operator is doing: the machines and people on the tailnet, who
 * may reach what, how packets and names travel, what happened, and the switches, credentials and
 * outbound integrations that configure all of it.
 */
export const navGroups: readonly NavGroup[] = [
  { items: [{ to: "/", label: "Overview", icon: SquaresFourIcon, exact: true }] },
  {
    label: "Tailnet",
    items: [
      {
        to: "/machines",
        label: "Machines",
        icon: DesktopIcon,
        scope: "devices:core:read",
        badge: "pendingNodes",
      },
      { to: "/users", label: "Users", icon: UsersIcon, scope: "users:read", badge: "pendingUsers" },
    ],
  },
  {
    label: "Access",
    items: [
      { to: "/policy", label: "Access controls", icon: ShieldCheckIcon, scope: "policy_file:read" },
      { to: "/access", label: "My access", icon: HandWavingIcon },
    ],
  },
  {
    label: "Connectivity",
    items: [
      { to: "/networks", label: "Networks", icon: PathIcon, scope: "devices:routes:read" },
      { to: "/dns", label: "DNS", icon: GlobeIcon, scope: "dns:read" },
      { to: "/relays", label: "Relays", icon: BroadcastIcon, scope: "feature_settings:read" },
    ],
  },
  {
    label: "Logs",
    items: [
      {
        to: "/audit",
        label: "Audit log",
        icon: ClockCounterClockwiseIcon,
        scope: "logs:configuration:read",
      },
      {
        to: "/sessions",
        label: "SSH sessions",
        icon: TerminalWindowIcon,
        scope: "logs:configuration:read",
      },
    ],
  },
  {
    label: "Settings",
    items: [
      { to: "/settings", label: "General", icon: GearSixIcon, scope: "feature_settings:read" },
      { to: "/keys", label: "Keys", icon: KeyIcon },
      {
        to: "/webhooks",
        label: "Integrations",
        icon: WebhooksLogoIcon,
        scope: "webhooks:read",
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
