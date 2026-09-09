import { cn } from "@cloudflare/kumo/utils";
import { CheckIcon, CopyIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import { copyText } from "~/lib/copy.ts";

const iconSize = 12;
const resetAfterMs = 1500;

/**
 * A monospace value that copies itself when clicked. The copy icon is always there, small and
 * muted, so a copyable value ends where every other value ends and a row stays as tall as its text.
 * Hiding the icon until hover left a hole beside every copyable value. The icon's ink is centred on
 * the line box, which puts it a pixel above the centre of lowercase text, so it steps down one.
 */
export function CopyText({
  value,
  copy = value,
  display,
  label,
  wrap = false,
  className,
}: {
  readonly value: string;
  /** What goes on the clipboard when it differs from the shown text, such as a whole secret. */
  readonly copy?: string;
  /** What to show in place of the plain value, such as a URL with its host picked out. */
  readonly display?: ReactNode;
  /** The tooltip and accessible name of the control. */
  readonly label?: string;
  /** Let a long value wrap onto several lines instead of truncating it. */
  readonly wrap?: boolean;
  readonly className?: string;
}): ReactElement {
  const [copied, setCopied] = useState(false);
  const name = label ?? `Copy ${value}`;

  async function run(): Promise<void> {
    if (await copyText(copy)) {
      setCopied(true);
      setTimeout(() => {
        setCopied(false);
      }, resetAfterMs);
    }
  }

  return (
    <button
      type="button"
      title={copied ? "Copied" : name}
      aria-label={name}
      onClick={() => {
        void run();
      }}
      className={cn(
        "group/copy -mx-1 inline-flex max-w-full min-w-0 items-center gap-1 rounded-sm px-1 font-mono text-[0.9em] hover:bg-kumo-tint focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none",
        className,
      )}
    >
      <span className={cn("min-w-0", wrap ? "break-all" : "truncate")}>{display ?? value}</span>
      {copied ? (
        <CheckIcon size={iconSize} className="relative top-px shrink-0 text-kumo-success" />
      ) : (
        <CopyIcon
          size={iconSize}
          aria-hidden
          className="relative top-px shrink-0 text-kumo-subtle group-hover/copy:text-kumo-default"
        />
      )}
      {/* The tick says it to a sighted user; this says it to a screen reader. */}
      <output className="sr-only">{copied ? "Copied" : ""}</output>
    </button>
  );
}
