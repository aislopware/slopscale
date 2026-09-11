import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

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

const hueSteps = 360;
const hashMultiplier = 31;
/** Keeps the running hash inside the range multiplication is exact in. */
const hashModulus = 4_294_967_296;

/** A hash of the name onto the hue circle: the same name is the same hue on every page and visit. */
export function hueOf(seed: string): number {
  let hash = 0;

  for (const char of seed) {
    hash = (hash * hashMultiplier + (char.codePointAt(0) ?? 0)) % hashModulus;
  }

  return hash % hueSteps;
}

/**
 * The mark's colours from its hue: one lightness and chroma per theme, so every avatar is as quiet
 * as every other and only the hue tells them apart. `light-dark()` follows Kumo's `color-scheme`,
 * so the pair flips with the theme without a second rule.
 */
export function seededColours(seed: string): { backgroundColor: string; color: string } {
  const hue = hueOf(seed);

  return {
    backgroundColor: `light-dark(oklch(0.93 0.045 ${hue}), oklch(0.3 0.05 ${hue}))`,
    color: `light-dark(oklch(0.42 0.11 ${hue}), oklch(0.86 0.07 ${hue}))`,
  };
}

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
