import { Banner } from "@cloudflare/kumo/components/banner";
import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import { InfoIcon, WarningCircleIcon, WarningIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

/**
 * What the callout is telling the operator: a fact about the tailnet, or something to watch out
 * for.
 */
export type CalloutTone = "info" | "warning" | "error";

const tones = {
  info: { variant: "default", icon: InfoIcon, color: "text-kumo-info" },
  warning: { variant: "alert", icon: WarningIcon, color: "text-kumo-warning" },
  error: { variant: "error", icon: WarningCircleIcon, color: "text-kumo-danger" },
} as const;

/**
 * A short notice about the state of things, as Kumo's Banner draws it on the dashboard: the tone's
 * tint, its icon on the first line, a bold first line and a sentence or two, all in the tone's
 * colour (the info banner is blue throughout).
 */
export function Callout({
  tone = "info",
  icon,
  title,
  description,
  action,
  className,
}: {
  readonly tone?: CalloutTone;
  /** An icon that says more than the tone does, such as the file a setting came from. */
  readonly icon?: Icon;
  readonly title: string;
  readonly description?: ReactNode;
  /** A trailing control, such as Banner.Action. */
  readonly action?: ReactNode;
  readonly className?: string;
}): ReactElement {
  const { variant, icon: toneIcon, color } = tones[tone];
  const Glyph = icon ?? toneIcon;

  return (
    <Banner
      size="sm"
      variant={variant}
      icon={
        <span className="flex h-lh items-center">
          <Glyph className={color} />
        </span>
      }
      title={title}
      description={description}
      action={action}
      className={cn("items-start", className)}
    />
  );
}
