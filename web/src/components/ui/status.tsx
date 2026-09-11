import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import { WarningIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

import { HoverPopover } from "~/components/ui/hover-popover.tsx";

/** What a state means to the operator, coloured the same way everywhere. */
export type Tone = "success" | "warning" | "danger" | "info" | "neutral";

/**
 * Only bad news gets a colour. A good or ordinary state reads in the default text, an inactive one
 * steps back to subtle, so a column of states is a column of words and the coloured ones are the
 * rows to look at.
 */
const texts: Record<Tone, string> = {
  success: "text-kumo-default",
  warning: "text-kumo-warning",
  danger: "text-kumo-danger",
  info: "text-kumo-default",
  neutral: "text-kumo-subtle",
};

const noteIconSize = 14;

/**
 * A state as words: "On", "Valid", "Update available". Plain text, not a pill and not a dot, for a
 * fact about a thing inside a definition list, a sentence or a cell that says something else too.
 * The state of the thing itself, in a table's state column or a page header, is `Badge` from
 * `ui/badge.tsx`, so a page carries one tinted state per thing and the rest stays words.
 */
export function Status({
  tone,
  children,
  className,
}: {
  readonly tone: Tone;
  readonly children: ReactNode;
  readonly className?: string;
}): ReactElement {
  return <span className={cn("whitespace-nowrap", texts[tone], className)}>{children}</span>;
}

/**
 * A state whose reason does not fit on the line: the word, and the reason, the client's own text
 * and any timestamp one hover, tap or focus away. A sentence rendered where "Valid" goes is cut off
 * by the row it sits in and reads as a state of its own, so the words stay short and the rest goes
 * in the popover.
 */
export function StatusDetail({
  tone,
  label,
  title,
  detail,
}: {
  readonly tone: Tone;
  /** The state in a word: "Failed", "Unhealthy", "Expiring". */
  readonly label: string;
  /** The heading of the popover; the label itself when there is nothing shorter to say. */
  readonly title?: ReactNode;
  readonly detail?: ReactNode;
}): ReactElement {
  return (
    <HoverPopover
      title={title ?? label}
      {...(detail === undefined ? {} : { detail })}
      triggerClassName="inline-flex max-w-full rounded-sm underline decoration-dotted underline-offset-4 outline-none focus-visible:ring-2 focus-visible:ring-kumo-focus"
    >
      <Status tone={tone}>{label}</Status>
    </HoverPopover>
  );
}

/**
 * A sentence the operator should read before moving on, in the tone's colour with its icon on the
 * first line. It replaces a pill with a sentence in it, which wrapped badly and shouted.
 */
export function Note({
  tone = "warning",
  icon: NoteIcon = WarningIcon,
  children,
  className,
}: {
  readonly tone?: Tone;
  readonly icon?: Icon;
  readonly children: ReactNode;
  readonly className?: string;
}): ReactElement {
  return (
    <span className={cn("inline-flex items-start gap-1.5 text-sm", texts[tone], className)}>
      <span className="flex h-lh shrink-0 items-center">
        <NoteIcon size={noteIconSize} aria-hidden />
      </span>
      <span>{children}</span>
    </span>
  );
}
