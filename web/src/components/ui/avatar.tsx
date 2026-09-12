import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { baseHue, hueColours } from "~/lib/hue.ts";

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
  xl: "size-10 text-sm",
} as const;

const iconSizes: Record<keyof typeof sizes, number> = { sm: 12, base: 14, lg: 16, xl: 20 };

/**
 * Identity mark: a circle with a hairline inside its edge, so it sits on the surface instead of
 * floating. Sized to the row it lives in, never to touch-target minimums. A person is their picture
 * when they have one, on top of their initials in a quiet tint of a hue spaced by their id (see
 * `baseHue`), so the same person is the same colour in every list and a small team spreads over the
 * whole wheel; the tint is what shows until the picture arrives, and what stays when it fails.
 * Something that is not a person, such as a key or the server itself, is an icon on the neutral
 * mark, so a column of actors still lines up while the kind is told at a glance.
 */
export function Avatar({
  name,
  id,
  src,
  icon: Mark,
  size = "base",
  className,
}: {
  readonly name: string;
  /** The user's id, which spaces people apart; without it the colour is hashed from the name. */
  readonly id?: string;
  /** The person's picture; empty or missing shows the initials. */
  readonly src?: string | undefined;
  /** Stands in for the initials when the name is not a person's. */
  readonly icon?: Icon;
  readonly size?: keyof typeof sizes;
  readonly className?: string;
}): ReactElement {
  return (
    <span
      aria-hidden
      className={cn(
        "relative inline-flex shrink-0 items-center justify-center overflow-hidden rounded-full leading-none font-medium select-none",
        Mark === undefined ? "" : "bg-kumo-elevated text-kumo-strong",
        sizes[size],
        className,
      )}
      style={Mark === undefined ? hueColours(baseHue(name, id)) : undefined}
    >
      {Mark === undefined ? initials(name) : <Mark size={iconSizes[size]} weight="bold" />}
      {src === undefined || src === "" || Mark !== undefined ? null : (
        <Picture key={src} src={src} />
      )}
      {/* Over the picture, so its edge is drawn on it as on the tint. */}
      <span className="absolute inset-0 rounded-full ring ring-kumo-line ring-inset" />
    </span>
  );
}

/**
 * The picture, faded in over the initials once it has loaded and dropped if it never does. The
 * element is there from the first paint, so a picture the browser has already fetched shows at
 * once; the referrer stays home because the URL belongs to whichever provider signed the user in.
 */
function Picture({ src }: { readonly src: string }): ReactElement | null {
  const [state, setState] = useState<"loading" | "loaded" | "failed">("loading");

  if (state === "failed") {
    return null;
  }

  return (
    <img
      src={src}
      alt=""
      draggable={false}
      referrerPolicy="no-referrer"
      data-loaded={state === "loaded"}
      className="absolute inset-0 size-full object-cover opacity-0 transition-opacity duration-300 data-[loaded=true]:opacity-100 motion-reduce:transition-none"
      ref={(img) => {
        // A cached picture is complete before any load event this element will see.
        if (img !== null && img.complete && img.naturalWidth > 0) {
          setState("loaded");
        }
      }}
      onLoad={() => {
        setState("loaded");
      }}
      onError={() => {
        setState("failed");
      }}
    />
  );
}
