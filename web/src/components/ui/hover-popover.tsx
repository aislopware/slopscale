import { Popover } from "@cloudflare/kumo/components/popover";
import { InfoIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

const openDelay = 150;
const helpIconSize = 14;

/**
 * A trigger with the long version of itself one hover, tap or focus away. The trigger is a button,
 * so the keyboard reaches it and a phone opens it with a tap; the popover carries the sentence that
 * would otherwise sit on the line and push everything beside it out of the way.
 */
export function HoverPopover({
  title,
  detail,
  triggerLabel,
  triggerClassName,
  children,
}: {
  /** The heading of the popover: the reason in one line. */
  readonly title: ReactNode;
  /** The rest of it: what exactly, and since when. */
  readonly detail?: ReactNode;
  /** The accessible name of the trigger, for a trigger that is an icon rather than words. */
  readonly triggerLabel?: string;
  readonly triggerClassName?: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <Popover>
      <Popover.Trigger
        openOnHover
        delay={openDelay}
        {...(triggerLabel === undefined ? {} : { "aria-label": triggerLabel })}
        className={
          triggerClassName ??
          "inline-flex max-w-full rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-kumo-focus"
        }
      >
        {children}
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

/**
 * The explanation of a setting, beside its short label. The label says which setting it is and this
 * says what it does, so a grid of settings stays a grid instead of a column of sentences.
 */
export function HelpTip({
  label,
  children,
}: {
  /** The setting this explains, for the operator who hears the button rather than sees it. */
  readonly label: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <HoverPopover
      title={label}
      detail={children}
      triggerLabel={`What ${label} does`}
      triggerClassName="inline-flex h-lh shrink-0 items-center rounded-sm text-kumo-subtle outline-none hover:text-kumo-default focus-visible:ring-2 focus-visible:ring-kumo-focus"
    >
      <InfoIcon size={helpIconSize} aria-hidden />
    </HoverPopover>
  );
}
