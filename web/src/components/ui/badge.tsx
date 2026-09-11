import { Badge as KumoBadge } from "@cloudflare/kumo/components/badge";
import type { BadgeVariant } from "@cloudflare/kumo/components/badge";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import type { Tone } from "~/components/ui/status.tsx";

const variants: Record<Tone, BadgeVariant> = {
  success: "success",
  warning: "warning",
  danger: "error",
  info: "info",
  neutral: "secondary",
};

/**
 * The state of a row's subject, in the tint of its tone: "Connected", "Needs approval", "Expired".
 * It goes in exactly two places, a table's state column and the meta line of a page header, so a
 * page carries one badge per thing and the tint is what the eye scans for.
 *
 * A state that is a fact about a thing rather than the thing's own state (Funnel on, key valid, an
 * update available) stays a `Status` word, so a definition list is a list of words. Neither carries
 * a dot or an icon: the word and its tint already say it. The corner radius is the console's
 * control radius rather than a full pill, so a badge sits with the buttons and inputs around it
 * instead of reading as a tag.
 */
export function Badge({
  tone,
  children,
  className,
}: {
  readonly tone: Tone;
  readonly children: ReactNode;
  readonly className?: string;
}): ReactElement {
  return (
    <KumoBadge
      variant={variants[tone]}
      className={cn(
        "rounded-md",
        // Kumo's neutral fill is as loud as its tints; a state that is merely ordinary (Disconnected,
        // Used, Off) steps back to the recessed surface and the subtle text, so the tinted ones are
        // the rows to look at.
        tone === "neutral" && "bg-kumo-recessed text-kumo-subtle",
        className,
      )}
    >
      {children}
    </KumoBadge>
  );
}
