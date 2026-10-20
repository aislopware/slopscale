import { Popover } from "@cloudflare/kumo/components/popover";
import type { Icon } from "@phosphor-icons/react";
import { WarningIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

/** The tones a flag comes in; success and neutral are states, not flags. */
export type FlagTone = "warning" | "danger" | "info";

const iconColours: Record<FlagTone, string> = {
  warning: "text-kumo-warning",
  danger: "text-kumo-danger",
  info: "text-kumo-info",
};

const iconSize = 14;

const openDelay = 150;

/**
 * A thing with something to say about it, as the dashboard flags a DNS record: a 14px icon in the
 * tone's colour before the name, the name underlined with dots, and the reason one hover or tap
 * away in a white popover. A second pill beside the name that said "Missing" doubled the chrome and
 * still left the operator to guess which prefix was missing.
 *
 * The trigger is a button, so the keyboard reaches it and a screen reader hears the name; the
 * popover opens on hover on a desktop and on a tap on a phone.
 */
export function Flagged({
  tone = "warning",
  icon: FlagIcon = WarningIcon,
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
        className="inline-flex max-w-full items-center gap-1.5 rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-kumo-focus"
      >
        <FlagIcon size={iconSize} aria-hidden className={`shrink-0 ${iconColours[tone]}`} />
        <span className="truncate underline decoration-current/40 decoration-dotted underline-offset-4">
          {children}
        </span>
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
