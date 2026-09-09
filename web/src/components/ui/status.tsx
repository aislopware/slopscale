import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import { WarningIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

/** What a state means to the operator, coloured the same way everywhere. */
export type Tone = "success" | "warning" | "danger" | "info" | "neutral";

const dots: Record<Tone, string> = {
  success: "bg-kumo-success",
  warning: "bg-kumo-warning",
  danger: "bg-kumo-danger",
  info: "bg-kumo-info",
  // Kumo has no inactive fill token, only the text one, so the dot borrows it through currentColor.
  neutral: "bg-current text-kumo-inactive",
};

const texts: Record<Tone, string> = {
  success: "text-kumo-success",
  warning: "text-kumo-warning",
  danger: "text-kumo-danger",
  info: "text-kumo-info",
  neutral: "text-kumo-subtle",
};

const noteIconSize = 14;

export function Dot({
  tone,
  className,
}: {
  readonly tone: Tone;
  readonly className?: string;
}): ReactElement {
  return (
    <span
      aria-hidden
      className={cn("inline-block size-2 shrink-0 rounded-full", dots[tone], className)}
    />
  );
}

/**
 * A state as words with a coloured dot in front: "Connected", "Pending", "Failed". Plain text, not
 * a pill, so a column of states reads as a column and the colour alone carries the meaning. A pill
 * is for a thing the operator can pick out, such as a tag; a state is a fact about the row.
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
  return (
    <span className={cn("inline-flex items-center gap-1.5 whitespace-nowrap", className)}>
      <Dot tone={tone} />
      {children}
    </span>
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
