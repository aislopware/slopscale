import { useSyncExternalStore } from "react";

/** Tailwind's breakpoints, as the stylesheet declares them. */
const queries = {
  sm: "(min-width: 40rem)",
  md: "(min-width: 48rem)",
  lg: "(min-width: 64rem)",
  xl: "(min-width: 80rem)",
  "2xl": "(min-width: 96rem)",
} as const;

export type Breakpoint = keyof typeof queries;

const subscribers = new Map<Breakpoint, (onChange: () => void) => () => void>();

function subscribeTo(breakpoint: Breakpoint): (onChange: () => void) => () => void {
  let subscribe = subscribers.get(breakpoint);

  if (subscribe === undefined) {
    subscribe = (onChange): (() => void) => {
      const media = globalThis.matchMedia(queries[breakpoint]);

      media.addEventListener("change", onChange);

      return () => {
        media.removeEventListener("change", onChange);
      };
    };
    subscribers.set(breakpoint, subscribe);
  }

  return subscribe;
}

/**
 * Whether the viewport is at least as wide as the breakpoint. A table that drops columns on a
 * narrow screen leaves them out of its model with this, rather than hiding their cells with CSS, so
 * the last column drawn is the last cell in the row and gets the panel's closing edge.
 */
export function useMinWidth(breakpoint: Breakpoint): boolean {
  return useSyncExternalStore(
    subscribeTo(breakpoint),
    () => globalThis.matchMedia(queries[breakpoint]).matches,
  );
}

/** Every breakpoint the viewport reaches, for a table choosing its columns. */
export interface Widths {
  readonly sm: boolean;
  readonly md: boolean;
  readonly lg: boolean;
  readonly xl: boolean;
  readonly "2xl": boolean;
}

export function useWidths(): Widths {
  return {
    sm: useMinWidth("sm"),
    md: useMinWidth("md"),
    lg: useMinWidth("lg"),
    xl: useMinWidth("xl"),
    "2xl": useMinWidth("2xl"),
  };
}
