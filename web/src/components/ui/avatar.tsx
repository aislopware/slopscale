import { cn } from "@cloudflare/kumo/utils";
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

/**
 * Identity mark: a muted squircle with a hairline ring so it sits on the surface instead of
 * floating. Sized to the row it lives in, never to touch-target minimums.
 */
export function Avatar({
  name,
  size = "base",
  className,
}: {
  readonly name: string;
  readonly size?: keyof typeof sizes;
  readonly className?: string;
}): ReactElement {
  return (
    <span
      aria-hidden
      className={cn(
        "inline-flex shrink-0 items-center justify-center rounded-md bg-kumo-elevated font-medium text-kumo-strong ring ring-kumo-line",
        sizes[size],
        className,
      )}
    >
      {initials(name)}
    </span>
  );
}
