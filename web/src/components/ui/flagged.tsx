import { Badge } from "@cloudflare/kumo/components/badge";
import type { BadgeVariant } from "@cloudflare/kumo/components/badge";
import { Popover } from "@cloudflare/kumo/components/popover";
import type { Icon } from "@phosphor-icons/react";
import { WarningIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

/** The tones a flag comes in; success and neutral are states, not flags. */
export type FlagTone = "warning" | "danger" | "info";

const variants: Record<FlagTone, BadgeVariant> = {
  warning: "warning",
  danger: "error",
  info: "info",
};

const openDelay = 150;

/**
 * A thing with something to say about it: the name itself becomes the pill, tinted and carrying the
 * icon, and the reason is one hover or tap away. A second pill beside the name that said "Missing"
 * doubled the chrome and still left the operator to guess which prefix was missing.
 *
 * The trigger is a button, so the keyboard reaches it and a screen reader hears the name; the
 * popover opens on hover on a desktop and on a tap on a phone.
 */
export function Flagged({
  tone = "warning",
  icon = WarningIcon,
  title,
  detail,
  children,
}: {
  readonly tone?: FlagTone;
  readonly icon?: Icon;
  /** The reason in one line, the popover's heading. */
  readonly title: string;
  /** What exactly: the prefixes, the routes, what to do about it. */
  readonly detail?: ReactNode;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <Popover>
      <Popover.Trigger
        openOnHover
        delay={openDelay}
        className="inline-flex max-w-full rounded-full outline-none focus-visible:ring-2 focus-visible:ring-kumo-focus"
      >
        <Badge variant={variants[tone]} icon={icon} className="max-w-full">
          <span className="truncate">{children}</span>
        </Badge>
      </Popover.Trigger>
      <Popover.Content side="top" className="max-w-72 gap-1">
        <Popover.Title className="text-sm leading-5">{title}</Popover.Title>
        {detail === undefined ? null : (
          <Popover.Description className="text-sm text-kumo-subtle">{detail}</Popover.Description>
        )}
      </Popover.Content>
    </Popover>
  );
}
