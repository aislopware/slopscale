import { Button } from "@cloudflare/kumo/components/button";
import { cn } from "@cloudflare/kumo/utils";
import { CheckIcon, CopySimpleIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import { copyText } from "~/lib/copy.ts";

const iconSize = 14;
const resetAfterMs = 1500;

/**
 * A monospace value that copies itself when clicked, drawn like Kumo's InlineCopyText: the copy
 * icon fades in while the value, or the table row around it, is hovered or focused, and turns into
 * a tick once copied. The icon keeps its room while hidden, so a row does not shift under the
 * pointer. It is the console's own control rather than Kumo's because Kumo writes through
 * `navigator.clipboard`, which a console served over plain HTTP on a LAN does not have; `copyText`
 * falls back to the selection API there.
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
        // The hover tint reaches 4px past the text either side, so the text keeps the edge every
        // other value has. A container sized to the control measures its margin box, 8px short of
        // the text, so the cap is 100% plus those 8px or the last character is always cut.
        "group/copy -mx-1 inline-flex max-w-[calc(100%+0.5rem)] min-w-0 items-center gap-1 rounded-sm px-1 font-mono text-[0.9em] focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none",
        className,
      )}
    >
      <span className={cn("min-w-0", wrap ? "break-all" : "truncate")}>{display ?? value}</span>
      {copied ? (
        <CheckIcon size={iconSize} className="shrink-0 text-kumo-success" />
      ) : (
        <CopySimpleIcon
          size={iconSize}
          aria-hidden
          className="shrink-0 text-kumo-subtle opacity-0 group-focus-within:opacity-100 group-hover:opacity-100 group-hover/copy:opacity-100 group-focus-visible/copy:opacity-100"
        />
      )}
      {/* The tick says it to a sighted user; this says it to a screen reader. */}
      <output className="sr-only">{copied ? "Copied" : ""}</output>
    </button>
  );
}

/**
 * A ghost icon button that copies a value shown beside it, the 26px square Zero Trust's details
 * rail puts at the end of its ID row ("Copy Profile ID"). The icon turns into a tick once copied.
 */
export function CopyButton({
  value,
  label,
  className,
}: {
  readonly value: string;
  /** The accessible name: "Copy tailnet ID". */
  readonly label: string;
  readonly className?: string;
}): ReactElement {
  const [copied, setCopied] = useState(false);

  async function run(): Promise<void> {
    if (await copyText(value)) {
      setCopied(true);
      setTimeout(() => {
        setCopied(false);
      }, resetAfterMs);
    }
  }

  return (
    <Button
      variant="ghost"
      shape="square"
      size="sm"
      aria-label={copied ? "Copied" : label}
      title={copied ? "Copied" : label}
      className={cn("shrink-0 text-kumo-subtle", className)}
      onClick={() => {
        void run();
      }}
    >
      {copied ? (
        <CheckIcon size={iconSize} aria-hidden className="text-kumo-success" />
      ) : (
        <CopySimpleIcon size={iconSize} aria-hidden />
      )}
    </Button>
  );
}
