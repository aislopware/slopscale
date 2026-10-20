import { Badge as KumoBadge } from "@cloudflare/kumo/components/badge";
import type { BadgeVariant } from "@cloudflare/kumo/components/badge";
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
 * The state of a row's subject as Kumo's Badge, the dashboard's 20px pill in the tone's tint:
 * "Active" and "Connected" in success green, "Disabled" and "Expired" in the neutral secondary
 * fill, "Needs approval" in warning, "Failed" in error. It goes in a table's state column and the
 * meta line of a page header, one per thing. A fact about a thing rather than its own state (Funnel
 * on, key valid) is a `Status`: an icon and the word.
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
    <KumoBadge variant={variants[tone]} {...(className === undefined ? {} : { className })}>
      {children}
    </KumoBadge>
  );
}
