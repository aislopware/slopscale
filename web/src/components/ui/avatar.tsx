import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import { seededColours } from "~/lib/hue.ts";

/** Initials from a name: "Alice Nguyen" → "AN", "jane.doe" → "JA". */
export function initials(name: string): string {
  const [first = "", second] = name
    .trim()
    .split(/[\s._-]+/u)
    .filter(Boolean);

  return (
    second === undefined ? first.slice(0, 2) : first.slice(0, 1) + second.slice(0, 1)
  ).toUpperCase();
}

const sizes = {
  sm: "size-5 text-[10px]",
  base: "size-6 text-[11px]",
  lg: "size-8 text-xs",
} as const;

const iconSizes: Record<keyof typeof sizes, number> = { sm: 12, base: 14, lg: 16 };

/**
 * Identity mark: a squircle with a hairline ring so it sits on the surface instead of floating.
 * Sized to the row it lives in, never to touch-target minimums. A person is their initials on a
 * tint seeded by their name, so the same person is the same colour in every list and two people
 * with the same initials still tell apart; something that is not a person, such as a key or the
 * server itself, is an icon on the neutral mark, so a column of actors still lines up while the
 * kind is told at a glance.
 */
export function Avatar({
  name,
  icon: Mark,
  size = "base",
  className,
}: {
  readonly name: string;
  /** Stands in for the initials when the name is not a person's. */
  readonly icon?: Icon;
  readonly size?: keyof typeof sizes;
  readonly className?: string;
}): ReactElement {
  return (
    <span
      aria-hidden
      className={cn(
        "inline-flex shrink-0 items-center justify-center rounded-md leading-none font-medium ring ring-kumo-line",
        Mark === undefined ? "ring-inset" : "bg-kumo-elevated text-kumo-strong",
        sizes[size],
        className,
      )}
      style={Mark === undefined ? seededColours(name) : undefined}
    >
      {Mark === undefined ? initials(name) : <Mark size={iconSizes[size]} weight="bold" />}
    </span>
  );
}
